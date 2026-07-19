// Validates: REQ-SECURITY-001.
// Per: ADR-0029.
// Discipline: C-14.

package ratelimit_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/security/ratelimit"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestNewTokenBucketRejectsZeroRate(t *testing.T) {
	t.Parallel()

	_, err := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{Rate: 0, Burst: 10})
	if !errors.Is(err, ratelimit.ErrInvalidRate) {
		t.Fatalf("expected ErrInvalidRate, got %v", err)
	}
	_, err = ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{Rate: -1, Burst: 10})
	if !errors.Is(err, ratelimit.ErrInvalidRate) {
		t.Fatalf("expected ErrInvalidRate for negative rate, got %v", err)
	}
}

func TestNewTokenBucketRejectsZeroBurst(t *testing.T) {
	t.Parallel()

	_, err := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{Rate: 10, Burst: 0})
	if !errors.Is(err, ratelimit.ErrInvalidBurst) {
		t.Fatalf("expected ErrInvalidBurst, got %v", err)
	}
	_, err = ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{Rate: 10, Burst: -3})
	if !errors.Is(err, ratelimit.ErrInvalidBurst) {
		t.Fatalf("expected ErrInvalidBurst for negative burst, got %v", err)
	}
}

func TestTokenBucketAllowsBurstThenLimits(t *testing.T) {
	t.Parallel()

	// Rate = 1 tok/sec so a burst of 5 truly drains before any refill on
	// real time. Use IdleTTL=-1 to disable reaping interference.
	tb, err := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
		Rate:    1,
		Burst:   5,
		IdleTTL: -1,
	})
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	for i := range 5 {
		allowed, _ := tb.Allow("ip-1")
		if !allowed {
			t.Fatalf("request %d within burst should be allowed", i+1)
		}
	}
	allowed, retryAfter := tb.Allow("ip-1")
	if allowed {
		t.Fatal("6th request must be denied after exhausting burst of 5")
	}
	if retryAfter <= 0 {
		t.Fatalf("retryAfter must be > 0 when denied, got %v", retryAfter)
	}
}

func TestTokenBucketRefillsOverTime(t *testing.T) {
	t.Parallel()

	// Use a 100ms-per-token rate so the test runs fast.
	tb, err := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
		Rate:    10, // 10 tokens/sec => 100ms per token
		Burst:   1,
		IdleTTL: -1,
	})
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	// First call consumes the only token.
	if allowed, _ := tb.Allow("ip-1"); !allowed {
		t.Fatal("first call should consume initial token")
	}
	// Immediate second call: denied.
	if allowed, _ := tb.Allow("ip-1"); allowed {
		t.Fatal("immediate second call must be denied with burst=1")
	}
	// Wait for refill.
	time.Sleep(150 * time.Millisecond)
	if allowed, _ := tb.Allow("ip-1"); !allowed {
		t.Fatal("after 150ms with rate=10/s the bucket should have refilled")
	}
}

func TestTokenBucketIsolatesKeys(t *testing.T) {
	t.Parallel()

	tb, err := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
		Rate:    1,
		Burst:   1,
		IdleTTL: -1,
	})
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	// Exhaust ip-1.
	if allowed, _ := tb.Allow("ip-1"); !allowed {
		t.Fatal("first ip-1 should be allowed")
	}
	if allowed, _ := tb.Allow("ip-1"); allowed {
		t.Fatal("second ip-1 must be denied")
	}
	// ip-2 has its own bucket.
	if allowed, _ := tb.Allow("ip-2"); !allowed {
		t.Fatal("ip-2 must have its own fresh bucket")
	}
}

func TestTokenBucketReportsRetryAfter(t *testing.T) {
	t.Parallel()

	// Rate = 2/sec, burst = 1: refill = 0.5s.
	tb, err := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
		Rate:    2,
		Burst:   1,
		IdleTTL: -1,
	})
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	// Consume initial token, then ask again.
	if allowed, _ := tb.Allow("k"); !allowed {
		t.Fatal("first call should consume initial token")
	}
	allowed, retryAfter := tb.Allow("k")
	if allowed {
		t.Fatal("second immediate call must be denied")
	}
	// Token deficit ~1 / rate 2 = 0.5s. Allow generous slack.
	if retryAfter < 300*time.Millisecond || retryAfter > 700*time.Millisecond {
		t.Fatalf("retryAfter ~500ms expected, got %v", retryAfter)
	}
}

