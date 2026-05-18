// Package outbox_test exercises the durable outbox from outside the
// package: tests import only the published surface (New, Outbox, Store,
// NewMemoryStore, NewSQLStore, ErrDuplicate, SchemaSQL, Options) and
// never reach into unexported state. SQLStore is covered structurally
// (the SchemaSQL constant is well-formed and NewSQLStore validates its
// argument); end-to-end SQL integration is the downstream tests' job.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package outbox_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/event"
	"github.com/septagon-oss/pk-core/pkg/event/memory"
	"github.com/septagon-oss/pk-core/pkg/event/outbox"
)

func newEnv(id, typ string) event.Envelope {
	return event.Envelope{
		ID:     id,
		Type:   typ,
		Source: "test",
		Data:   []byte(`{"k":"v"}`),
	}
}

// waitFor polls fn until it returns true or the deadline elapses.
func waitFor(t *testing.T, deadline time.Duration, fn func() bool) {
	t.Helper()
	stop := time.Now().Add(deadline)
	for time.Now().Before(stop) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor: timed out after %v", deadline)
}

// --- MemoryStore contract tests -------------------------------------------

func TestMemoryStoreSaveAndNextBatch(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		env := newEnv("evt-"+string(rune('a'+i)), "e")
		if err := store.Save(ctx, env); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}
	batch, err := store.NextBatch(ctx, 10)
	if err != nil {
		t.Fatalf("NextBatch: %v", err)
	}
	if len(batch) != 3 {
		t.Errorf("len(batch) = %d, want 3", len(batch))
	}
	if batch[0].Envelope.ID != "evt-a" {
		t.Errorf("FIFO violated: first = %s", batch[0].Envelope.ID)
	}
}

func TestMemoryStoreNextBatchHonorsLimit(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = store.Save(ctx, newEnv("evt-"+string(rune('a'+i)), "e"))
	}
	batch, _ := store.NextBatch(ctx, 2)
	if len(batch) != 2 {
		t.Errorf("limited batch len = %d, want 2", len(batch))
	}
}

func TestMemoryStoreMarkDeliveredRemovesFromBatch(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	ctx := context.Background()
	_ = store.Save(ctx, newEnv("evt-1", "e"))
	_ = store.Save(ctx, newEnv("evt-2", "e"))
	if err := store.MarkDelivered(ctx, "evt-1"); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	batch, _ := store.NextBatch(ctx, 10)
	if len(batch) != 1 || batch[0].Envelope.ID != "evt-2" {
		t.Errorf("after delivered: batch=%v", batch)
	}
}

func TestMemoryStoreMarkFailedIncrementsAttempts(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	ctx := context.Background()
	_ = store.Save(ctx, newEnv("evt-1", "e"))
	if err := store.MarkFailed(ctx, "evt-1", "boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if err := store.MarkFailed(ctx, "evt-1", "boom"); err != nil {
		t.Fatalf("MarkFailed 2: %v", err)
	}
	batch, _ := store.NextBatch(ctx, 10)
	if len(batch) != 1 || batch[0].Attempts != 2 {
		t.Errorf("attempts = %d, want 2; batch=%v", batch[0].Attempts, batch)
	}
}

func TestMemoryStoreDuplicateIdempotencyKey(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	ctx := context.Background()
	env1 := newEnv("evt-1", "e")
	env1.IdempotencyKey = "k1"
	if err := store.Save(ctx, env1); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	env2 := newEnv("evt-2", "e")
	env2.IdempotencyKey = "k1"
	err := store.Save(ctx, env2)
	if !errors.Is(err, outbox.ErrDuplicate) {
		t.Errorf("got %v, want ErrDuplicate", err)
	}
	batch, _ := store.NextBatch(ctx, 10)
	if len(batch) != 1 {
		t.Errorf("dedupe failed: batch=%v", batch)
	}
}

