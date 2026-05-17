// Package cookies_test — cookies_test.go is the contract test suite for the
// cookies package. Each test exercises one observable property of the
// composable contract: profile fidelity, Secure-flag derivation, clearing
// semantics, error sentinels, override behavior, name round-trip, and
// race-freedom under concurrent Configure + Build.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cookies_test

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/security/cookies"
)

var customKindSeq atomic.Uint64

func uniqueCookieName(prefix string) string {
	return fmt.Sprintf("test_%s_%d", prefix, customKindSeq.Add(1))
}

// resetSettings clears any package-level Settings between tests. Without
// it, a Configure call in one test leaks into another.
func resetSettings(t *testing.T) {
	t.Helper()
	cookies.Configure(cookies.Settings{})
}

func TestBuildSession(t *testing.T) {
	resetSettings(t)
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	r.TLS = &tls.ConnectionState{}

	c, err := cookies.Build(r, cookies.KindSession, "tok")
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
	if c.MaxAge != int(cookies.DefaultSessionMaxAge.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(cookies.DefaultSessionMaxAge.Seconds()))
	}
}

func TestBuildCSRFIsNotHttpOnly(t *testing.T) {
	resetSettings(t)
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	c, err := cookies.Build(r, cookies.KindCSRF, "csrf-tok")
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
	c, err := cookies.Build(r, cookies.KindSession, "v")
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
	c, err := cookies.Build(r, cookies.KindSession, "v")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !c.Secure {
		t.Error("Secure = false, want true when X-Forwarded-Proto=https")
	}
}

func TestForceSecureOverride(t *testing.T) {
	resetSettings(t)
	cookies.Configure(cookies.Settings{ForceSecure: true})
	t.Cleanup(func() { resetSettings(t) })

	r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	c, err := cookies.Build(r, cookies.KindSession, "v")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !c.Secure {
		t.Error("Secure = false, want true under Settings.ForceSecure")
	}
}

func TestForceSecureWithNilRequest(t *testing.T) {
	resetSettings(t)
	cookies.Configure(cookies.Settings{ForceSecure: true})
	t.Cleanup(func() { resetSettings(t) })

	c, err := cookies.Build(nil, cookies.KindSession, "v")
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
	c, err := cookies.BuildClear(r, cookies.KindSession)
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
	_, err := cookies.Build(nil, cookies.Kind(999), "v")
	if err == nil {
		t.Fatal("Build(unknown): err = nil, want ErrUnknownKind")
	}
	if !errors.Is(err, cookies.ErrUnknownKind) {
		t.Errorf("err = %v, want errors.Is(err, ErrUnknownKind) == true", err)
	}
}

func TestBuildClearUnknownKindErrors(t *testing.T) {
	resetSettings(t)
	_, err := cookies.BuildClear(nil, cookies.Kind(999))
	if !errors.Is(err, cookies.ErrUnknownKind) {
		t.Errorf("err = %v, want ErrUnknownKind", err)
	}
}

func TestBuildZeroKindErrors(t *testing.T) {
	resetSettings(t)
	// The zero-value Kind has no profile and must error rather than ship
	// a default-but-incorrect profile.
	var k cookies.Kind
	if _, err := cookies.Build(nil, k, "v"); !errors.Is(err, cookies.ErrUnknownKind) {
		t.Errorf("zero Kind err = %v, want ErrUnknownKind", err)
	}
}

func TestWithMaxAgeOverride(t *testing.T) {
	resetSettings(t)
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	c, err := cookies.Build(r, cookies.KindSession, "v", cookies.WithMaxAge(10*time.Minute))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c.MaxAge != 600 {
		t.Errorf("MaxAge = %d, want 600", c.MaxAge)
	}
}

func TestNameRoundTrip(t *testing.T) {
	resetSettings(t)
	cases := map[cookies.Kind]string{
		cookies.KindSession:        "session",
		cookies.KindCSRF:           "_csrf",
		cookies.KindPreviewSession: "preview_session",
		cookies.KindTenantPin:      "tenant_id",
	}
	for k, want := range cases {
		got, err := cookies.Name(k)
		if err != nil {
			t.Errorf("Name(%d): %v", k, err)
			continue
		}
		if got != want {
			t.Errorf("Name(%d) = %q, want %q", k, got, want)
		}
	}
	if _, err := cookies.Name(cookies.Kind(999)); !errors.Is(err, cookies.ErrUnknownKind) {
		t.Errorf("Name(unknown) err = %v, want ErrUnknownKind", err)
	}
}

