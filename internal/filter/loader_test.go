package filter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aaronflorey/snip/internal/trust"
)

// trustAllFiles creates a trust store with all YAML files in the given
// directories pre-trusted. This avoids touching the real trust store on disk.
func trustAllFiles(t *testing.T, dirs ...string) trust.Store {
	t.Helper()
	store := make(trust.Store)
	for _, dir := range dirs {
		files, err := trust.FindFilterFiles(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := trust.Trust(store, files); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestLoadUserFilters(t *testing.T) {
	dir := t.TempDir()

	validYAML := `
name: "user-filter"
version: 1
match:
  command: "echo"
pipeline:
  - action: "keep_lines"
    pattern: "\\S"
on_error: "passthrough"
`
	if err := os.WriteFile(filepath.Join(dir, "echo.yaml"), []byte(validYAML), 0644); err != nil {
		t.Fatal(err)
	}

	filters, err := LoadUserFilters(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filters) != 1 {
		t.Fatalf("got %d filters, want 1", len(filters))
	}
	if filters[0].Name != "user-filter" {
		t.Errorf("name = %q", filters[0].Name)
	}
}

func TestLoadUserFiltersYMLExtension(t *testing.T) {
	dir := t.TempDir()

	validYAML := `
name: "yml-filter"
version: 1
match:
  command: "echo"
pipeline:
  - action: "keep_lines"
    pattern: "\\S"
on_error: "passthrough"
`
	if err := os.WriteFile(filepath.Join(dir, "echo.yml"), []byte(validYAML), 0644); err != nil {
		t.Fatal(err)
	}

	filters, err := LoadUserFilters(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filters) != 1 {
		t.Fatalf("got %d filters, want 1", len(filters))
	}
	if filters[0].Name != "yml-filter" {
		t.Errorf("name = %q, want %q", filters[0].Name, "yml-filter")
	}
}

func TestLoadUserFiltersBothExtensions(t *testing.T) {
	dir := t.TempDir()

	yamlContent := `
name: "yaml-filter"
version: 1
match:
  command: "cmd1"
pipeline:
  - action: "head"
    n: 5
on_error: "passthrough"
`
	ymlContent := `
name: "yml-filter"
version: 1
match:
  command: "cmd2"
pipeline:
  - action: "tail"
    n: 3
on_error: "passthrough"
`
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yml"), []byte(ymlContent), 0644); err != nil {
		t.Fatal(err)
	}

	filters, err := LoadUserFilters(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filters) != 2 {
		t.Fatalf("got %d filters, want 2", len(filters))
	}
}

func TestLoadUserFiltersMissingDir(t *testing.T) {
	filters, err := LoadUserFilters("/tmp/nonexistent-snip-filters-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filters != nil {
		t.Errorf("expected nil, got %v", filters)
	}
}

func TestLoadUserFiltersSkipsInvalid(t *testing.T) {
	dir := t.TempDir()

	// Invalid filter (no name)
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("pipeline: []"), 0644); err != nil {
		t.Fatal(err)
	}

	filters, err := LoadUserFilters(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filters) != 0 {
		t.Errorf("expected 0 filters, got %d", len(filters))
	}
}

func TestLoadAllUserOverridesEmbedded(t *testing.T) {
	dir := t.TempDir()

	// Create user filter that would override an embedded one
	userYAML := `
name: "custom"
version: 1
match:
  command: "custom"
pipeline:
  - action: "head"
    n: 5
on_error: "passthrough"
`
	if err := os.WriteFile(filepath.Join(dir, "custom.yaml"), []byte(userYAML), 0644); err != nil {
		t.Fatal(err)
	}

	store := trustAllFiles(t, dir)
	filters, err := LoadAllWithStore([]string{dir}, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have user filter
	found := false
	for _, f := range filters {
		if f.Name == "custom" {
			found = true
		}
	}
	if !found {
		t.Error("user filter not found in merged results")
	}
}

func TestLoadAllMultipleDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	yaml1 := `
name: "filter-a"
version: 1
match:
  command: "a"
pipeline:
  - action: "head"
    n: 5
on_error: "passthrough"
`
	yaml2 := `
name: "filter-a"
version: 1
match:
  command: "a-override"
pipeline:
  - action: "tail"
    n: 3
on_error: "passthrough"
`
	yaml3 := `
name: "filter-b"
version: 1
match:
  command: "b"
pipeline:
  - action: "head"
    n: 10
on_error: "passthrough"
`
	if err := os.WriteFile(filepath.Join(dir1, "a.yaml"), []byte(yaml1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "a.yaml"), []byte(yaml2), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "b.yaml"), []byte(yaml3), 0644); err != nil {
		t.Fatal(err)
	}

	store := trustAllFiles(t, dir1, dir2)
	filters, err := LoadAllWithStore([]string{dir1, dir2}, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// filter-a should be overridden by dir2 (command "a-override")
	// filter-b should be from dir2
	var filterA, filterB *Filter
	for i := range filters {
		switch filters[i].Name {
		case "filter-a":
			filterA = &filters[i]
		case "filter-b":
			filterB = &filters[i]
		}
	}

	if filterA == nil {
		t.Fatal("filter-a not found")
	}
	if filterA.Match.Command != "a-override" {
		t.Errorf("filter-a should be overridden by dir2, got command=%q", filterA.Match.Command)
	}

	if filterB == nil {
		t.Fatal("filter-b not found")
	}
	if filterB.Match.Command != "b" {
		t.Errorf("filter-b command: got %q, want %q", filterB.Match.Command, "b")
	}
}

func TestLoadAllEmptyDirs(t *testing.T) {
	filters, err := LoadAll(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not error even with no directories (EmbeddedFS is nil in tests)
	_ = filters
}

func TestLoadAllSkipsUntrustedProjectLocal(t *testing.T) {
	dir := t.TempDir()

	yamlContent := `
name: "untrusted-filter"
version: 1
match:
  command: "evil"
pipeline:
  - action: "head"
    n: 5
on_error: "passthrough"
`
	if err := os.WriteFile(filepath.Join(dir, "evil.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Use an empty trust store: file should be skipped
	store := make(trust.Store)
	filters, err := LoadAllWithStore([]string{dir}, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, f := range filters {
		if f.Name == "untrusted-filter" {
			t.Error("untrusted filter should have been skipped")
		}
	}
}

func TestLoadAllLoadsModifiedTrustedFile(t *testing.T) {
	dir := t.TempDir()

	yamlContent := `
name: "trusted-filter"
version: 1
match:
  command: "safe"
pipeline:
  - action: "head"
    n: 5
on_error: "passthrough"
`
	path := filepath.Join(dir, "safe.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Trust the file
	store := trustAllFiles(t, dir)

	// Modify the file after trusting (simulates tampering)
	if err := os.WriteFile(path, []byte(yamlContent+"\n# tampered"), 0644); err != nil {
		t.Fatal(err)
	}

	filters, err := LoadAllWithStore([]string{dir}, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, f := range filters {
		if f.Name == "trusted-filter" {
			t.Error("modified trusted filter should have been skipped (hash mismatch)")
		}
	}
}
