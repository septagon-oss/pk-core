package entity

// entity_test.go validates descriptor normalization, policy extraction, and
// entity catalog diagnostics.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"strings"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/registry"
)

func validDescriptor() Descriptor {
	return Descriptor{
		ModuleID:     "user_management",
		Entity:       "User",
		TenantScoped: true,
		Capabilities: []Capability{CapabilityReadable, CapabilityReadable},
		Fields: []Field{
			{Name: "email", Type: FieldString, Readable: true, Writable: true, Searchable: true, Metadata: map[string]string{" ui.widget ": " email "}},
			{Name: "id", Type: FieldString, Readable: true},
		},
		Relationships: []Relationship{
			{Name: "roles", Kind: RelationshipMany, TargetEntity: "Role", Metadata: map[string]string{"inverse": "users"}},
		},
		Policy: Policy{
			Read:  []string{"user:read", "user:read"},
			Write: []string{"user:write"},
		},
	}
}

func TestDescriptorNormalize(t *testing.T) {
	descriptor, err := validDescriptor().Normalize()
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if descriptor.DisplayName != "User" || descriptor.PluralName != "Users" {
		t.Fatalf("display names = %q/%q; want User/Users", descriptor.DisplayName, descriptor.PluralName)
	}
	if !hasCapability(descriptor.Capabilities, CapabilityTenantScoped) {
		t.Fatal("tenant-scoped descriptor should include tenant_scoped capability")
	}
	if descriptor.Fields[0].Name != "email" || descriptor.Fields[1].Name != "id" {
		t.Fatalf("fields not sorted: %#v", descriptor.Fields)
	}
	if descriptor.Fields[0].Metadata["ui.widget"] != "email" {
		t.Fatalf("field metadata = %#v; want normalized widget metadata", descriptor.Fields[0].Metadata)
	}
	if descriptor.Relationships[0].TargetModule != "user_management" {
		t.Fatalf("relationship target module = %q; want same-module default", descriptor.Relationships[0].TargetModule)
	}
	if got := descriptor.RequiredPolicyTokens(); strings.Join(got, ",") != "user:read,user:write" {
		t.Fatalf("RequiredPolicyTokens = %#v; want read/write", got)
	}
	if got := descriptor.OperationPolicy("read"); len(got) != 1 || got[0] != "user:read" {
		t.Fatalf("OperationPolicy(read) = %#v", got)
	}
}

func TestDescriptorAllowsNamespacedExtensionVocabulary(t *testing.T) {
	descriptor, err := (Descriptor{
		ModuleID:     "commerce",
		Entity:       "Invoice",
		Capabilities: []Capability{"x.exportable"},
		Fields: []Field{
			{Name: "amount", Type: "x.money"},
		},
		Relationships: []Relationship{
			{Name: "settlement", Kind: "x.external_reference", TargetModule: "payments", TargetEntity: "Settlement"},
		},
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if descriptor.Fields[0].Type != "x.money" {
		t.Fatalf("field type = %q; want x.money", descriptor.Fields[0].Type)
	}
	if !hasCapability(descriptor.Capabilities, "x.exportable") {
		t.Fatalf("capabilities = %#v; want extension capability", descriptor.Capabilities)
	}
	if !IsExtensionToken("x.money") || IsExtensionToken("money") || IsExtensionToken("x.") || IsExtensionToken("x.bad value") {
		t.Fatal("extension token validation should require the x. namespace")
	}
}

func TestDescriptorNormalizeRejectsInvalid(t *testing.T) {
	tests := []Descriptor{
		{},
		{ModuleID: "m"},
		{ModuleID: "m", Entity: "E", Fields: []Field{{Type: FieldString}}},
		{ModuleID: "m", Entity: "E", Fields: []Field{{Name: "id"}}},
		{ModuleID: "m", Entity: "E", Fields: []Field{{Name: "id", Type: "uuid"}}},
		{ModuleID: "m", Entity: "E", Fields: []Field{{Name: "status", Type: FieldEnum}}},
		{ModuleID: "m", Entity: "E", Fields: []Field{{Name: "id", Type: FieldString}, {Name: "id", Type: FieldString}}},
		{ModuleID: "m", Entity: "E", Relationships: []Relationship{{Name: "owner", Kind: RelationshipOne}}},
		{ModuleID: "m", Entity: "E", Relationships: []Relationship{{Name: "owner", Kind: "parent", TargetEntity: "User"}}},
		{ModuleID: "bad module", Entity: "E"},
		{ModuleID: "m", Entity: "Bad Entity"},
		{ModuleID: "m", Entity: "E", Capabilities: []Capability{"exportable"}},
		{ModuleID: "m", Entity: "E", Fields: []Field{{Name: "bad field", Type: FieldString}}},
		{ModuleID: "m", Entity: "E", Relationships: []Relationship{{Name: "bad rel", Kind: RelationshipOne, TargetEntity: "User"}}},
		{ModuleID: "m", Entity: "E", Relationships: []Relationship{{Name: "owner", Kind: RelationshipOne, TargetEntity: "Bad Entity"}}},
		{ModuleID: "m", Entity: "E", Relationships: []Relationship{{Name: "owner", Kind: RelationshipOne, TargetModule: "bad module", TargetEntity: "User"}}},
	}
	for _, descriptor := range tests {
		if _, err := descriptor.Normalize(); err == nil {
			t.Fatalf("Normalize(%#v) should fail", descriptor)
		}
	}
}

func TestNewCatalog(t *testing.T) {
	catalog, err := NewCatalog(validDescriptor())
	if err != nil {
		t.Fatalf("NewCatalog error = %v", err)
	}
	spec, ok := catalog.Spec()
	if !ok {
		t.Fatal("entity catalog should expose spec")
	}
	if spec.ID != "core.entities" || spec.Owner != registry.OwnerCore {
		t.Fatalf("spec = %#v", spec)
	}
	descriptor, ok := catalog.Lookup(Key{ModuleID: "user_management", Entity: "User"})
	if !ok {
		t.Fatal("expected user descriptor")
	}
	if descriptor.Key().String() != "user_management:User" {
		t.Fatalf("descriptor key = %q", descriptor.Key())
	}

	_, err = NewCatalog(validDescriptor(), validDescriptor())
	if err == nil {
		t.Fatal("duplicate entity descriptors should fail")
	}
}
