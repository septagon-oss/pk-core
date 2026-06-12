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

// ErrorHandler observes dispatcher-side failures (ADR-0049 D6). op is
// one of "claim", "next_batch", "publish", "mark_delivered",
// "mark_failed", "dead_letter"; envelopeID is empty for batch-level
// failures. The previous dispatcher swallowed every error silently —
// an unreachable bus produced zero operator signal.
type ErrorHandler func(ctx context.Context, op string, envelopeID string, err error)

type config struct {
	interval   time.Duration
	batchSize  int
	maxRetries int
	claimTTL   time.Duration
	onError    ErrorHandler
}

func defaultConfig() config {
	return config{
		interval:  100 * time.Millisecond,
		batchSize: 64,
		// Bounded by default (ADR-0049 D6): unlimited retries turn one
		// poison entry into a permanent per-pass tax with no terminal
		// state. Use WithMaxRetries(0) to restore unlimited explicitly.
		maxRetries: 25,
		claimTTL:   30 * time.Second,
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
// dispatcher gives up on an entry (dead-lettering it when the Store
// supports DeadLetterStore). 0 means unlimited retries — an explicit
// opt-in since ADR-0049 D6 made the default bounded.
func WithMaxRetries(n int) Option {
	return func(c *config) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// WithClaimTTL sets how long a dispatcher's claim on an entry excludes
// it from other dispatchers (ClaimingStore only). Must comfortably
// exceed one delivery attempt's worst case. Must be > 0.
func WithClaimTTL(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.claimTTL = d
		}
	}
}

// WithErrorHandler installs an observer for dispatcher-side failures.
// Without one, failures remain silent (legacy behavior) — production
// deployments SHOULD wire this to their logger/metrics.
func WithErrorHandler(h ErrorHandler) Option {
	return func(c *config) {
		c.onError = h
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

	// claimUnavailable latches ONLY when the very first ClaimBatch call
	// fails — the signature of a backing schema that predates the claim
	// column (existing deployments that ran the old SchemaSQL). The
	// dispatcher then falls back to NextBatch — single-dispatcher
	// semantics — instead of stalling forever, reporting the downgrade
	// via onError. Once ClaimBatch has succeeded even once, later
	// errors are treated as TRANSIENT: the pass stalls and retries,
	// because permanently downgrading a multi-dispatcher deployment to
	// unclaimed reads over a network blip would silently reintroduce
	// double delivery (council finding, Track D review).
	claimProbed      bool
	claimUnavailable bool

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

// report forwards a dispatcher-side failure to the configured
// ErrorHandler; without one, failures stay silent (legacy behavior).
func (o *Outbox) report(ctx context.Context, op, envelopeID string, err error) {
	if o.cfg.onError != nil && err != nil {
		o.cfg.onError(ctx, op, envelopeID, err)
	}
}

// fetchBatch claims a batch when the store supports it. A failure on
// the FIRST ever ClaimBatch call downgrades to NextBatch permanently
// (pre-claim schema); a failure after at least one success is treated
// as transient and stalls the pass. See Outbox.claimUnavailable.
func (o *Outbox) fetchBatch(ctx context.Context) ([]PendingEntry, error) {
	claimer, ok := o.store.(ClaimingStore)
	if !ok || o.claimUnavailable {
		return o.store.NextBatch(ctx, o.cfg.batchSize)
	}
	entries, err := claimer.ClaimBatch(ctx, o.cfg.batchSize, o.cfg.claimTTL)
	if err == nil {
		o.claimProbed = true
		return entries, nil
	}
	if !o.claimProbed {
		// First-ever call failed: assume the schema lacks the claim
		// column and fall back for the Outbox's lifetime.
		o.claimUnavailable = true
		o.report(ctx, "claim", "", err)
		return o.store.NextBatch(ctx, o.cfg.batchSize)
	}
	// Claiming has worked before — this is transient. Stall the pass
	// rather than silently downgrade to double-delivery semantics.
	return nil, err
}

// dispatchOnce performs a single batch pass: fetch (claiming when
// supported) -> for each entry inner.Publish -> MarkDelivered,
// MarkFailed, or — past maxRetries — MarkDead (ADR-0049 D6).
func (o *Outbox) dispatchOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	entries, err := o.fetchBatch(ctx)
	if err != nil {
		// A fetch error stalls this pass; the next tick will retry.
		o.report(ctx, "next_batch", "", err)
		return
	}
	deadLetterer, canDeadLetter := o.store.(DeadLetterStore)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if o.cfg.maxRetries > 0 && entry.Attempts >= o.cfg.maxRetries {
			const exhausted = "event/outbox: max retries exceeded"
			if canDeadLetter {
				if err := deadLetterer.MarkDead(ctx, entry.Envelope.ID, exhausted); err != nil {
					o.report(ctx, "mark_failed", entry.Envelope.ID, err)
					continue
				}
				o.report(ctx, "dead_letter", entry.Envelope.ID, errors.New(exhausted))
				continue
			}
			// Legacy stores without a dead-letter state: preserve the
			// old remove-from-queue behavior so the queue cannot wedge.
			if err := o.store.MarkFailed(ctx, entry.Envelope.ID, exhausted); err != nil {
				o.report(ctx, "mark_failed", entry.Envelope.ID, err)
			}
			if err := o.store.MarkDelivered(ctx, entry.Envelope.ID); err != nil {
				o.report(ctx, "mark_delivered", entry.Envelope.ID, err)
			}
			o.report(ctx, "dead_letter", entry.Envelope.ID, errors.New(exhausted))
			continue
		}
		if err := o.inner.Publish(ctx, entry.Envelope); err != nil {
			o.report(ctx, "publish", entry.Envelope.ID, err)
			if err := o.store.MarkFailed(ctx, entry.Envelope.ID, err.Error()); err != nil {
				o.report(ctx, "mark_failed", entry.Envelope.ID, err)
			}
			continue
		}
		if err := o.store.MarkDelivered(ctx, entry.Envelope.ID); err != nil {
			o.report(ctx, "mark_delivered", entry.Envelope.ID, err)
		}
	}
}
