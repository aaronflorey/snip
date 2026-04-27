package harness

import "testing"

func TestExtractBaseCommand(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"go test ./...", "go"},
		{"git status", "git"},
		{"CGO_ENABLED=0 go build", "go"},
		{"FOO=bar BAZ=1 make test", "make"},
		{"make build; make test", "make"},
		{"git log | head -5", "git"},
		{"/usr/bin/git status", "git"},
		{"npm install && npm test", "npm"},
		{"echo hello", "echo"},
		{"  ls -la", "ls"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ExtractBaseCommand(tt.input)
			if got != tt.want {
				t.Fatalf("ExtractBaseCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMatchesProject(t *testing.T) {
	query := Query{ProjectRoot: "/work/repo"}
	if !matchesProject(query, "/work/repo") {
		t.Fatal("expected exact project match")
	}
	if !matchesProject(query, "/work/repo/internal") {
		t.Fatal("expected child project match")
	}
	if !matchesProject(query, "/work") {
		t.Fatal("expected parent project match")
	}
	if matchesProject(query, "/other/repo") {
		t.Fatal("did not expect unrelated project match")
	}
}

func TestNormalizeUnixTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  int64
	}{
		{name: "seconds", input: 1_777_269_296, want: 1_777_269_296 * 1_000_000_000},
		{name: "milliseconds", input: 1_777_269_296_976, want: 1_777_269_296_976 * 1_000_000},
		{name: "microseconds", input: 1_777_269_296_976_000, want: 1_777_269_296_976_000 * 1_000},
		{name: "nanoseconds", input: 1_777_269_296_976_000_000, want: 1_777_269_296_976_000_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeUnixTimestamp(tt.input); got != tt.want {
				t.Fatalf("normalizeUnixTimestamp(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
