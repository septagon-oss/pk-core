// Package cookies — cookies.go owns the Kind enum, the per-Kind profile
// table, the Settings struct, and the public builder/writer functions
// (Build, BuildClear, Write, Clear, Name). The profile table is the single
// source of truth for the security posture of every PlatformKit cookie.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cookies

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Kind identifies the purpose of a cookie. Each Kind has a fixed security
// profile (HttpOnly, SameSite, Path) that callers cannot override — that is
// the whole point of centralizing here.
type Kind int

const (
	// KindSession is the long-lived session cookie that carries the access
	// token (typically a JWT) to authenticated handlers. HttpOnly so JS
	// cannot exfiltrate via XSS, SameSite=Lax so top-level GET navigations
	// after OAuth callbacks still carry it, Path=/ so every route sees the
	// same session.
	KindSession Kind = iota + 1

	// KindCSRF is the double-submit token cookie. NOT HttpOnly because the
	// frontend reads it via JS to mirror it into a header on state-changing
	// requests. SameSite=Strict because the cookie's only purpose is to
	// defeat cross-site state-changing requests; Lax would undermine the
	// defense.
	KindCSRF

	// KindPreviewSession is the short-lived session used by preview /
	// staging environments to authenticate without going through full
	// login. Same security flags as KindSession; the separation lets
	// preview deployments adjust max-age without touching the main session
	// cookie's behavior.
	KindPreviewSession

	// KindTenantPin records the active tenant for multi-tenant users.
	// HttpOnly because client JS does not need to read it; the server
	// applies it during request enrichment.
	KindTenantPin
)

// ErrUnknownKind is returned by Build, BuildClear, Write, Clear, and Name
// when the caller passes a Kind that has no profile registered. Tests and
// callers may use errors.Is to detect it.
var ErrUnknownKind = errors.New("cookies: unknown kind")

// ErrKindNameTaken indicates RegisterKind was called with a profile.Name that
// is already registered.
var ErrKindNameTaken = errors.New("cookies: kind name already registered")

// ErrInvalidProfile indicates RegisterKind was called with a structurally
// invalid Profile.
var ErrInvalidProfile = errors.New("cookies: invalid profile")

var profileMu sync.RWMutex

// RegisterKind allocates a new Kind value at runtime for downstream packages
// to add their own cookie purposes (OAuth state, magic-link sessions, etc.).
// Returns ErrKindNameTaken if profile.Name conflicts with an existing Kind.
// Safe to call concurrently with Build/Write/Clear/Name; registration still
// belongs in startup/bootstrap code so the cookie surface is deterministic.
func RegisterKind(profile Profile) (Kind, error) {
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" {
		return 0, fmt.Errorf("%w: profile.Name must not be empty", ErrInvalidProfile)
	}
	if !validCookieName(profile.Name) {
		return 0, fmt.Errorf("%w: profile.Name %q is not a valid cookie name", ErrInvalidProfile, profile.Name)
	}
	if profile.Path == "" {
		profile.Path = "/"
	}
	if !validCookiePath(profile.Path) {
		return 0, fmt.Errorf("%w: profile.Path %q must be an absolute cookie path", ErrInvalidProfile, profile.Path)
	}

	profileMu.Lock()
	defer profileMu.Unlock()
	for _, p := range kindProfiles {
		if p.Name == profile.Name {
			return 0, fmt.Errorf("cookies.RegisterKind: %w: %q", ErrKindNameTaken, profile.Name)
		}
	}
	next := Kind(len(kindProfiles) + 1)
	for {
		if _, exists := kindProfiles[next]; !exists {
			break
		}
		next++
	}
	kindProfiles[next] = profile
	return next, nil
}

// Profile is the per-Kind security posture. Fields here are the
// "fixed by the package" portion; callers contribute Value and
// (optionally) MaxAge through Build's parameters and Options.
type Profile struct {
	Name     string
	HttpOnly bool
	SameSite http.SameSite
	Path     string
	// DefaultMaxAge is used when Build receives no WithMaxAge override and
	// the Kind has no Settings-backed lifetime.
	DefaultMaxAge time.Duration
}

// kindProfiles is the single source of truth for cookie security settings.
// Built-in cookie purposes are added here; downstream purposes go through
// RegisterKind so profileMu keeps readers and writers race-free.
var kindProfiles = map[Kind]Profile{
	KindSession: {
		Name:     "session",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	},
	KindCSRF: {
		Name:     "_csrf",
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	},
	KindPreviewSession: {
		Name:     "preview_session",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	},
	KindTenantPin: {
		Name:     "tenant_id",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	},
}

// Default lifetimes applied when Settings does not override them. Exported
// so downstream Pro packages and operators can reference the platform
// baseline without copy-pasting magic numbers.
const (
	DefaultSessionMaxAge   = 24 * time.Hour
	DefaultCSRFMaxAge      = 12 * time.Hour
	DefaultPreviewMaxAge   = 1 * time.Hour
	DefaultTenantPinMaxAge = 30 * 24 * time.Hour
)

