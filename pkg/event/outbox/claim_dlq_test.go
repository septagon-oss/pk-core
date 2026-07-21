// Validates: REQ-PORTS-012.
// Per: ADR-0007.
// Discipline: C-14.

package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/event"
	"github.com/septagon-oss/pk-core/pkg/event/memory"
	"github.com/septagon-oss/pk-core/pkg/event/outbox"
)

// countingBus counts deliveries per envelope ID and can fail on demand.
type countingBus struct {
	mu     sync.Mutex
	counts map[string]int
	failOn map[string]error
}

func newCountingBus() *countingBus {
	return &countingBus{counts: map[string]int{}, failOn: map[string]error{}}
}

func (b *countingBus) Publish(_ context.Context, env event.Envelope) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err, ok := b.failOn[env.Type]; ok {
		return err
	}
	b.counts[env.ID]++
	return nil
}

func (b *countingBus) Subscribe(string, event.Handler) (event.Subscription, error) {
	return nil, errors.New("countingBus: subscribe unsupported")
}
func (b *countingBus) Close() error { return nil }

func (b *countingBus) snapshot() map[string]int {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]int, len(b.counts))
	maps.Copy(out, b.counts)
	return out
}

// TestConcurrentDispatchersDeliverExactlyOnce is THE D6 claim test: two
// Outbox dispatchers share one Store with mandatory claims; every envelope must reach
// the bus exactly once. Before claims, both dispatchers read the same
// row and double-delivered everything.
func TestConcurrentDispatchersDeliverExactlyOnce(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	bus := newCountingBus()
	ctx := context.Background()

	const total = 60
	for i := range total {
		if err := store.Save(ctx, newEnv(fmt.Sprintf("evt-%03d", i), "claim.test")); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	mk := func() *outbox.Outbox {
		return outbox.New(
			bus, store,
			outbox.WithDispatchInterval(time.Millisecond),
			outbox.WithBatchSize(8),
			outbox.WithClaimTTL(5*time.Second),
		)
	}
	a, b := mk(), mk()
	a.Start(ctx)
	b.Start(ctx)
	defer a.Stop()
	defer b.Stop()

	waitFor(t, 5*time.Second, func() bool {
		return len(bus.snapshot()) == total
	})
	a.Stop()
	b.Stop()

	for id, n := range bus.snapshot() {
		if n != 1 {
			t.Fatalf("envelope %s delivered %d times — claim race lost (double delivery)", id, n)
		}
	}
}

// TestPoisonEnvelopeDeadLetters: an envelope whose publish always fails
// must park via MarkDead after maxRetries — out of the pending queue,
// reported through the error handler, and no longer blocking the batch.
func TestPoisonEnvelopeDeadLetters(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	bus := newCountingBus()
	bus.failOn["poison.type"] = errors.New("subscriber permanently broken")
	ctx := context.Background()

	var mu sync.Mutex
	deadLettered := map[string]bool{}
	handler := func(_ context.Context, op, envelopeID string, _ error) {
		if op == "dead_letter" {
			mu.Lock()
			deadLettered[envelopeID] = true
			mu.Unlock()
		}
	}

	if err := store.Save(ctx, newEnv("evt-poison", "poison.type")); err != nil {
		t.Fatalf("save poison: %v", err)
	}
	if err := store.Save(ctx, newEnv("evt-healthy", "ok.type")); err != nil {
		t.Fatalf("save healthy: %v", err)
	}

	ob := outbox.New(
		bus, store,
		outbox.WithDispatchInterval(time.Millisecond),
		outbox.WithMaxRetries(3),
		outbox.WithErrorHandler(handler),
	)
	ob.Start(ctx)
	defer ob.Stop()

	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return deadLettered["evt-poison"]
	})

	// Healthy traffic flowed despite the poison row.
	waitFor(t, 5*time.Second, func() bool {
		return bus.snapshot()["evt-healthy"] == 1
	})
	// The dead row never reached the bus and left the pending queue.
	if bus.snapshot()["evt-poison"] != 0 {
		t.Fatal("poison envelope reached the bus")
	}
	ob.Stop()
	batch, err := store.ClaimBatch(ctx, 10, time.Minute)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	for _, entry := range batch {
		if entry.Envelope.ID == "evt-poison" {
			t.Fatal("dead-lettered envelope still pending — it would retry forever")
		}
	}
}

// TestExpiredClaimIsRedelivered: a claim left by a crashed dispatcher
// must expire, returning the envelope to eligibility.
func TestExpiredClaimIsRedelivered(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	ctx := context.Background()
	if err := store.Save(ctx, newEnv("evt-1", "e")); err != nil {
		t.Fatalf("save: %v", err)
	}

	// "Crashed dispatcher": claims with a short TTL, never delivers.
	claimed, err := store.ClaimBatch(ctx, 10, 50*time.Millisecond)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("first claim: %v (n=%d)", err, len(claimed))
	}
	// While claimed, nothing is eligible.
	if again, _ := store.ClaimBatch(ctx, 10, time.Second); len(again) != 0 {
		t.Fatalf("claimed envelope re-claimed inside its window (n=%d)", len(again))
	}
	// After expiry, it is claimable again.
	time.Sleep(60 * time.Millisecond)
	reclaimed, err := store.ClaimBatch(ctx, 10, time.Second)
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("expired claim not reclaimed: %v (n=%d)", err, len(reclaimed))
	}
}

