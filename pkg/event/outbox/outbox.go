// Package outbox — outbox.go owns the Outbox struct + Option type + its
// public methods (Publish, Subscribe, Close, Start, Stop). The dispatcher
// loop runs in a goroutine launched by Start; it reads NextBatch from the
// Store, forwards each Envelope to the inner event.Bus, and records the
// outcome via MarkDelivered / MarkFailed.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package outbox

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/septagon-oss/pk-core/pkg/event"
)

// Option configures an Outbox at construction time.
type Option func(*config)

type config struct {
	interval   time.Duration
	batchSize  int
	maxRetries int
}

func defaultConfig() config {
	return config{
		interval:   100 * time.Millisecond,
		batchSize:  64,
		maxRetries: 0, // 0 = unlimited; the dispatcher retries on every pass
	}
}

// WithDispatchInterval sets how often the dispatcher checks the Store for
// new pending entries. Smaller values reduce latency; larger values
// reduce store load. Must be > 0.
func WithDispatchInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.interval = d
		}
	}
}

// WithBatchSize caps the number of entries the dispatcher reads from the
// Store per pass. Must be > 0.
func WithBatchSize(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.batchSize = n
		}
	}
}

// WithMaxRetries caps the number of delivery attempts before the
// dispatcher gives up on an entry. 0 (default) means unlimited retries.
func WithMaxRetries(n int) Option {
	return func(c *config) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// Outbox wraps an inner event.Bus with a durable Store. Publish writes to
// the Store; Start launches a dispatcher goroutine that forwards stored
// envelopes to the inner Bus.
//
// Outbox satisfies event.Bus so callers can swap in/out without changing
// dependent code.
type Outbox struct {
	inner event.Bus
	store Store
	cfg   config

	mu        sync.Mutex
	started   bool
	stopped   bool
	lifecycle context.Context
	cancel    context.CancelFunc
	doneCh    chan struct{}
}

// New constructs an Outbox. Call Start to launch the dispatcher and Stop
// to halt it gracefully.
func New(inner event.Bus, store Store, opts ...Option) *Outbox {
	if inner == nil {
		panic("event/outbox: New requires non-nil inner Bus")
	}
	if store == nil {
		panic("event/outbox: New requires non-nil Store")
	}
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Outbox{
		inner: inner,
		store: store,
		cfg:   cfg,
	}
}

// Publish validates env, then persists it via the Store. If env has an
// IdempotencyKey and a prior entry already used it, Publish returns nil
// (silent dedupe) — callers who care can use Store.Save directly and
// branch on ErrDuplicate.
func (o *Outbox) Publish(ctx context.Context, env event.Envelope) error {
	if err := env.Validate(); err != nil {
		return err
	}
	o.mu.Lock()
	stopped := o.stopped
	o.mu.Unlock()
	if stopped {
		return event.ErrBusClosed
	}
	err := o.store.Save(ctx, env)
	if errors.Is(err, ErrDuplicate) {
		return nil
	}
	return err
}

// Subscribe forwards to the inner Bus. The Outbox itself does not retain
// subscriber state; subscribers register with the underlying bus.
func (o *Outbox) Subscribe(eventType string, handler event.Handler) (event.Subscription, error) {
	return o.inner.Subscribe(eventType, handler)
}

// Close stops the dispatcher (if running) and closes the inner Bus.
func (o *Outbox) Close() error {
	o.Stop()
	return o.inner.Close()
}

// Start launches the dispatcher goroutine. It runs until ctx is cancelled
// or Stop is called. Start is idempotent: calling it twice has no effect.
func (o *Outbox) Start(ctx context.Context) {
	o.mu.Lock()
	if o.started || o.stopped {
		o.mu.Unlock()
		return
	}
	o.started = true
	// Derive a lifecycle ctx that Stop can cancel independently of the
	// caller's ctx — the dispatcher belongs to the Outbox lifetime, not
	// the request that called Start.
	o.lifecycle, o.cancel = context.WithCancel(ctx)
	o.doneCh = make(chan struct{})
	o.mu.Unlock()

	go o.dispatchLoop()
}

// Stop halts the dispatcher and waits for the goroutine to exit. Stop is
// idempotent.
func (o *Outbox) Stop() {
	o.mu.Lock()
	if o.stopped {
		o.mu.Unlock()
		return
	}
	o.stopped = true
	cancel := o.cancel
	doneCh := o.doneCh
	o.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if doneCh != nil {
		<-doneCh
	}
}

// dispatchLoop is the dispatcher goroutine entry point. It polls the
// Store every cfg.interval, forwards each entry to the inner Bus, and
// records the outcome.
func (o *Outbox) dispatchLoop() {
	defer close(o.doneCh)

	ticker := time.NewTicker(o.cfg.interval)
	defer ticker.Stop()

	// Run one immediate pass so callers don't wait an interval for the
	// first dispatch after Start.
	o.dispatchOnce(o.lifecycle)

	for {
		select {
		case <-o.lifecycle.Done():
			return
		case <-ticker.C:
			o.dispatchOnce(o.lifecycle)
		}
	}
}

// dispatchOnce performs a single batch pass: NextBatch -> for each entry
// inner.Publish -> MarkDelivered or MarkFailed.
func (o *Outbox) dispatchOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	entries, err := o.store.NextBatch(ctx, o.cfg.batchSize)
	if err != nil {
		// A NextBatch error stalls this pass; the next tick will retry.
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if o.cfg.maxRetries > 0 && entry.Attempts >= o.cfg.maxRetries {
			// Give up: mark delivered to remove from pending queue, with
			// a final failure marker recorded.
			_ = o.store.MarkFailed(ctx, entry.Envelope.ID, "event/outbox: max retries exceeded")
			_ = o.store.MarkDelivered(ctx, entry.Envelope.ID)
			continue
		}
		if err := o.inner.Publish(ctx, entry.Envelope); err != nil {
			_ = o.store.MarkFailed(ctx, entry.Envelope.ID, err.Error())
			continue
		}
		_ = o.store.MarkDelivered(ctx, entry.Envelope.ID)
	}
}
