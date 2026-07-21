// Validates: REQ-PORTS-012.
// Per: ADR-0029.
// Discipline: C-14.

package memory_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/event"
	"github.com/septagon-oss/pk-core/pkg/event/memory"
)

func newEnv(id, typ string) event.Envelope {
	return event.Envelope{
		ID:     id,
		Type:   typ,
		Source: "test",
		Data:   []byte(`{"k":"v"}`),
	}
}

func TestSyncPublishDeliversToSubscribers(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	defer bus.Close()

	var got int32
	_, err := bus.Subscribe("user.created", func(ctx context.Context, env event.Envelope) error {
		atomic.AddInt32(&got, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := bus.Publish(context.Background(), newEnv("evt-1", "user.created")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got != 1 {
		t.Errorf("handler invocations = %d, want 1", got)
	}
}

func TestSubscribeMultipleHandlers(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	defer bus.Close()

	var hits int32
	for range 3 {
		if _, err := bus.Subscribe("e", func(ctx context.Context, env event.Envelope) error {
			atomic.AddInt32(&hits, 1)
			return nil
		}); err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
	}
	if err := bus.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if hits != 3 {
		t.Errorf("hits = %d, want 3", hits)
	}
}

func TestSubscribeIgnoresUnmatchedType(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	defer bus.Close()

	var hits int32
	if _, err := bus.Subscribe("user.created", func(ctx context.Context, env event.Envelope) error {
		atomic.AddInt32(&hits, 1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Publish(context.Background(), newEnv("evt-1", "user.deleted")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if hits != 0 {
		t.Errorf("hits = %d, want 0", hits)
	}
}

func TestCancelStopsDelivery(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	defer bus.Close()

	var hits int32
	sub, err := bus.Subscribe("e", func(ctx context.Context, env event.Envelope) error {
		atomic.AddInt32(&hits, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatalf("Publish 1: %v", err)
	}
	sub.Cancel()
	if err := bus.Publish(context.Background(), newEnv("evt-2", "e")); err != nil {
		t.Fatalf("Publish 2: %v", err)
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1", hits)
	}
	// Double cancel must be a no-op.
	sub.Cancel()
}

func TestCloseRejectsFurtherPublish(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	if err := bus.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := bus.Publish(context.Background(), newEnv("evt-1", "e"))
	if !errors.Is(err, event.ErrBusClosed) {
		t.Errorf("got %v, want ErrBusClosed", err)
	}
	// Subscribe after close should also fail.
	if _, err := bus.Subscribe("e", func(context.Context, event.Envelope) error { return nil }); !errors.Is(err, event.ErrBusClosed) {
		t.Errorf("Subscribe after Close: got %v, want ErrBusClosed", err)
	}
	// Close is idempotent.
	if err := bus.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestPublishInvalidEnvelopeReturnsError(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	defer bus.Close()
	err := bus.Publish(context.Background(), event.Envelope{Type: "e", Source: "test"}) // missing ID
	if !errors.Is(err, event.ErrInvalidEnvelope) {
		t.Errorf("got %v, want ErrInvalidEnvelope", err)
	}
}

func TestSubscribeRejectsNilHandler(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	defer bus.Close()
	if _, err := bus.Subscribe("e", nil); err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestSubscribeRejectsEmptyEventType(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	defer bus.Close()
	if _, err := bus.Subscribe("", func(context.Context, event.Envelope) error { return nil }); err == nil {
		t.Fatal("expected error for empty eventType")
	}
}

func TestHandlerErrorBubblesSync(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	defer bus.Close()

	wantErr := errors.New("boom")
	if _, err := bus.Subscribe("e", func(context.Context, event.Envelope) error { return wantErr }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	err := bus.Publish(context.Background(), newEnv("evt-1", "e"))
	if !errors.Is(err, wantErr) {
		t.Errorf("got %v, want %v", err, wantErr)
	}
}

func TestSyncContextCancellationStopsDispatch(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	defer bus.Close()

	var hits int32
	// Add 3 subscribers; cancellation happens after the first.
	if _, err := bus.Subscribe("e", func(ctx context.Context, env event.Envelope) error {
		atomic.AddInt32(&hits, 1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := bus.Subscribe("e", func(c context.Context, env event.Envelope) error {
		atomic.AddInt32(&hits, 1)
		cancel()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Subscribe("e", func(context.Context, event.Envelope) error {
		atomic.AddInt32(&hits, 1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	err := bus.Publish(ctx, newEnv("evt-1", "e"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2 (third should be skipped after cancel)", hits)
	}
}

func TestAsyncPublishReturnsImmediately(t *testing.T) {
	t.Parallel()
	bus := memory.New(memory.WithAsync(2))
	defer bus.Close()

	blocked := make(chan struct{})
	release := make(chan struct{})
	if _, err := bus.Subscribe("e", func(context.Context, event.Envelope) error {
		close(blocked)
		<-release
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	start := time.Now()
	if err := bus.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		t.Errorf("Publish took %v; async should return quickly", elapsed)
	}
	// Make sure the handler actually got dispatched.
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}
	close(release)
}

func TestAsyncHandlerErrorReachesReporter(t *testing.T) {
	t.Parallel()
	var (
		mu       sync.Mutex
		reports  []error
		reported = make(chan struct{}, 1)
	)
	wantErr := errors.New("kaboom")
	bus := memory.New(
		memory.WithAsync(1),
		memory.WithErrorReporter(func(env event.Envelope, err error) {
			mu.Lock()
			reports = append(reports, err)
			mu.Unlock()
			select {
			case reported <- struct{}{}:
			default:
			}
		}),
	)
	defer bus.Close()

	if _, err := bus.Subscribe("e", func(context.Context, event.Envelope) error { return wantErr }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-reported:
	case <-time.After(2 * time.Second):
		t.Fatal("ErrorReporter never invoked")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reports) != 1 || !errors.Is(reports[0], wantErr) {
		t.Errorf("reports = %v, want [%v]", reports, wantErr)
	}
}

func TestWithAsyncPanicsOnZero(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	memory.New(memory.WithAsync(0))
}

func TestWithAsyncQueueSizePanicsOnZero(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	memory.New(memory.WithAsyncQueueSize(0))
}

func TestCloseDrainsInFlightAsync(t *testing.T) {
	t.Parallel()
	bus := memory.New(memory.WithAsync(1))

	started := make(chan struct{})
	done := make(chan struct{})
	if _, err := bus.Subscribe("e", func(context.Context, event.Envelope) error {
		close(started)
		time.Sleep(50 * time.Millisecond)
		close(done)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Publish(context.Background(), newEnv("evt-1", "e")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	<-started
	if err := bus.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-done:
	default:
		t.Fatal("Close returned before handler completed")
	}
}

func TestPublishConcurrentSafe(t *testing.T) {
	t.Parallel()
	bus := memory.New()
	defer bus.Close()

	var hits int64
	for range 4 {
		if _, err := bus.Subscribe("e", func(context.Context, event.Envelope) error {
			atomic.AddInt64(&hits, 1)
			return nil
		}); err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
	}

	const publishers = 8
	const each = 50
	var wg sync.WaitGroup
	for range publishers {
		wg.Go(func() {
			for range each {
				if err := bus.Publish(context.Background(), newEnv("evt", "e")); err != nil {
					t.Errorf("Publish: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
	want := int64(publishers * each * 4)
	if hits != want {
		t.Errorf("hits = %d, want %d", hits, want)
	}
}
