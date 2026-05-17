package architecture_test

// block_manifest_test.go makes the public block inventory executable: every
// release-grade core block must declare identity, ownership, contracts, status,
// composition laws, and evidence files that exist in the repo.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type blockManifest struct {
	SchemaVersion string          `json:"schemaVersion"`
	Repository    string          `json:"repository"`
	Blocks        []manifestBlock `json:"blocks"`
}

type manifestBlock struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Owner           string   `json:"owner"`
	Version         string   `json:"version"`
	Package         string   `json:"package"`
	Status          string   `json:"status"`
	Contracts       []string `json:"contracts"`
	CompositionLaws []string `json:"compositionLaws"`
	ExtensionPoints []string `json:"extensionPoints"`
	Evidence        []string `json:"evidence"`
}

func TestCoreBlockManifestIsReleaseGrade(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	manifest := readBlockManifest(t, filepath.Join(repoRoot, "docs", "block-manifest.json"))
	if manifest.SchemaVersion != "pk.block-manifest.v1" {
		t.Fatalf("schemaVersion = %q", manifest.SchemaVersion)
	}
	if manifest.Repository != "pk-core" {
		t.Fatalf("repository = %q", manifest.Repository)
	}
	if len(manifest.Blocks) == 0 {
		t.Fatal("manifest must declare at least one public block")
	}

	seen := map[string]struct{}{}
	for _, block := range manifest.Blocks {
		requireBlockField(t, block.ID, "id", block)
		requireBlockField(t, block.Kind, "kind", block)
		requireBlockField(t, block.Owner, "owner", block)
		requireBlockField(t, block.Version, "version", block)
		requireBlockField(t, block.Package, "package", block)
		if block.Status != "composable" {
			t.Fatalf("%s status = %q, want composable", block.ID, block.Status)
		}
		if _, exists := seen[block.ID]; exists {
			t.Fatalf("duplicate block id %q", block.ID)
		}
		seen[block.ID] = struct{}{}
		if len(block.Contracts) == 0 {
			t.Fatalf("%s must declare public contracts", block.ID)
		}
		if len(block.CompositionLaws) == 0 {
			t.Fatalf("%s must declare composition laws", block.ID)
		}
		if len(block.ExtensionPoints) == 0 {
			t.Fatalf("%s must declare extension points", block.ID)
		}
		if len(block.Evidence) == 0 {
			t.Fatalf("%s must declare evidence", block.ID)
		}
		for _, evidence := range block.Evidence {
			if _, err := os.Stat(filepath.Join(repoRoot, evidence)); err != nil {
				t.Fatalf("%s evidence %q: %v", block.ID, evidence, err)
			}
		}
	}

	requireLaws(t, seenBlock(t, manifest, "pk-core.module"), "identity", "associativity", "closure", "determinism", "diagnostics", "substitution")
	requireLaws(t, seenBlock(t, manifest, "pk-core.registry"), "identity", "closure", "determinism", "diagnostics")
}

func readBlockManifest(t *testing.T, path string) blockManifest {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read block manifest: %v", err)
	}
	var manifest blockManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode block manifest: %v", err)
	}
	return manifest
}

func requireBlockField(t *testing.T, value, name string, block manifestBlock) {
	t.Helper()
	if value == "" {
		t.Fatalf("%s must declare %s", block.ID, name)
	}
}

func seenBlock(t *testing.T, manifest blockManifest, id string) manifestBlock {
	t.Helper()
	for _, block := range manifest.Blocks {
		if block.ID == id {
			return block
		}
	}
	t.Fatalf("missing block %q", id)
	return manifestBlock{}
}

func requireLaws(t *testing.T, block manifestBlock, laws ...string) {
	t.Helper()
	for _, law := range laws {
		if !slices.Contains(block.CompositionLaws, law) {
			t.Fatalf("%s missing composition law %q", block.ID, law)
		}
	}
}