func TestTokenBucketConcurrentSafe(t *testing.T) {
	t.Parallel()

	tb, err := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
		Rate:    1000,
		Burst:   50,
		IdleTTL: -1,
	})
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	const goroutines = 16
	const perG = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var allowedCount int64
	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			key := "ip-" + strconv.Itoa(id%4)
			for range perG {
				if allowed, _ := tb.Allow(key); allowed {
					atomic.AddInt64(&allowedCount, 1)
				}
			}
		}(g)
	}
	wg.Wait()
	// We only assert it ran without races (race detector flags the test
	// run if any data race fires) and made forward progress.
	if allowedCount == 0 {
		t.Fatal("expected some allowed requests under concurrent load")
	}
}

func TestClientIPKeyPrefersXForwardedFor(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	req.Header.Set("X-Real-IP", "192.0.2.1")

	got := ratelimit.ClientIPKey(req)
	if got != "203.0.113.5" {
		t.Fatalf("expected X-Forwarded-For first hop, got %q", got)
	}
}

func TestClientIPKeyFallsBackToXRealIP(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Real-IP", "192.0.2.1")

	got := ratelimit.ClientIPKey(req)
	if got != "192.0.2.1" {
		t.Fatalf("expected X-Real-IP, got %q", got)
	}
}

func TestClientIPKeyFallsBackToRemoteAddr(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.99:5555"

	got := ratelimit.ClientIPKey(req)
	if got != "10.0.0.99" {
		t.Fatalf("expected RemoteAddr host, got %q", got)
	}
}

func TestClientIPKeyHandlesIPv6(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[2001:db8::1]:443"
	got := ratelimit.ClientIPKey(req)
	if got != "2001:db8::1" {
		t.Fatalf("expected IPv6 host without brackets, got %q", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "[::1]:9999"
	req2.Header.Set("X-Forwarded-For", "[2001:db8::beef]")
	got2 := ratelimit.ClientIPKey(req2)
	if got2 != "2001:db8::beef" {
		t.Fatalf("expected IPv6 from XFF without brackets, got %q", got2)
	}
}

func TestMiddlewareAllowsUnderLimit(t *testing.T) {
	t.Parallel()

	tb, err := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
		Rate: 100, Burst: 10, IdleTTL: -1,
	})
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	mw := ratelimit.Middleware(tb, ratelimit.Config{
		KeyFunc: func(*http.Request) string { return "fixed-key" },
	})

	for i := range 5 {
		rec := httptest.NewRecorder()
		mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("under-limit request %d should be 200, got %d", i+1, rec.Code)
		}
	}
}

func TestMiddlewareReturns429AndRetryAfterOverLimit(t *testing.T) {
	t.Parallel()

	tb, err := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
		Rate: 1, Burst: 2, IdleTTL: -1,
	})
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	mw := ratelimit.Middleware(tb, ratelimit.Config{
		KeyFunc: func(*http.Request) string { return "fixed-key" },
	})

	// Drain the burst.
	for i := range 2 {
		rec := httptest.NewRecorder()
		mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("burst request %d should be 200, got %d", i+1, rec.Code)
		}
	}
	// Third should be 429.
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	retry := rec.Header().Get("Retry-After")
	if retry == "" {
		t.Fatal("expected Retry-After header on 429")
	}
	if n, err := strconv.Atoi(retry); err != nil || n < 1 {
		t.Fatalf("Retry-After should be a positive integer seconds value, got %q (%v)", retry, err)
	}
}

