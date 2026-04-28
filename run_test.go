package snip

import (
	"testing"

	"github.com/aaronflorey/snip/internal/filter"
)

// TestRun_VersionFlag is a smoke test verifying that Run wires the embedded
// filesystem and forwards the call to cli.Run, returning a clean exit code
// for the --version short-circuit path.
func TestRun_VersionFlag(t *testing.T) {
	filter.EmbeddedFS = nil // ensure Run sets it

	if code := Run([]string{"snip", "--version"}); code != 0 {
		t.Fatalf("Run(--version) = %d, want 0", code)
	}

	if filter.EmbeddedFS == nil {
		t.Fatal("Run did not wire filter.EmbeddedFS")
	}
}

// TestRun_NoArgs verifies the usage path returns 0, matching the cli.Run
// contract for an argv that contains only the program name.
func TestRun_NoArgs(t *testing.T) {
	if code := Run([]string{"snip"}); code != 0 {
		t.Fatalf("Run([snip]) = %d, want 0", code)
	}
}
