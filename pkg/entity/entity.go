// Package entity defines provider-neutral entity descriptors for PlatformKit.
package entity

// entity.go owns provider-neutral entity descriptors and catalog validation for
// module-owned data surfaces.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/septagon-oss/pk-core/pkg/registry"
)

// FieldType describes a platform-neutral field shape.
type FieldType string

// Supported platform-neutral field types.
const (
	// FieldString is a short single-line string value.
	FieldString FieldType = "string"
	// FieldText is a long multi-line text value.
	FieldText FieldType = "text"
	// FieldBoolean is a true/false value.
	FieldBoolean FieldType = "boolean"
	// FieldInteger is a whole-number value.
	FieldInteger FieldType = "integer"
	// FieldNumber is a floating-point numeric value.
	FieldNumber FieldType = "number"
	// FieldDateTime is a timestamp value.
	FieldDateTime FieldType = "datetime"
	// FieldEnum is a value constrained to a fixed set of EnumValues.
	FieldEnum FieldType = "enum"
	// FieldJSON is an arbitrary structured JSON value.
	FieldJSON FieldType = "json"
	// FieldRelation references another entity.
	FieldRelation FieldType = "relation"
)

// ExtensionPrefix marks caller-owned descriptor vocabulary. Core validates the
// shape of extension tokens without assigning semantics to them.
const ExtensionPrefix = "x."

// Capability describes a reusable entity capability.
type Capability string

// Reusable entity capabilities.
const (
	// CapabilityReadable indicates the entity supports read access.
	CapabilityReadable Capability = "readable"
	// CapabilityWritable indicates the entity supports create and update.
	CapabilityWritable Capability = "writable"
	// CapabilityListable indicates the entity supports list retrieval.
	CapabilityListable Capability = "listable"
	// CapabilityQueryable indicates the entity supports filtered queries.
	CapabilityQueryable Capability = "queryable"
	// CapabilityAudited indicates entity changes are audited.
	CapabilityAudited Capability = "audited"
	// CapabilityTenantScoped indicates entity rows are isolated per tenant.
	CapabilityTenantScoped Capability = "tenant_scoped"
)

// RelationshipKind describes a relationship cardinality.
type RelationshipKind string

// Supported relationship cardinalities.
const (
	// RelationshipOne is a to-one relationship.
	RelationshipOne RelationshipKind = "one"
	// RelationshipMany is a to-many relationship.
	RelationshipMany RelationshipKind = "many"
	// RelationshipBelongs is an owning belongs-to relationship.
	RelationshipBelongs RelationshipKind = "belongs_to"
)

// Field describes one entity field.
type Field struct {
	Name        string
	Type        FieldType
	Label       string
	Required    bool
	Readable    bool
	Writable    bool
	Searchable  bool
	Sortable    bool
	Filterable  bool
	EnumValues  []string
	Description string
	Metadata    map[string]string
}

// Relationship describes an entity-to-entity edge.
type Relationship struct {
	Name         string
	Kind         RelationshipKind
	TargetModule string
	TargetEntity string
	Description  string
	Metadata     map[string]string
}

// Policy declares action tokens required for entity operations.
type Policy struct {
	Read   []string
	Write  []string
	Delete []string
}

// Descriptor is the stable metadata a module publishes for an entity.
type Descriptor struct {
	ModuleID      string
	Entity        string
	DisplayName   string
	PluralName    string
	Description   string
	TenantScoped  bool
	Capabilities  []Capability
	Fields        []Field
	Relationships []Relationship
	Policy        Policy
	Metadata      map[string]string
}

// Key returns the module/entity ownership key.
func (d Descriptor) Key() Key {
	return Key{ModuleID: strings.TrimSpace(d.ModuleID), Entity: strings.TrimSpace(d.Entity)}
}

// Key identifies an entity descriptor.
type Key struct {
	ModuleID string
	Entity   string
}

// String returns the "moduleID:entity" representation of the key.
func (k Key) String() string {
	return strings.TrimSpace(k.ModuleID) + ":" + strings.TrimSpace(k.Entity)
}