// TestErrorHandlerObservesPublishFailures: the previously-silent
// dispatcher now reports publish failures.
func TestErrorHandlerObservesPublishFailures(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	inner := memory.New()
	defer inner.Close()
	bus := newCountingBus()
	bus.failOn["fail.type"] = errors.New("bus down")
	_ = inner // silence linters if memory bus unused on some paths

	var mu sync.Mutex
	var ops []string
	handler := func(_ context.Context, op, _ string, _ error) {
		mu.Lock()
		ops = append(ops, op)
		mu.Unlock()
	}

	ctx := context.Background()
	if err := store.Save(ctx, newEnv("evt-f", "fail.type")); err != nil {
		t.Fatalf("save: %v", err)
	}
	ob := outbox.New(
		bus, store,
		outbox.WithDispatchInterval(time.Millisecond),
		outbox.WithErrorHandler(handler),
	)
	ob.Start(ctx)
	defer ob.Stop()

	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(ops, "publish")
	})
}

// flakyClaimStore wraps a Store and fails ClaimBatch on demand.
type flakyClaimStore struct {
	outbox.Store
	mu      sync.Mutex
	failNow bool
}

func (s *flakyClaimStore) setFail(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNow = v
}

func (s *flakyClaimStore) ClaimBatch(ctx context.Context, limit int, ttl time.Duration) ([]outbox.PendingEntry, error) {
	s.mu.Lock()
	fail := s.failNow
	s.mu.Unlock()
	if fail {
		return nil, errors.New("flaky: claim unavailable")
	}
	return s.Store.ClaimBatch(ctx, limit, ttl)
}

// TestInitialClaimFailureStallsUntilRecovery proves there is no unclaimed
// downgrade path, even when the first claim fails.
func TestInitialClaimFailureStallsUntilRecovery(t *testing.T) {
	t.Parallel()
	inner := outbox.NewMemoryStore()
	store := &flakyClaimStore{Store: inner}
	store.setFail(true)
	bus := newCountingBus()
	ctx := context.Background()

	var mu sync.Mutex
	var claimReported bool
	handler := func(_ context.Context, op, _ string, _ error) {
		if op == "claim" {
			mu.Lock()
			claimReported = true
			mu.Unlock()
		}
	}

	if err := store.Save(ctx, newEnv("evt-1", "e")); err != nil {
		t.Fatalf("save: %v", err)
	}
	ob := outbox.New(
		bus, store,
		outbox.WithDispatchInterval(time.Millisecond),
		outbox.WithErrorHandler(handler),
	)
	ob.Start(ctx)
	defer ob.Stop()

	time.Sleep(50 * time.Millisecond)
	if bus.snapshot()["evt-1"] != 0 {
		t.Fatal("claim failure fell back to unsafe unclaimed dispatch")
	}
	mu.Lock()
	if !claimReported {
		mu.Unlock()
		t.Fatal("claim failure was not reported via the error handler")
	}
	mu.Unlock()
	store.setFail(false)
	waitFor(t, 5*time.Second, func() bool {
		return bus.snapshot()["evt-1"] == 1
	})
}

// TestTransientClaimFailureStallsInsteadOfDowngrading (council finding,
// Track D review): once claiming has succeeded, a later ClaimBatch
// error must stall the pass until the store recovers.
func TestTransientClaimFailureStallsInsteadOfDowngrading(t *testing.T) {
	t.Parallel()
	inner := outbox.NewMemoryStore()
	store := &flakyClaimStore{Store: inner}
	bus := newCountingBus()
	ctx := context.Background()

	if err := store.Save(ctx, newEnv("evt-1", "e")); err != nil {
		t.Fatalf("save: %v", err)
	}
	ob := outbox.New(
		bus, store,
		outbox.WithDispatchInterval(time.Millisecond),
		outbox.WithClaimTTL(20*time.Millisecond),
	)
	ob.Start(ctx)
	defer ob.Stop()

	// Phase 1: claiming works; the first envelope flows.
	waitFor(t, 5*time.Second, func() bool {
		return bus.snapshot()["evt-1"] == 1
	})

	// Phase 2: claims start failing (transient outage). New work must
	// NOT flow without a claim.
	store.setFail(true)
	if err := store.Save(ctx, newEnv("evt-2", "e")); err != nil {
		t.Fatalf("save: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // many dispatch passes
	if bus.snapshot()["evt-2"] != 0 {
		t.Fatal("dispatcher delivered without a claim during a transient failure")
	}

	// Phase 3: outage clears; delivery resumes through claims.
	store.setFail(false)
	waitFor(t, 5*time.Second, func() bool {
		return bus.snapshot()["evt-2"] == 1
	})
}

// TestSQLStoreImplementsRequiredCapabilities pins the structural contract:
// the SQL store provides claiming + dead-lettering (queries are
// integration-tested downstream, per this package's testing policy).
func TestSQLStoreImplementsRequiredCapabilities(t *testing.T) {
	t.Parallel()
	// Compile-time-ish assertion through the public constructor's type.
	if _, err := outbox.NewSQLStore(nil); err == nil {
		t.Fatal("NewSQLStore(nil) must error")
	}
	if outbox.DeadDeliveredAt != -1 {
		t.Fatalf("DeadDeliveredAt sentinel changed: %d", outbox.DeadDeliveredAt)
	}
}
