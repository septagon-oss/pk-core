// Validates: REQ-RESILIENCE-001.
// Per: ADR-0029.
// Discipline: C-14.

package bulkhead_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/resilience/bulkhead"
)

func TestNewRejectsZeroMaxConcurrent(t *testing.T) {
	t.Parallel()
	if _, err := bulkhead.New(bulkhead.Config{MaxConcurrent: 0}); err == nil {
		t.Fatal("New with MaxConcurrent=0: want error, got nil")
	}
	if _, err := bulkhead.New(bulkhead.Config{MaxConcurrent: -1}); err == nil {
		t.Fatal("New with MaxConcurrent=-1: want error, got nil")
	}
	if _, err := bulkhead.New(bulkhead.Config{MaxConcurrent: 1, MaxQueue: -1}); err == nil {
		t.Fatal("New with MaxQueue=-1: want error, got nil")
	}
	if _, err := bulkhead.New(bulkhead.Config{MaxConcurrent: 1, AcquireTimeout: -time.Second}); err == nil {
		t.Fatal("New with negative AcquireTimeout: want error, got nil")
	}
}

func TestNewAcceptsZeroMaxQueue(t *testing.T) {
	t.Parallel()
	bh, err := bulkhead.New(bulkhead.Config{MaxConcurrent: 1, MaxQueue: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Single fast call should succeed; this proves zero-queue is a valid
	// configuration, not an off-by-one error.
	err = bh.Do(context.Background(), func(_ context.Context) error { return nil })
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestDoAllowsUnderLimit(t *testing.T) {
	t.Parallel()
	bh, err := bulkhead.New(bulkhead.Config{MaxConcurrent: 3, MaxQueue: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i := range 10 {
		err := bh.Do(context.Background(), func(_ context.Context) error { return nil })
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}

func TestDoBlocksOverLimit(t *testing.T) {
	t.Parallel()
	bh, err := bulkhead.New(bulkhead.Config{MaxConcurrent: 1, MaxQueue: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hold := make(chan struct{})
	released := make(chan struct{})
	go func() {
		_ = bh.Do(context.Background(), func(_ context.Context) error {
			<-hold
			return nil
		})
		close(released)
	}()
	// Give the first goroutine a chance to acquire the slot.
	for i := 0; i < 100 && bh.InFlight() == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	if bh.InFlight() != 1 {
		t.Fatalf("InFlight = %d, want 1", bh.InFlight())
	}

	// Start a second caller. It should NOT run until we release `hold`.
	secondRan := make(chan struct{})
	go func() {
		_ = bh.Do(context.Background(), func(_ context.Context) error {
			close(secondRan)
			return nil
		})
	}()

	select {
	case <-secondRan:
		t.Fatal("second op ran before first released the slot")
	case <-time.After(30 * time.Millisecond):
	}

	close(hold)
	<-released
	select {
	case <-secondRan:
	case <-time.After(time.Second):
		t.Fatal("second op never ran after slot released")
	}
}

func TestDoReturnsErrFullOverQueue(t *testing.T) {
	t.Parallel()
	bh, err := bulkhead.New(bulkhead.Config{MaxConcurrent: 1, MaxQueue: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hold := make(chan struct{})
	// Caller 1: holds the slot.
	go func() {
		_ = bh.Do(context.Background(), func(_ context.Context) error {
			<-hold
			return nil
		})
	}()
	for i := 0; i < 100 && bh.InFlight() == 0; i++ {
		time.Sleep(time.Millisecond)
	}

	// Caller 2: queues (MaxQueue=1).
	queued := make(chan struct{})
	go func() {
		close(queued)
		_ = bh.Do(context.Background(), func(_ context.Context) error { return nil })
	}()
	<-queued
	time.Sleep(20 * time.Millisecond) // let caller 2 enter the waiting state

	// Caller 3: should fail fast with ErrFull.
	err = bh.Do(context.Background(), func(_ context.Context) error {
		t.Fatal("op should not run when queue is full")
		return nil
	})
	if !errors.Is(err, bulkhead.ErrFull) {
		t.Fatalf("err = %v, want ErrFull", err)
	}

	close(hold)
}

func TestAcquireTimeoutRespectsContextCancellation(t *testing.T) {
	t.Parallel()
	bh, err := bulkhead.New(bulkhead.Config{
		MaxConcurrent:  1,
		MaxQueue:       5,
		AcquireTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hold := make(chan struct{})
	go func() {
		_ = bh.Do(context.Background(), func(_ context.Context) error {
			<-hold
			return nil
		})
	}()
	for i := 0; i < 100 && bh.InFlight() == 0; i++ {
		time.Sleep(time.Millisecond)
	}

	// Context cancellation should take precedence over AcquireTimeout.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err = bh.Do(ctx, func(_ context.Context) error {
		t.Fatal("op should not run after ctx cancelled before acquire")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("ctx cancel took %v, expected near-immediate", took)
	}
	close(hold)
}

func TestAcquireTimeoutReturnsErrFull(t *testing.T) {
	t.Parallel()
	bh, err := bulkhead.New(bulkhead.Config{
		MaxConcurrent:  1,
		MaxQueue:       5,
		AcquireTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hold := make(chan struct{})
	go func() {
		_ = bh.Do(context.Background(), func(_ context.Context) error {
			<-hold
			return nil
		})
	}()
	for i := 0; i < 100 && bh.InFlight() == 0; i++ {
		time.Sleep(time.Millisecond)
	}

	start := time.Now()
	err = bh.Do(context.Background(), func(_ context.Context) error {
		t.Fatal("op should not run after AcquireTimeout")
		return nil
	})
	if !errors.Is(err, bulkhead.ErrFull) {
		t.Fatalf("err = %v, want ErrFull", err)
	}
	if took := time.Since(start); took < 15*time.Millisecond {
		t.Fatalf("AcquireTimeout returned too fast: %v", took)
	}
	close(hold)
}

func TestInFlightTracksCorrectly(t *testing.T) {
	t.Parallel()
	bh, err := bulkhead.New(bulkhead.Config{MaxConcurrent: 5, MaxQueue: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if bh.InFlight() != 0 {
		t.Fatalf("initial InFlight = %d, want 0", bh.InFlight())
	}

	hold := make(chan struct{})
	var wg sync.WaitGroup
	const n = 3
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_ = bh.Do(context.Background(), func(_ context.Context) error {
				<-hold
				return nil
			})
		}()
	}
	// Wait for all 3 to acquire.
	for i := 0; i < 100 && bh.InFlight() < n; i++ {
		time.Sleep(time.Millisecond)
	}
	if bh.InFlight() != n {
		t.Fatalf("InFlight = %d, want %d", bh.InFlight(), n)
	}
	close(hold)
	wg.Wait()
	if bh.InFlight() != 0 {
		t.Fatalf("post-release InFlight = %d, want 0", bh.InFlight())
	}
}

func TestBulkheadConcurrentSafe(t *testing.T) {
	t.Parallel()
	const maxConcurrent = 4
	bh, err := bulkhead.New(bulkhead.Config{
		MaxConcurrent: maxConcurrent,
		MaxQueue:      1000,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var inFlight atomic.Int32
	var peak atomic.Int32
	var wg sync.WaitGroup
	const total = 100
	wg.Add(total)
	for range total {
		go func() {
			defer wg.Done()
			_ = bh.Do(context.Background(), func(_ context.Context) error {
				cur := inFlight.Add(1)
				for {
					p := peak.Load()
					if cur <= p || peak.CompareAndSwap(p, cur) {
						break
					}
				}
				// Hold the slot briefly so concurrency overlaps.
				time.Sleep(time.Millisecond)
				inFlight.Add(-1)
				return nil
			})
		}()
	}
	wg.Wait()
	if got := peak.Load(); got > maxConcurrent {
		t.Fatalf("peak simultaneous = %d, want <= %d", got, maxConcurrent)
	}
	if peak.Load() == 0 {
		t.Fatal("peak simultaneous = 0; concurrency test never overlapped")
	}
}
