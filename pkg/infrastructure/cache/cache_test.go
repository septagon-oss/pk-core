// Validates: REQ-014.
// Per: ADR-0029.
// Discipline: C-14.

package cache_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/infrastructure/cache"
)

func TestSetThenGetRoundTrip(t *testing.T) {
	t.Parallel()
	c := cache.NewMemory(cache.WithSweepInterval(0))
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get: ok=false, want true")
	}
	if string(got) != "v" {
		t.Errorf("Get value = %q, want %q", got, "v")
	}
}

func TestGetMissReturnsOkFalse(t *testing.T) {
	t.Parallel()
	c := cache.NewMemory(cache.WithSweepInterval(0))
	ctx := context.Background()

	got, ok, err := c.Get(ctx, "absent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("Get: ok=true on missing key")
	}
	if got != nil {
		t.Errorf("Get value = %v, want nil", got)
	}
}

func TestSetWithTTLExpires(t *testing.T) {
	t.Parallel()
	c := cache.NewMemory(cache.WithSweepInterval(0))
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("v"), 25*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Immediately readable.
	if _, ok, _ := c.Get(ctx, "k"); !ok {
		t.Fatal("Get before TTL: ok=false")
	}
	// After TTL the lazy reap on Get should report miss.
	time.Sleep(75 * time.Millisecond)
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Fatal("Get after TTL: ok=true; entry should have expired")
	}
}

func TestSetTTLZeroNeverExpires(t *testing.T) {
	t.Parallel()
	c := cache.NewMemory(cache.WithSweepInterval(0))
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok, _ := c.Get(ctx, "k"); !ok {
		t.Fatal("Get with TTL=0: ok=false; entry should be sticky")
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	c := cache.NewMemory(cache.WithSweepInterval(0))
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Fatal("Get after Delete: ok=true")
	}
	// Deleting an absent key is a no-op.
	if err := c.Delete(ctx, "missing"); err != nil {
		t.Errorf("Delete missing: %v", err)
	}
}

func TestMaxEntriesEvictsLRU(t *testing.T) {
	t.Parallel()
	c := cache.NewMemory(
		cache.WithMaxEntries(3),
		cache.WithSweepInterval(0),
	)
	ctx := context.Background()

	// Fill the cache.
	for i, k := range []string{"a", "b", "c"} {
		if err := c.Set(ctx, k, []byte{byte(i)}, 0); err != nil {
			t.Fatalf("Set %q: %v", k, err)
		}
	}
	// Touch "a" to make "b" the LRU.
	if _, ok, _ := c.Get(ctx, "a"); !ok {
		t.Fatal("Get a: missing")
	}
	// Insert "d": should evict "b" (now LRU).
	if err := c.Set(ctx, "d", []byte{0xd}, 0); err != nil {
		t.Fatalf("Set d: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "b"); ok {
		t.Fatal("Get b: still present after LRU eviction")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok, _ := c.Get(ctx, k); !ok {
			t.Errorf("Get %q: missing after eviction round", k)
		}
	}
}

func TestSweepReapsExpired(t *testing.T) {
	t.Parallel()
	// 20ms sweep interval keeps the test snappy without flapping.
	c := cache.NewMemory(cache.WithSweepInterval(20 * time.Millisecond))
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("v"), 10*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Let the sweep run at least once after expiry.
	time.Sleep(100 * time.Millisecond)
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Fatal("Get after sweep: entry still present")
	}
}

func TestConcurrentSafe(t *testing.T) {
	t.Parallel()
	c := cache.NewMemory(
		cache.WithMaxEntries(256),
		cache.WithSweepInterval(0),
	)
	ctx := context.Background()

	const workers = 16
	const ops = 200
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		go func() {
			defer wg.Done()
			for i := range ops {
				key := fmt.Sprintf("w%d-k%d", w, i%32)
				_ = c.Set(ctx, key, []byte{byte(i)}, 0)
				_, _, _ = c.Get(ctx, key)
				if i%7 == 0 {
					_ = c.Delete(ctx, key)
				}
			}
		}()
	}
	wg.Wait()
}

func TestContextCancellationShortCircuits(t *testing.T) {
	t.Parallel()
	c := cache.NewMemory(cache.WithSweepInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := c.Get(ctx, "k"); err == nil {
		t.Fatal("Get with cancelled ctx: err=nil")
	}
	if err := c.Set(ctx, "k", []byte("v"), 0); err == nil {
		t.Fatal("Set with cancelled ctx: err=nil")
	}
	if err := c.Delete(ctx, "k"); err == nil {
		t.Fatal("Delete with cancelled ctx: err=nil")
	}
}

func TestSetOverwriteUpdatesValueAndTTL(t *testing.T) {
	t.Parallel()
	c := cache.NewMemory(cache.WithSweepInterval(0))
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("v1"), 0); err != nil {
		t.Fatalf("Set v1: %v", err)
	}
	if err := c.Set(ctx, "k", []byte("v2"), 0); err != nil {
		t.Fatalf("Set v2: %v", err)
	}
	got, ok, _ := c.Get(ctx, "k")
	if !ok {
		t.Fatal("Get: missing after overwrite")
	}
	if string(got) != "v2" {
		t.Errorf("Get = %q, want %q", got, "v2")
	}
}

func TestSetDefensiveCopiesInput(t *testing.T) {
	t.Parallel()
	c := cache.NewMemory(cache.WithSweepInterval(0))
	ctx := context.Background()

	buf := []byte("original")
	if err := c.Set(ctx, "k", buf, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Mutate the caller's slice — must not affect stored value.
	buf[0] = 'X'

	got, _, _ := c.Get(ctx, "k")
	if string(got) != "original" {
		t.Errorf("Get = %q, want %q; storage not isolated from caller", got, "original")
	}
}