// Normalize trims and validates a descriptor.
func (d Descriptor) Normalize() (Descriptor, error) {
	d.ModuleID = strings.TrimSpace(d.ModuleID)
	d.Entity = strings.TrimSpace(d.Entity)
	d.DisplayName = strings.TrimSpace(d.DisplayName)
	d.PluralName = strings.TrimSpace(d.PluralName)
	d.Description = strings.TrimSpace(d.Description)
	d.Capabilities = normalizeCapabilities(d.Capabilities)
	d.Metadata = normalizeMetadata(d.Metadata)

	if d.ModuleID == "" {
		return Descriptor{}, fmt.Errorf("entity descriptor module ID is required")
	}
	if hasWhitespace(d.ModuleID) {
		return Descriptor{}, fmt.Errorf("entity descriptor module ID %q must not contain whitespace", d.ModuleID)
	}
	if d.Entity == "" {
		return Descriptor{}, fmt.Errorf("entity descriptor entity is required")
	}
	if hasWhitespace(d.Entity) {
		return Descriptor{}, fmt.Errorf("entity descriptor entity %q must not contain whitespace", d.Entity)
	}
	for _, capability := range d.Capabilities {
		if !validCapability(capability) {
			return Descriptor{}, fmt.Errorf("entity descriptor capability %q is not supported", capability)
		}
	}
	if d.DisplayName == "" {
		d.DisplayName = d.Entity
	}
	if d.PluralName == "" {
		d.PluralName = d.DisplayName + "s"
	}
	if d.TenantScoped && !hasCapability(d.Capabilities, CapabilityTenantScoped) {
		d.Capabilities = append(d.Capabilities, CapabilityTenantScoped)
		sortCapabilities(d.Capabilities)
	}

	fields, err := normalizeFields(d.Fields)
	if err != nil {
		return Descriptor{}, fmt.Errorf("%s: %w", d.Key(), err)
	}
	d.Fields = fields

	relationships, err := normalizeRelationships(d.ModuleID, d.Relationships)
	if err != nil {
		return Descriptor{}, fmt.Errorf("%s: %w", d.Key(), err)
	}
	d.Relationships = relationships
	d.Policy = normalizePolicy(d.Policy)
	return d, nil
}

// RequiredPolicyTokens returns every policy token referenced by the descriptor.
func (d Descriptor) RequiredPolicyTokens() []string {
	normalized, err := d.Normalize()
	if err != nil {
		return nil
	}
	return normalizeStrings(slices.Concat(normalized.Policy.Read, normalized.Policy.Write, normalized.Policy.Delete))
}

// OperationPolicy returns the policy tokens for a standard entity operation.
func (d Descriptor) OperationPolicy(operation string) []string {
	normalized, err := d.Normalize()
	if err != nil {
		return nil
	}
	switch strings.TrimSpace(operation) {
	case "read":
		return slices.Clone(normalized.Policy.Read)
	case "write":
		return slices.Clone(normalized.Policy.Write)
	case "delete":
		return slices.Clone(normalized.Policy.Delete)
	default:
		return nil
	}
}

// CatalogSpec is the standard registry spec for entity descriptors.
func CatalogSpec() registry.Spec {
	return registry.Spec{
		ID:           "core.entities",
		Owner:        registry.OwnerCore,
		Contribution: "entity.Descriptor",
		Key:          "moduleID:entity",
		Description:  "Module-owned entity descriptor catalog",
	}
}

// NewCatalog builds a deterministic entity descriptor catalog.
func NewCatalog(descriptors ...Descriptor) (*registry.Catalog[Key, Descriptor], error) {
	builder := registry.NewCatalogBuilder[Key, Descriptor](
		registry.WithCatalogSpec[Key, Descriptor](CatalogSpec()),
		registry.WithCatalogKeyLess[Key, Descriptor](func(a, b Key) bool { return a.String() < b.String() }),
		registry.WithCatalogKeyValidator[Key, Descriptor](func(key Key) error {
			if strings.TrimSpace(key.ModuleID) == "" || strings.TrimSpace(key.Entity) == "" {
				return fmt.Errorf("moduleID and entity are required")
			}
			return nil
		}),
		registry.WithCatalogValueValidator[Key, Descriptor](func(descriptor Descriptor) error {
			_, err := descriptor.Normalize()
			return err
		}),
	)
	for _, descriptor := range descriptors {
		normalized, err := descriptor.Normalize()
		if err != nil {
			return nil, err
		}
		builder.Add(normalized.Key(), normalized, normalized.ModuleID)
	}
	return builder.Build()
}