// Settings is the platform-wide cookie configuration. Mutated only via
// Configure; reads use atomic.Pointer so Configure is safe under
// concurrent cookie writers.
type Settings struct {
	// ForceSecure causes every cookie to be emitted with Secure=true,
	// regardless of the request's apparent scheme. Production deployments
	// behind a TLS-terminating proxy that does not propagate the original
	// scheme MUST set this true.
	ForceSecure bool

	// CookieDomain is the Domain attribute applied to all cookies. Empty
	// means do not set a Domain (browser defaults to the request host).
	CookieDomain string

	// SessionMaxAge controls how long KindSession cookies persist. Zero
	// falls back to DefaultSessionMaxAge.
	SessionMaxAge time.Duration

	// CSRFMaxAge controls how long KindCSRF cookies persist. Zero falls
	// back to DefaultCSRFMaxAge.
	CSRFMaxAge time.Duration

	// PreviewMaxAge controls how long KindPreviewSession cookies persist.
	// Zero falls back to DefaultPreviewMaxAge.
	PreviewMaxAge time.Duration

	// TenantPinMaxAge controls how long KindTenantPin cookies persist.
	// Zero falls back to DefaultTenantPinMaxAge.
	TenantPinMaxAge time.Duration
}

// settings is held in an atomic.Pointer so Configure is safe under
// concurrent reads from Build.
var settings atomic.Pointer[Settings]

// Configure installs deployment-wide cookie settings. Pass a zero Settings
// to clear any previously installed override. Safe to call concurrently
// with cookie builders.
func Configure(s Settings) {
	clone := s
	settings.Store(&clone)
}

func currentSettings() Settings {
	if s := settings.Load(); s != nil {
		return *s
	}
	return Settings{}
}

// Build returns a fully-formed *http.Cookie configured for kind with the
// given value. r is used to detect TLS / X-Forwarded-Proto for the Secure
// flag and may be nil; in that case Secure is derived solely from
// Settings.ForceSecure.
func Build(r *http.Request, kind Kind, value string, opts ...Option) (*http.Cookie, error) {
	p, ok := profileFor(kind)
	if !ok {
		return nil, fmt.Errorf("%w %d", ErrUnknownKind, int(kind))
	}

	o := option{}
	for _, fn := range opts {
		fn(&o)
	}

	s := currentSettings()
	cookie := &http.Cookie{
		Name:     p.Name,
		Value:    value,
		Path:     p.Path,
		Domain:   s.CookieDomain,
		HttpOnly: p.HttpOnly,
		Secure:   isSecureRequest(r, s),
		SameSite: p.SameSite,
	}
	if o.maxAgeSet {
		cookie.MaxAge = secondsOrZero(o.maxAge)
	} else if d := resolveMaxAge(kind, p, s); d > 0 {
		cookie.MaxAge = secondsOrZero(d)
	}
	return cookie, nil
}

// BuildClear returns an *http.Cookie that, when written, removes the named
// cookie (MaxAge=-1, value=""). Path and Domain match the profile so the
// browser's match for deletion succeeds.
func BuildClear(r *http.Request, kind Kind) (*http.Cookie, error) {
	cookie, err := Build(r, kind, "")
	if err != nil {
		return nil, err
	}
	cookie.MaxAge = -1
	cookie.Expires = time.Unix(1, 0)
	return cookie, nil
}

// Write builds and emits the cookie via http.SetCookie. Convenience over
// Build + http.SetCookie. Returns any error from Build unchanged.
func Write(w http.ResponseWriter, r *http.Request, kind Kind, value string, opts ...Option) error {
	cookie, err := Build(r, kind, value, opts...)
	if err != nil {
		return err
	}
	http.SetCookie(w, cookie)
	return nil
}

// Clear builds and emits a clearing cookie via http.SetCookie.
func Clear(w http.ResponseWriter, r *http.Request, kind Kind) error {
	cookie, err := BuildClear(r, kind)
	if err != nil {
		return err
	}
	http.SetCookie(w, cookie)
	return nil
}

// Name returns the wire name of kind (e.g., "session", "_csrf"). Callers
// that read cookies (rather than write them) use this so the read path
// tracks any future deployment-time renaming applied to the write path.
func Name(kind Kind) (string, error) {
	p, ok := profileFor(kind)
	if !ok {
		return "", fmt.Errorf("%w %d", ErrUnknownKind, int(kind))
	}
	return p.Name, nil
}

func profileFor(kind Kind) (Profile, bool) {
	profileMu.RLock()
	defer profileMu.RUnlock()
	p, ok := kindProfiles[kind]
	return p, ok
}

func resolveMaxAge(kind Kind, p Profile, s Settings) time.Duration {
	switch kind {
	case KindSession:
		if s.SessionMaxAge > 0 {
			return s.SessionMaxAge
		}
		return DefaultSessionMaxAge
	case KindCSRF:
		if s.CSRFMaxAge > 0 {
			return s.CSRFMaxAge
		}
		return DefaultCSRFMaxAge
	case KindPreviewSession:
		if s.PreviewMaxAge > 0 {
			return s.PreviewMaxAge
		}
		return DefaultPreviewMaxAge
	case KindTenantPin:
		if s.TenantPinMaxAge > 0 {
			return s.TenantPinMaxAge
		}
		return DefaultTenantPinMaxAge
	default:
		return p.DefaultMaxAge
	}
}

func isSecureRequest(r *http.Request, s Settings) bool {
	if s.ForceSecure {
		return true
	}
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if v := r.Header.Get("X-Forwarded-Proto"); strings.EqualFold(strings.TrimSpace(v), "https") {
		return true
	}
	return false
}

func secondsOrZero(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d.Seconds())
}

func validCookieName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c <= 0x20 || c >= 0x7f {
			return false
		}
		switch c {
		case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}':
			return false
		}
	}
	return true
}

func validCookiePath(path string) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c <= 0x20 || c >= 0x7f || c == ';' {
			return false
		}
	}
	return true
}
