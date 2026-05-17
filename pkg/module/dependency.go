package module

// dependency.go owns typed dependency declarations and version-constraint
// matching for ports-only module composition.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"fmt"
	"strconv"
	"strings"
)

// DependencyCategory defines the broad kind of port dependency a module has.
type DependencyCategory string

const (
	DependencyCategoryInfrastructure DependencyCategory = "infrastructure"
	DependencyCategoryBusiness       DependencyCategory = "business"
	DependencyCategoryUI             DependencyCategory = "ui"
	DependencyCategorySecurity       DependencyCategory = "security"
	DependencyCategoryMonitoring     DependencyCategory = "monitoring"
	DependencyCategoryData           DependencyCategory = "data"
	DependencyCategoryUnknown        DependencyCategory = "unknown"
)

// PortSpec is the human-authored part of a dependency declaration.
type PortSpec struct {
	Required bool
	Purpose  string
	Version  string

	Category    DependencyCategory
	SubCategory string

	PreferredProvider string
	FallbackProviders []string
}

// DependencySpec is kept as a friendlier name for APIs that describe module
// dependencies rather than port contracts. It is identical to PortSpec.
type DependencySpec = PortSpec

// RequiresPort declares a typed, required dependency on interface T.
func RequiresPort[T any](spec PortSpec) Dependency {
	spec.Required = true
	return dependencyOf[T](spec)
}

// OptionalPort declares a typed, optional dependency on interface T.
func OptionalPort[T any](spec PortSpec) Dependency {
	spec.Required = false
	return dependencyOf[T](spec)
}

// Require declares a required dependency on interface T.
func Require[T any](spec DependencySpec) Dependency {
	return RequiresPort[T](spec)
}

// Optional declares an optional dependency on interface T.
func Optional[T any](spec DependencySpec) Dependency {
	return OptionalPort[T](spec)
}

func dependencyOf[T any](spec PortSpec) Dependency {
	name := portNameOf[T]("module.RequiresPort")
	if spec.Category == "" {
		spec.Category = DependencyCategoryUnknown
	}

	return Dependency{
		Port:              Port{Name: name, Version: strings.TrimSpace(spec.Version)},
		Required:          spec.Required,
		Purpose:           strings.TrimSpace(spec.Purpose),
		Category:          spec.Category,
		SubCategory:       strings.TrimSpace(spec.SubCategory),
		PreferredProvider: strings.TrimSpace(spec.PreferredProvider),
		FallbackProviders: trimStrings(spec.FallbackProviders),
	}
}

// PortVersion is the producer-side version for a provided port.
type PortVersion string

// ValidatePortVersion reports whether a provided port version is syntactically
// valid. Empty versions are allowed and are treated as 0.0.0 by compatibility
// checks. Pre-release and build metadata are accepted but ignored for
// compatibility comparisons; port versions represent API shape, not release
// channel.
func ValidatePortVersion(version PortVersion) error {
	_, err := parseSemver(string(version))
	return err
}

// ValidateVersionConstraint reports whether a dependency version constraint is
// syntactically valid.
func ValidateVersionConstraint(constraint string) error {
	_, err := parseVersionConstraint(constraint)
	return err
}

// MatchesVersion reports whether a producer's version satisfies a dependency
// constraint. Empty constraints and "*" match anything. Empty producer versions
// are treated as 0.0.0.
func MatchesVersion(producer PortVersion, constraint string) (bool, error) {
	parsed, err := parseVersionConstraint(constraint)
	if err != nil {
		return false, err
	}
	if len(parsed) == 0 {
		return true, nil
	}
	have, err := parseSemver(string(producer))
	if err != nil {
		return false, fmt.Errorf("invalid producer version %q: %w", producer, err)
	}
	for _, comp := range parsed {
		if !comp.matches(have) {
			return false, nil
		}
	}
	return true, nil
}

func trimStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

type semver struct {
	major, minor, patch int
}