func TestMiddlewareSkipFuncBypassesLimiter(t *testing.T) {
	t.Parallel()

	// Limiter that always denies — proves SkipFunc bypasses entirely.
	deny := ratelimit.LimiterFunc(func(string) (bool, time.Duration) {
		return false, 5 * time.Second
	})

	mw := ratelimit.Middleware(deny, ratelimit.Config{
		KeyFunc:  func(*http.Request) string { return "k" },
		SkipFunc: func(r *http.Request) bool { return r.Header.Get("X-Internal-Monitor") == "yes" },
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Internal-Monitor", "yes")

	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("SkipFunc must bypass limiter; got %d", rec.Code)
	}

	// Without the skip header, the deny limiter rejects.
	rec2 := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("non-skipped request should hit deny limiter; got %d", rec2.Code)
	}
}

func TestMiddlewareOnLimitedCallbackFires(t *testing.T) {
	t.Parallel()

	tb, err := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
		Rate: 1, Burst: 1, IdleTTL: -1,
	})
	if err != nil {
		t.Fatalf("NewTokenBucket: %v", err)
	}

	var calls int64
	var seenRetry time.Duration
	mw := ratelimit.Middleware(tb, ratelimit.Config{
		KeyFunc: func(*http.Request) string { return "k" },
		OnLimited: func(_ *http.Request, retryAfter time.Duration) {
			atomic.AddInt64(&calls, 1)
			seenRetry = retryAfter
		},
	})

	// First request consumes the burst.
	mw(okHandler()).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	// Second is rate-limited.
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if atomic.LoadInt64(&calls) != 1 {
		t.Fatalf("OnLimited should fire exactly once on rate-limited request, got %d", atomic.LoadInt64(&calls))
	}
	if seenRetry <= 0 {
		t.Fatalf("OnLimited should receive a positive retryAfter, got %v", seenRetry)
	}
}

func TestMiddlewareDefaultKeyFuncIsClientIP(t *testing.T) {
	t.Parallel()

	// Use a LimiterFunc that records the key it saw so we can assert
	// the default keying path went through ClientIPKey.
	var seenKey atomic.Value
	seenKey.Store("")
	captureAllow := ratelimit.LimiterFunc(func(key string) (bool, time.Duration) {
		seenKey.Store(key)
		return true, 0
	})

	mw := ratelimit.Middleware(captureAllow, ratelimit.Config{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.42:1111"
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := seenKey.Load().(string); got != "198.51.100.42" {
		t.Fatalf("default KeyFunc should be ClientIPKey; saw key %q", got)
	}
}

func TestLimiterFuncSatisfiesInterface(t *testing.T) {
	t.Parallel()

	var l ratelimit.Limiter = ratelimit.LimiterFunc(func(key string) (bool, time.Duration) {
		if key == "blocked" {
			return false, 2 * time.Second
		}
		return true, 0
	})

	if allowed, retry := l.Allow("ok"); !allowed || retry != 0 {
		t.Fatalf("expected (true, 0) for ok key, got (%v, %v)", allowed, retry)
	}
	if allowed, retry := l.Allow("blocked"); allowed || retry != 2*time.Second {
		t.Fatalf("expected (false, 2s) for blocked key, got (%v, %v)", allowed, retry)
	}
}

// TestMiddlewareNilLimiterDegradesToPassthrough pins the documented
// fallback when the middleware is constructed with no Limiter.
func TestMiddlewareNilLimiterDegradesToPassthrough(t *testing.T) {
	t.Parallel()

	mw := ratelimit.Middleware(nil, ratelimit.Config{})
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("nil limiter should pass through; got %d", rec.Code)
	}
}

// TestMiddlewareUnkeyableRequestBypasses pins that an empty key from the
// KeyFunc bypasses the limiter (see ClientIPKey docs for rationale).
func TestMiddlewareUnkeyableRequestBypasses(t *testing.T) {
	t.Parallel()

	deny := ratelimit.LimiterFunc(func(string) (bool, time.Duration) {
		return false, time.Second
	})
	mw := ratelimit.Middleware(deny, ratelimit.Config{
		KeyFunc: func(*http.Request) string { return "" },
	})
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("empty key should bypass limiter; got %d", rec.Code)
	}
}
