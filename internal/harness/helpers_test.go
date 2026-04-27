package harness

import "testing"

func TestExtractBaseCommandFast(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		fastOK bool
	}{
		{name: "simple go test", input: "go test ./...", want: "go", fastOK: true},
		{name: "path binary", input: "/usr/bin/git status", want: "git", fastOK: true},
		{name: "env prefix", input: "CGO_ENABLED=0 go build", want: "go", fastOK: true},
		{name: "quoted env prefix", input: "FOO='bar baz' go test ./...", want: "go", fastOK: true},
		{name: "shell builtin still simple", input: "echo hello", want: "", fastOK: true},
		{name: "control flow falls back", input: "if [ -f package.json ]; then node scripts/check.js; fi", fastOK: false},
		{name: "pipeline falls back", input: "git log | head -5", fastOK: false},
		{name: "assignment with substitution falls back", input: "tmpfile=$(mktemp) && rtk go test ./...", fastOK: false},
		{name: "command wrapper falls back", input: "command log show --last 20m --style compact", fastOK: false},
		{name: "bash c wrapper falls back", input: "bash -lc 'go test ./internal/harness/...'", fastOK: false},
		{name: "pnpm exec wrapper falls back", input: "pnpm exec playwright test", fastOK: false},
		{name: "mise wrapper falls back", input: "mise x -- bun test", fastOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractBaseCommandFast(tt.input)
			if ok != tt.fastOK {
				t.Fatalf("extractBaseCommandFast(%q) ok = %v, want %v", tt.input, ok, tt.fastOK)
			}
			if got != tt.want {
				t.Fatalf("extractBaseCommandFast(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractBaseCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple", input: "go test ./...", want: "go"},
		{name: "path", input: "/usr/bin/git status", want: "git"},
		{name: "env prefix", input: "CGO_ENABLED=0 go build", want: "go"},
		{name: "multiple env prefix", input: "FOO=bar BAZ=1 make test", want: "make"},
		{name: "binary chain", input: "make build; make test", want: "make"},
		{name: "pipeline", input: "git log | head -5", want: "git"},
		{name: "and chain", input: "npm install && npm test", want: "npm"},
		{name: "whitespace", input: "  ls -la", want: "ls"},
		{name: "assignment then wrapper", input: "tmpfile=$(mktemp) && rtk go test ./... -coverprofile=\"$tmpfile\" && rm \"$tmpfile\"", want: "go"},
		{name: "if builtin body ignored", input: "if [ -f .planning/REQUIREMENTS.md ]; then echo exists; else echo missing; fi", want: ""},
		{name: "if with real command body", input: "if [ -f package.json ]; then node scripts/check.js; fi", want: "node"},
		{name: "command lookup unwrap", input: "command -v npx && rtk npx wrangler --version", want: "wrangler"},
		{name: "command builtin actual command", input: "command log show --last 20m --style compact", want: "log"},
		{name: "bash c unwrap", input: "bash -lc 'go test ./internal/harness/...'", want: "go"},
		{name: "shell builtin ignored", input: "echo hello", want: ""},
		{name: "pnpm exec unwrap", input: "pnpm exec playwright test", want: "playwright"},
		{name: "pnpm regular preserved", input: "pnpm test", want: "pnpm"},
		{name: "mise exec unwrap", input: "mise x -- bun test", want: "bun"},
		{name: "which probe ignored", input: "which node && node --version", want: "node"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