func TestMemoryStoreSaveRequiresValidEnvelope(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	err := store.Save(context.Background(), event.Envelope{Type: "e", Source: "test"})
	if !errors.Is(err, event.ErrInvalidEnvelope) {
		t.Errorf("got %v, want ErrInvalidEnvelope", err)
	}
}

func TestMemoryStoreRespectsCtxCancellation(t *testing.T) {
	t.Parallel()
	store := outbox.NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Save(ctx, newEnv("evt-1", "e")); !errors.Is(err, context.Canceled) {
		t.Errorf("Save: %v, want Canceled", err)
	}
	if _, err := store.NextBatch(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Errorf("NextBatch: %v, want Canceled", err)
	}
	if err := store.MarkDelivered(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Errorf("MarkDelivered: %v, want Canceled", err)
	}
	if err := store.MarkFailed(ctx, "x", "y"); !errors.Is(err, context.Canceled) {
		t.Errorf("MarkFailed: %v, want Canceled", err)
	}
}

// --- Outbox dispatcher tests ---------------------------------------------

func TestPublishWritesToStore(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()
	ob := outbox.New(inner, store)

	if err := ob.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	batch, _ := store.NextBatch(context.Background(), 10)
	if len(batch) != 1 || batch[0].Envelope.ID != "evt-1" {
		t.Errorf("store contents: %+v", batch)
	}
}

func TestDispatcherForwardsToInnerAndMarksDelivered(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()

	var hits int32
	if _, err := inner.Subscribe("e", func(context.Context, event.Envelope) error {
		atomic.AddInt32(&hits, 1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	ob := outbox.New(inner, store, outbox.WithDispatchInterval(10*time.Millisecond))
	ob.Start(context.Background())
	defer ob.Stop()

	if err := ob.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		return atomic.LoadInt32(&hits) == 1
	})
	waitFor(t, 2*time.Second, func() bool {
		batch, _ := store.NextBatch(context.Background(), 10)
		return len(batch) == 0
	})
}

func TestDispatcherMarksFailedOnError(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()

	var attempts int32
	if _, err := inner.Subscribe("e", func(context.Context, event.Envelope) error {
		atomic.AddInt32(&attempts, 1)
		return errors.New("downstream offline")
	}); err != nil {
		t.Fatal(err)
	}

	ob := outbox.New(inner, store, outbox.WithDispatchInterval(10*time.Millisecond))
	ob.Start(context.Background())
	defer ob.Stop()

	if err := ob.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Wait for at least two dispatcher passes (entry stays pending).
	waitFor(t, 2*time.Second, func() bool {
		return atomic.LoadInt32(&attempts) >= 2
	})
	batch, _ := store.NextBatch(context.Background(), 10)
	if len(batch) != 1 {
		t.Errorf("failed entry should remain pending, got %d", len(batch))
		return
	}
	if batch[0].Attempts < 2 {
		t.Errorf("attempts counter = %d, want >= 2", batch[0].Attempts)
	}
}

func TestDispatcherGivesUpAfterMaxRetries(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()

	if _, err := inner.Subscribe("e", func(context.Context, event.Envelope) error {
		return errors.New("nope")
	}); err != nil {
		t.Fatal(err)
	}

	ob := outbox.New(inner, store,
		outbox.WithDispatchInterval(5*time.Millisecond),
		outbox.WithMaxRetries(2),
	)
	ob.Start(context.Background())
	defer ob.Stop()

	if err := ob.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		batch, _ := store.NextBatch(context.Background(), 10)
		return len(batch) == 0
	})
}

func TestDuplicateIdempotencyKeyDeduplicatedAtOutbox(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()
	ob := outbox.New(inner, store)

	env := newEnv("evt-1", "e")
	env.IdempotencyKey = "k1"
	if err := ob.Publish(context.Background(), env); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	env2 := newEnv("evt-2", "e")
	env2.IdempotencyKey = "k1"
	if err := ob.Publish(context.Background(), env2); err != nil {
		t.Fatalf("second Publish (dedupe): %v", err)
	}
	batch, _ := store.NextBatch(context.Background(), 10)
	if len(batch) != 1 {
		t.Errorf("dedupe failed: batch=%v", batch)
	}
}

