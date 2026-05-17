// Package cookies — cookies_test.go is the contract test suite for the
// cookies package. Each test exercises one observable property of the
// composable contract: profile fidelity, Secure-flag derivation, clearing
// semantics, error sentinels, override behavior, name round-trip, and
// race-freedom under concurrent Configure + Build.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cookies

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// resetSettings clears any package-level Settings between tests. Without
// it, a Configure call in one test leaks into another.
func resetSettings(t *testing.T) {
	t.Helper()
	Configure(Settings{})
}

func TestBuildSession(t *testing.T) {
	resetSettings(t)
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	r.TLS = &tls.ConnectionState{}

	c, err := Build(r, KindSession, "tok")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c.Name != "session" {
		t.Errorf("Name = %q, want %q", c.Name, "session")
	}
	if c.Value != "tok" {
		t.Errorf("Value = %q, want %q", c.Value, "tok")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true")
	}
	if !c.Secure {
		t.Error("Secure = false, want true (TLS request)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want %q", c.Path, "/")
	}
	if c.MaxAge != int(DefaultSessionMaxAge.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(DefaultSessionMaxAge.Seconds()))
	}
}

func TestBuildCSRFIsNotHttpOnly(t *testing.T) {
	resetSettings(t)
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	c, err := Build(r, KindCSRF, "csrf-tok")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c.HttpOnly {
		t.Error("KindCSRF HttpOnly = true, want false (JS must read it)")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("KindCSRF SameSite = %v, want Strict", c.SameSite)
	}
	if c.Name != "_csrf" {
		t.Errorf("KindCSRF Name = %q, want %q", c.Name, "_csrf")
	}
}

func TestSecureDerivedFromTLS(t *testing.T) {
	resetSettings(t)
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	r.TLS = &tls.ConnectionState{}
	c, err := Build(r, KindSession, "v")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !c.Secure {
		t.Error("Secure = false, want true when r.TLS != nil")
	}
}

func TestSecureDerivedFromForwardedProto(t *testing.T) {
	resetSettings(t)
	r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	c, err := Build(r, KindSession, "v")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !c.Secure {
		t.Error("Secure = false, want true when X-Forwarded-Proto=https")
	}
}

func TestForceSecureOverride(t *testing.T) {
	resetSettings(t)
	Configure(Settings{ForceSecure: true})
	t.Cleanup(func() { resetSettings(t) })

	r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	c, err := Build(r, KindSession, "v")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !c.Secure {
		t.Error("Secure = false, want true under Settings.ForceSecure")
	}
}

func TestForceSecureWithNilRequest(t *testing.T) {
	resetSettings(t)
	Configure(Settings{ForceSecure: true})
	t.Cleanup(func() { resetSettings(t) })

	c, err := Build(nil, KindSession, "v")
	if err != nil {
		t.Fatalf("Build(nil): %v", err)
	}
	if !c.Secure {
		t.Error("Secure = false, want true when r is nil but ForceSecure is set")
	}
}

func TestBuildClearSetsMaxAgeNegative(t *testing.T) {
	resetSettings(t)
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	c, err := BuildClear(r, KindSession)
	if err != nil {
		t.Fatalf("BuildClear: %v", err)
	}
	if c.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", c.MaxAge)
	}
	if c.Value != "" {
		t.Errorf("Value = %q, want empty", c.Value)
	}
	if c.Name != "session" {
		t.Errorf("Name = %q, want %q", c.Name, "session")
	}
}

func TestBuildUnknownKindErrors(t *testing.T) {
	resetSettings(t)
	_, err := Build(nil, Kind(999), "v")
	if err == nil {
		t.Fatal("Build(unknown): err = nil, want ErrUnknownKind")
	}
	if !errors.Is(err, ErrUnknownKind) {
		t.Errorf("err = %v, want errors.Is(err, ErrUnknownKind) == true", err)
	}
}

func TestBuildClearUnknownKindErrors(t *testing.T) {
	resetSettings(t)
	_, err := BuildClear(nil, Kind(999))
	if !errors.Is(err, ErrUnknownKind) {
		t.Errorf("err = %v, want ErrUnknownKind", err)
	}
}

func TestBuildZeroKindErrors(t *testing.T) {
	resetSettings(t)
	// The zero-value Kind has no profile and must error rather than ship
	// a default-but-incorrect profile.
	var k Kind
	if _, err := Build(nil, k, "v"); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("zero Kind err = %v, want ErrUnknownKind", err)
	}
}

func TestWithMaxAgeOverride(t *testing.T) {
	resetSettings(t)
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	c, err := Build(r, KindSession, "v", WithMaxAge(10*time.Minute))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c.MaxAge != 600 {
		t.Errorf("MaxAge = %d, want 600", c.MaxAge)
	}
}

func TestNameRoundTrip(t *testing.T) {
	resetSettings(t)
	cases := map[Kind]string{
		KindSession:        "session",
		KindCSRF:           "_csrf",
		KindPreviewSession: "preview_session",
		KindTenantPin:      "tenant_id",
	}
	for k, want := range cases {
		got, err := Name(k)
		if err != nil {
			t.Errorf("Name(%d): %v", k, err)
			continue
		}
		if got != want {
			t.Errorf("Name(%d) = %q, want %q", k, got, want)
		}
	}
	if _, err := Name(Kind(999)); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("Name(unknown) err = %v, want ErrUnknownKind", err)
	}
}

func TestWriteEmitsHeader(t *testing.T) {
	resetSettings(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err := Write(rec, r, KindSession, "v"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "v" {
		t.Errorf("emitted cookies = %+v, want one session=v cookie", cookies)
	}
}

func TestWriteUnknownKindReturnsError(t *testing.T) {
	resetSettings(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	err := Write(rec, r, Kind(999), "v")
	if !errors.Is(err, ErrUnknownKind) {
		t.Errorf("Write(unknown) err = %v, want ErrUnknownKind", err)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("Write returned an error but still emitted a Set-Cookie header")
	}
}

func TestClearEmitsExpiringHeader(t *testing.T) {
	resetSettings(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err := Clear(rec, r, KindSession); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if cookies[0].MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", cookies[0].MaxAge)
	}
}

func TestSettingsDomainPropagates(t *testing.T) {
	resetSettings(t)
	Configure(Settings{CookieDomain: "example.com"})
	t.Cleanup(func() { resetSettings(t) })

	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	c, err := Build(r, KindSession, "v")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c.Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", c.Domain, "example.com")
	}
}

func TestSettingsMaxAgeOverridesDefault(t *testing.T) {
	resetSettings(t)
	Configure(Settings{SessionMaxAge: 5 * time.Minute})
	t.Cleanup(func() { resetSettings(t) })

	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	c, err := Build(r, KindSession, "v")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c.MaxAge != 300 {
		t.Errorf("MaxAge = %d, want 300 (Settings override)", c.MaxAge)
	}
}

// TestConfigureIsRaceFree drives concurrent Configure + Build to exercise
// the atomic.Pointer storage. Run with `go test -race` to surface any
// non-atomic access.
func TestConfigureIsRaceFree(t *testing.T) {
	resetSettings(t)
	t.Cleanup(func() { resetSettings(t) })

	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				Configure(Settings{
					ForceSecure:   i%2 == 0,
					SessionMaxAge: time.Duration(i+1) * time.Minute,
				})
			}
		}(i)
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5000; j++ {
				c, err := Build(r, KindSession, "v")
				if err != nil {
					t.Errorf("Build: %v", err)
					return
				}
				_ = c.Secure
				_ = c.MaxAge
			}
		}()
	}

	// Let writers and readers run a beat, then signal stop.
	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()
}