func (a semver) cmp(b semver) int {
	switch {
	case a.major != b.major:
		return a.major - b.major
	case a.minor != b.minor:
		return a.minor - b.minor
	default:
		return a.patch - b.patch
	}
}

func parseSemver(s string) (semver, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return semver{}, nil
	}
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return semver{}, fmt.Errorf("missing numeric version")
	}
	if idx := strings.IndexAny(s, "-+"); idx >= 0 {
		if idx == 0 {
			return semver{}, fmt.Errorf("missing numeric version")
		}
		if idx == len(s)-1 {
			return semver{}, fmt.Errorf("empty version metadata")
		}
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return semver{}, fmt.Errorf("too many segments")
	}
	out := semver{}
	dst := []*int{&out.major, &out.minor, &out.patch}
	for i, p := range parts {
		if p == "" {
			return semver{}, fmt.Errorf("empty numeric segment")
		}
		if strings.TrimSpace(p) != p {
			return semver{}, fmt.Errorf("segment %q contains whitespace", p)
		}
		if len(p) > 1 && strings.HasPrefix(p, "0") {
			return semver{}, fmt.Errorf("segment %q has a leading zero", p)
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return semver{}, fmt.Errorf("segment %q is not numeric", p)
		}
		if n < 0 {
			return semver{}, fmt.Errorf("segment %q is negative", p)
		}
		*dst[i] = n
	}
	return out, nil
}

type versionComparator struct {
	op   string
	want semver
}

func (c versionComparator) matches(have semver) bool {
	cmp := have.cmp(c.want)
	switch c.op {
	case "==":
		return cmp == 0
	case "!=":
		return cmp != 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	default:
		return false
	}
}

func parseVersionConstraint(constraint string) ([]versionComparator, error) {
	c := strings.TrimSpace(constraint)
	if c == "" || c == "*" {
		return nil, nil
	}
	if strings.HasPrefix(c, "^") {
		base, err := parseConstraintSemver(strings.TrimPrefix(c, "^"))
		if err != nil {
			return nil, err
		}
		upper := caretUpperBound(base)
		return []versionComparator{
			{op: ">=", want: base},
			{op: "<", want: upper},
		}, nil
	}
	if strings.HasPrefix(c, "~") {
		base, err := parseConstraintSemver(strings.TrimPrefix(c, "~"))
		if err != nil {
			return nil, err
		}
		upper := semver{major: base.major, minor: base.minor + 1}
		return []versionComparator{
			{op: ">=", want: base},
			{op: "<", want: upper},
		}, nil
	}
	parts := strings.Split(c, ",")
	out := make([]versionComparator, 0, len(parts))
	for _, raw := range parts {
		p := strings.TrimSpace(raw)
		if p == "" {
			return nil, fmt.Errorf("empty constraint segment")
		}
		comp, err := parseSingleComparator(p)
		if err != nil {
			return nil, fmt.Errorf("invalid constraint segment %q: %w", p, err)
		}
		out = append(out, comp)
	}
	return out, nil
}

func caretUpperBound(base semver) semver {
	switch {
	case base.major > 0:
		return semver{major: base.major + 1}
	case base.minor > 0:
		return semver{minor: base.minor + 1}
	default:
		return semver{patch: base.patch + 1}
	}
}

func parseSingleComparator(p string) (versionComparator, error) {
	for _, op := range []string{">=", "<=", "==", "!=", ">", "<"} {
		if strings.HasPrefix(p, op) {
			ver, err := parseConstraintSemver(strings.TrimPrefix(p, op))
			if err != nil {
				return versionComparator{}, err
			}
			return versionComparator{op: op, want: ver}, nil
		}
	}
	ver, err := parseConstraintSemver(p)
	if err != nil {
		return versionComparator{}, err
	}
	return versionComparator{op: "==", want: ver}, nil
}

func parseConstraintSemver(s string) (semver, error) {
	if strings.TrimSpace(s) == "" {
		return semver{}, fmt.Errorf("missing version")
	}
	return parseSemver(s)
}