func TestPublishAfterStopReturnsErrBusClosed(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()
	ob := outbox.New(inner, store)
	ob.Start(context.Background())
	ob.Stop()

	err := ob.Publish(context.Background(), newEnv("evt-1", "e"))
	if !errors.Is(err, event.ErrBusClosed) {
		t.Errorf("got %v, want ErrBusClosed", err)
	}
}

func TestStartIsIdempotent(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()
	ob := outbox.New(inner, store, outbox.WithDispatchInterval(10*time.Millisecond))
	ob.Start(context.Background())
	ob.Start(context.Background()) // second call is a no-op
	ob.Stop()
}

func TestStopIsIdempotent(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()
	ob := outbox.New(inner, store)
	ob.Start(context.Background())
	ob.Stop()
	ob.Stop() // second call is a no-op
}

func TestStopDrainsDispatcher(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()

	var seen int32
	released := make(chan struct{})
	if _, err := inner.Subscribe("e", func(context.Context, event.Envelope) error {
		atomic.AddInt32(&seen, 1)
		// Cooperate with Stop by checking the channel; if Stop already
		// fired, ctx will be cancelled and we still complete cleanly.
		select {
		case <-released:
		case <-time.After(50 * time.Millisecond):
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ob := outbox.New(inner, store, outbox.WithDispatchInterval(5*time.Millisecond))
	ob.Start(context.Background())
	if err := ob.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt32(&seen) >= 1 })
	close(released)
	ob.Stop() // must wait for in-flight handler to complete
}

func TestSubscribeForwardsToInner(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()
	ob := outbox.New(inner, store, outbox.WithDispatchInterval(10*time.Millisecond))
	ob.Start(context.Background())
	defer ob.Stop()

	var mu sync.Mutex
	var got []string
	sub, err := ob.Subscribe("e", func(_ context.Context, env event.Envelope) error {
		mu.Lock()
		got = append(got, env.ID)
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	if err := ob.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1 && got[0] == "evt-1"
	})
}

func TestPublishValidatesEnvelope(t *testing.T) {
	t.Parallel()
	inner := memory.New()
	defer inner.Close()
	store := outbox.NewMemoryStore()
	ob := outbox.New(inner, store)

	err := ob.Publish(context.Background(), event.Envelope{Type: "e", Source: "test"})
	if !errors.Is(err, event.ErrInvalidEnvelope) {
		t.Errorf("got %v, want ErrInvalidEnvelope", err)
	}
}

func TestNewPanicsOnNilInner(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil inner")
		}
	}()
	outbox.New(nil, outbox.NewMemoryStore())
}

func TestNewPanicsOnNilStore(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil store")
		}
	}()
	inner := memory.New()
	defer inner.Close()
	outbox.New(inner, nil)
}

// --- SQLStore structural tests --------------------------------------------

func TestNewSQLStoreRejectsNilDB(t *testing.T) {
	t.Parallel()
	if _, err := outbox.NewSQLStore(nil); err == nil {
		t.Fatal("expected error for nil DB")
	}
}

func TestSchemaSQLIsWellFormed(t *testing.T) {
	t.Parallel()
	schema := outbox.SchemaSQL
	if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS pk_event_outbox") {
		t.Error("SchemaSQL missing CREATE TABLE statement")
	}
	if !strings.Contains(schema, "idempotency_key") {
		t.Error("SchemaSQL missing idempotency_key column")
	}
	if !strings.Contains(schema, "CREATE UNIQUE INDEX") {
		t.Error("SchemaSQL missing unique idempotency index")
	}
}