func TestWriteEmitsHeader(t *testing.T) {
	resetSettings(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err := cookies.Write(rec, r, cookies.KindSession, "v"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	c := rec.Result().Cookies()
	if len(c) != 1 || c[0].Name != "session" || c[0].Value != "v" {
		t.Errorf("emitted cookies = %+v, want one session=v cookie", c)
	}
}

func TestWriteUnknownKindReturnsError(t *testing.T) {
	resetSettings(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	err := cookies.Write(rec, r, cookies.Kind(999), "v")
	if !errors.Is(err, cookies.ErrUnknownKind) {
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
	if err := cookies.Clear(rec, r, cookies.KindSession); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	c := rec.Result().Cookies()
	if len(c) != 1 {
		t.Fatalf("got %d cookies, want 1", len(c))
	}
	if c[0].MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", c[0].MaxAge)
	}
}

func TestSettingsDomainPropagates(t *testing.T) {
	resetSettings(t)
	cookies.Configure(cookies.Settings{CookieDomain: "example.com"})
	t.Cleanup(func() { resetSettings(t) })

	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	c, err := cookies.Build(r, cookies.KindSession, "v")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c.Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", c.Domain, "example.com")
	}
}

func TestSettingsMaxAgeOverridesDefault(t *testing.T) {
	resetSettings(t)
	cookies.Configure(cookies.Settings{SessionMaxAge: 5 * time.Minute})
	t.Cleanup(func() { resetSettings(t) })

	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	c, err := cookies.Build(r, cookies.KindSession, "v")
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
				cookies.Configure(cookies.Settings{
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
				c, err := cookies.Build(r, cookies.KindSession, "v")
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

func TestRegisterKindAllocatesNewKind(t *testing.T) {
	t.Parallel()
	name := uniqueCookieName("oauth_state")
	profile := cookies.Profile{
		Name:          name,
		HttpOnly:      true,
		SameSite:      http.SameSiteLaxMode,
		Path:          "/",
		DefaultMaxAge: 10 * time.Minute,
	}
	kind, err := cookies.RegisterKind(profile)
	if err != nil {
		t.Fatalf("RegisterKind: %v", err)
	}
	registeredName, err := cookies.Name(kind)
	if err != nil {
		t.Fatalf("Name(%d): %v", kind, err)
	}
	if registeredName != profile.Name {
		t.Fatalf("Name = %q, want %q", registeredName, profile.Name)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c, err := cookies.Build(r, kind, "x")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !c.HttpOnly {
		t.Fatal("custom Kind should honor profile.HttpOnly")
	}
	if c.Path != "/" {
		t.Fatalf("Path = %q, want /", c.Path)
	}
	if c.MaxAge != int((10 * time.Minute).Seconds()) {
		t.Fatalf("MaxAge = %d, want 600", c.MaxAge)
	}
}

func TestRegisterKindRejectsDuplicateName(t *testing.T) {
	t.Parallel()
	name := uniqueCookieName("duplicate_check")
	profile := cookies.Profile{
		Name:          name,
		HttpOnly:      true,
		SameSite:      http.SameSiteStrictMode,
		Path:          "/",
		DefaultMaxAge: time.Minute,
	}
	if _, err := cookies.RegisterKind(profile); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if _, err := cookies.RegisterKind(profile); err == nil {
		t.Fatal("expected ErrKindNameTaken on duplicate")
	} else if !errors.Is(err, cookies.ErrKindNameTaken) {
		t.Fatalf("err = %v, want ErrKindNameTaken", err)
	}
}

func TestRegisterKindRejectsEmptyName(t *testing.T) {
	t.Parallel()
	if _, err := cookies.RegisterKind(cookies.Profile{}); !errors.Is(err, cookies.ErrInvalidProfile) {
		t.Fatalf("err = %v, want ErrInvalidProfile", err)
	}
}

func TestRegisterKindRejectsInvalidProfile(t *testing.T) {
	t.Parallel()
	cases := []cookies.Profile{
		{Name: "has space", Path: "/"},
		{Name: "has/slash", Path: "/"},
		{Name: uniqueCookieName("relative_path"), Path: "relative"},
		{Name: uniqueCookieName("bad_path"), Path: "/bad;path"},
	}
	for _, profile := range cases {
		if _, err := cookies.RegisterKind(profile); !errors.Is(err, cookies.ErrInvalidProfile) {
			t.Fatalf("RegisterKind(%+v) err = %v, want ErrInvalidProfile", profile, err)
		}
	}
}

func TestRegisterKindDefaultsEmptyPath(t *testing.T) {
	t.Parallel()
	name := uniqueCookieName("default_path")
	kind, err := cookies.RegisterKind(cookies.Profile{
		Name:          name,
		HttpOnly:      true,
		SameSite:      http.SameSiteLaxMode,
		DefaultMaxAge: time.Minute,
	})
	if err != nil {
		t.Fatalf("RegisterKind: %v", err)
	}
	c, err := cookies.Build(nil, kind, "value")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c.Path != "/" {
		t.Fatalf("Path = %q, want /", c.Path)
	}
}

func TestRegisterKindIsRaceFree(t *testing.T) {
	t.Parallel()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				name := uniqueCookieName(fmt.Sprintf("race_%d_%d", i, j))
				kind, err := cookies.RegisterKind(cookies.Profile{
					Name:          name,
					HttpOnly:      true,
					SameSite:      http.SameSiteLaxMode,
					Path:          "/",
					DefaultMaxAge: time.Minute,
				})
				if err != nil {
					t.Errorf("RegisterKind: %v", err)
					return
				}
				if _, err := cookies.Build(nil, kind, "v"); err != nil {
					t.Errorf("Build(custom): %v", err)
					return
				}
				if _, err := cookies.Build(nil, cookies.KindSession, "v"); err != nil {
					t.Errorf("Build(session): %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
