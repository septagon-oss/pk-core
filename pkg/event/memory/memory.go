// Package memory — memory.go owns the public surface of the in-process
// event bus: the New constructor, the Option type and its constructors
// (WithAsync, WithErrorReporter), and the unexported bus and subscription
// types that satisfy event.Bus / event.Subscription.
//
// The bus is intentionally compact: a single mutex protects the
// subscriber map, an optional bounded worker pool services async dispatch,
// and a sync.WaitGroup lets Close drain in-flight handlers before
// returning.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/septagon-oss/pk-core/pkg/event"
)

// Option configures a Bus at construction time.
type Option func(*config)

// config holds the resolved Options. The fields are unexported so
// downstream code cannot mutate the bus mid-flight.
type config struct {
	asyncWorkers   int
	errorReporter  func(env event.Envelope, err error)
	asyncQueueSize int
}

// WithAsync switches the bus into async dispatch mode with workerCount
// worker goroutines. Publish returns after enqueueing; handler errors are
// forwarded to the ErrorReporter (if configured) rather than back to the
// caller. workerCount must be > 0; otherwise New returns a usable sync bus
// and ignores the option silently is NOT what we do — we panic at New time
// because misconfiguration here is a programmer error, not a runtime
// condition.
func WithAsync(workerCount int) Option {
	return func(c *config) {
		if workerCount <= 0 {
			panic(fmt.Sprintf("event/memory: WithAsync workerCount must be > 0, got %d", workerCount))
		}
		c.asyncWorkers = workerCount
		if c.asyncQueueSize == 0 {
			// Default queue depth: 16x workers gives a sane buffer without
			// hiding sustained back-pressure forever.
			c.asyncQueueSize = workerCount * 16
		}
	}
}

// WithAsyncQueueSize overrides the default async dispatch queue depth.
// Has no effect unless WithAsync is also supplied. queueSize must be > 0.
func WithAsyncQueueSize(queueSize int) Option {
	return func(c *config) {
		if queueSize <= 0 {
			panic(fmt.Sprintf("event/memory: WithAsyncQueueSize must be > 0, got %d", queueSize))
		}
		c.asyncQueueSize = queueSize
	}
}

// WithErrorReporter installs fn as the handler-error callback for async
// dispatch. fn is invoked from a worker goroutine; implementations MUST
// NOT block on it.
func WithErrorReporter(fn func(env event.Envelope, err error)) Option {
	return func(c *config) {
		c.errorReporter = fn
	}
}

// New returns an in-process event.Bus. Without options, the bus dispatches
// synchronously on the publisher's goroutine and surfaces the first
// handler error back to Publish. With WithAsync(n), Publish enqueues and
// returns; n workers drain the queue and report errors via the
// ErrorReporter if one is configured.
func New(opts ...Option) event.Bus {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}

	b := &bus{
		subscribers: make(map[string][]*subscription),
		cfg:         cfg,
	}
	if cfg.asyncWorkers > 0 {
		b.queue = make(chan asyncJob, cfg.asyncQueueSize)
		b.lifecycle, b.shutdown = context.WithCancel(context.Background())
		for i := 0; i < cfg.asyncWorkers; i++ {
			b.workersWG.Add(1)
			go b.worker()
		}
	}
	return b
}

// asyncJob is the unit a worker consumes: an Envelope + its registered
// handlers snapshot taken at Publish time so subsequent Cancel calls do
// not race the worker.
type asyncJob struct {
	env      event.Envelope
	handlers []*subscription
}

// bus is the in-process event.Bus. All fields are guarded as documented.
type bus struct {
	cfg config

	mu          sync.RWMutex
	subscribers map[string][]*subscription // eventType -> ordered list
	closed      bool

	// Async-only fields. Nil unless WithAsync was supplied.
	queue     chan asyncJob
	lifecycle context.Context
	shutdown  context.CancelFunc
	workersWG sync.WaitGroup

	// inFlight tracks sync dispatches so Close can wait for them.
	inFlight sync.WaitGroup
}

// subscription pairs a Handler with its parent bus + event type so Cancel
// can locate and remove the entry.
type subscription struct {
	bus       *bus
	eventType string
	handler   event.Handler

	cancelOnce sync.Once
}

// Cancel removes the subscription from its parent bus. Safe to call any
// number of times.
func (s *subscription) Cancel() {
	s.cancelOnce.Do(func() {
		s.bus.removeSubscription(s)
	})
}

func (b *bus) removeSubscription(target *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := b.subscribers[target.eventType]
	for i, s := range list {
		if s == target {
			b.subscribers[target.eventType] = append(list[:i], list[i+1:]...)
			return
		}
	}
}

// Publish delivers env to every subscriber registered for env.Type.
func (b *bus) Publish(ctx context.Context, env event.Envelope) error {
	if err := env.Validate(); err != nil {
		return err
	}

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return event.ErrBusClosed
	}
	// Snapshot handlers under the read lock so subscriber list mutation
	// during dispatch cannot panic or skip entries.
	handlers := append([]*subscription(nil), b.subscribers[env.Type]...)
	b.mu.RUnlock()

	if len(handlers) == 0 {
		return nil
	}

	if b.cfg.asyncWorkers > 0 {
		// Async dispatch: enqueue and return.
		select {
		case b.queue <- asyncJob{env: env, handlers: handlers}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-b.lifecycle.Done():
			return event.ErrBusClosed
		}
	}

	// Sync dispatch: run handlers on the publisher's goroutine.
	b.inFlight.Add(1)
	defer b.inFlight.Done()

	var firstErr error
	for _, s := range handlers {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.handler(ctx, env); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Subscribe registers handler for eventType.
func (b *bus) Subscribe(eventType string, handler event.Handler) (event.Subscription, error) {
	if handler == nil {
		return nil, errors.New("event/memory: Subscribe handler must not be nil")
	}
	if eventType == "" {
		return nil, errors.New("event/memory: Subscribe eventType must not be empty")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, event.ErrBusClosed
	}
	s := &subscription{bus: b, eventType: eventType, handler: handler}
	b.subscribers[eventType] = append(b.subscribers[eventType], s)
	return s, nil
}

// Close stops the bus, rejects further Publish calls, and drains any
// in-flight dispatch before returning.
func (b *bus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.mu.Unlock()

	if b.cfg.asyncWorkers > 0 {
		// Cancel the lifecycle ctx first so any Publish currently waiting
		// for queue space unblocks with ErrBusClosed.
		b.shutdown()
		// Drain workers: closing the queue tells them no more jobs are
		// coming; they exit once the queue is empty.
		close(b.queue)
		b.workersWG.Wait()
	}
	// Sync dispatch may still have handlers running; wait them out.
	b.inFlight.Wait()
	return nil
}

// worker drains the async queue and invokes handlers. Errors flow to the
// ErrorReporter (if any); a nil reporter silently drops errors so async
// dispatch never blocks on a missing sink.
func (b *bus) worker() {
	defer b.workersWG.Done()
	for job := range b.queue {
		for _, s := range job.handlers {
			// Each handler receives the lifecycle ctx so it can observe
			// bus shutdown even if it outlives the publisher.
			err := s.handler(b.lifecycle, job.env)
			if err != nil && b.cfg.errorReporter != nil {
				b.cfg.errorReporter(job.env, err)
			}
		}
	}
}