func normalizeFields(fields []Field) ([]Field, error) {
	out := make([]Field, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		field.Name = strings.TrimSpace(field.Name)
		field.Type = FieldType(strings.TrimSpace(string(field.Type)))
		field.Label = strings.TrimSpace(field.Label)
		field.Description = strings.TrimSpace(field.Description)
		field.EnumValues = normalizeStrings(field.EnumValues)
		field.Metadata = normalizeMetadata(field.Metadata)
		if field.Name == "" {
			return nil, fmt.Errorf("field name is required")
		}
		if hasWhitespace(field.Name) {
			return nil, fmt.Errorf("field %q name must not contain whitespace", field.Name)
		}
		if field.Type == "" {
			return nil, fmt.Errorf("field %q type is required", field.Name)
		}
		if !validFieldType(field.Type) {
			return nil, fmt.Errorf("field %q type %q is not supported", field.Name, field.Type)
		}
		if field.Type == FieldEnum && len(field.EnumValues) == 0 {
			return nil, fmt.Errorf("field %q enum values are required", field.Name)
		}
		if field.Label == "" {
			field.Label = field.Name
		}
		if _, exists := seen[field.Name]; exists {
			return nil, fmt.Errorf("duplicate field %q", field.Name)
		}
		seen[field.Name] = struct{}{}
		out = append(out, field)
	}
	slices.SortStableFunc(out, func(a, b Field) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

func normalizeRelationships(moduleID string, relationships []Relationship) ([]Relationship, error) {
	out := make([]Relationship, 0, len(relationships))
	seen := map[string]struct{}{}
	for _, rel := range relationships {
		rel.Name = strings.TrimSpace(rel.Name)
		rel.Kind = RelationshipKind(strings.TrimSpace(string(rel.Kind)))
		rel.TargetModule = strings.TrimSpace(rel.TargetModule)
		rel.TargetEntity = strings.TrimSpace(rel.TargetEntity)
		rel.Description = strings.TrimSpace(rel.Description)
		rel.Metadata = normalizeMetadata(rel.Metadata)
		if rel.Name == "" {
			return nil, fmt.Errorf("relationship name is required")
		}
		if hasWhitespace(rel.Name) {
			return nil, fmt.Errorf("relationship %q name must not contain whitespace", rel.Name)
		}
		if rel.Kind == "" {
			return nil, fmt.Errorf("relationship %q kind is required", rel.Name)
		}
		if !validRelationshipKind(rel.Kind) {
			return nil, fmt.Errorf("relationship %q kind %q is not supported", rel.Name, rel.Kind)
		}
		if rel.TargetEntity == "" {
			return nil, fmt.Errorf("relationship %q target entity is required", rel.Name)
		}
		if hasWhitespace(rel.TargetEntity) {
			return nil, fmt.Errorf("relationship %q target entity %q must not contain whitespace", rel.Name, rel.TargetEntity)
		}
		if rel.TargetModule == "" {
			rel.TargetModule = moduleID
		}
		if hasWhitespace(rel.TargetModule) {
			return nil, fmt.Errorf("relationship %q target module %q must not contain whitespace", rel.Name, rel.TargetModule)
		}
		if _, exists := seen[rel.Name]; exists {
			return nil, fmt.Errorf("duplicate relationship %q", rel.Name)
		}
		seen[rel.Name] = struct{}{}
		out = append(out, rel)
	}
	slices.SortStableFunc(out, func(a, b Relationship) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

func validCapability(capability Capability) bool {
	switch capability {
	case CapabilityReadable, CapabilityWritable, CapabilityListable, CapabilityQueryable, CapabilityAudited, CapabilityTenantScoped:
		return true
	default:
		return isExtensionToken(string(capability))
	}
}

func validFieldType(fieldType FieldType) bool {
	switch fieldType {
	case FieldString, FieldText, FieldBoolean, FieldInteger, FieldNumber, FieldDateTime, FieldEnum, FieldJSON, FieldRelation:
		return true
	default:
		return isExtensionToken(string(fieldType))
	}
}

func validRelationshipKind(kind RelationshipKind) bool {
	switch kind {
	case RelationshipOne, RelationshipMany, RelationshipBelongs:
		return true
	default:
		return isExtensionToken(string(kind))
	}
}

// IsExtensionToken reports whether value is a namespaced extension token.
func IsExtensionToken(value string) bool {
	return isExtensionToken(value)
}

func isExtensionToken(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, ExtensionPrefix) {
		return false
	}
	if len(value) == len(ExtensionPrefix) {
		return false
	}
	return !strings.ContainsAny(value, " \t\n\r")
}

func hasWhitespace(value string) bool {
	return strings.ContainsAny(value, " \t\n\r")
}

func normalizeCapabilities(capabilities []Capability) []Capability {
	out := make([]Capability, 0, len(capabilities))
	seen := map[Capability]struct{}{}
	for _, capability := range capabilities {
		capability = Capability(strings.TrimSpace(string(capability)))
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}
	sortCapabilities(out)
	return out
}

func sortCapabilities(capabilities []Capability) {
	slices.Sort(capabilities)
}

func hasCapability(capabilities []Capability, want Capability) bool {
	return slices.Contains(capabilities, want)
}

func normalizePolicy(policy Policy) Policy {
	return Policy{
		Read:   normalizeStrings(policy.Read),
		Write:  normalizeStrings(policy.Write),
		Delete: normalizeStrings(policy.Delete),
	}
}

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func normalizeMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
