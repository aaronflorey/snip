package discover

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/edouard-claude/snip/internal/harness"
)

type stubProvider struct {
	id     string
	events []harness.Event
	err    error
}

func (p stubProvider) ID() string { return p.id }

func (p stubProvider) Stream(_ harness.Query, yield func(harness.Event) error) error {
	if p.err != nil {
		return p.err
	}
	for _, event := range p.events {
		if err := yield(event); err != nil {
			return err
		}
	}
	return nil
}

func TestScan(t *testing.T) {
	query := harness.Query{All: true}
	providers := []harness.Provider{
		stubProvider{
			id: "claude",
			events: []harness.Event{
				{Provider: "claude", SessionID: "session-1", ToolCall: &harness.ToolCall{Command: "git status"}},
				{Provider: "claude", SessionID: "session-1", ToolCall: &harness.ToolCall{Command: "go test ./..."}},
				{Provider: "claude", SessionID: "session-1", ToolCall: &harness.ToolCall{Command: "cat file.txt"}},
			},
		},
		stubProvider{
			id: "opencode",
			events: []harness.Event{
				{Provider: "opencode", SessionID: "session-2", ToolCall: &harness.ToolCall{Command: "git log --oneline"}},
				{Provider: "opencode", SessionID: "session-3", ToolCall: &harness.ToolCall{Command: "make build"}},
			},
		},
	}

	supportedCmds := map[string]struct{}{
		"git":  {},
		"go":   {},
		"make": {},
	}

	result := scan(providers, supportedCmds, query)

	if result.SessionsScanned != 3 {
		t.Errorf("sessions scanned = %d, want 3", result.SessionsScanned)
	}
	if result.TotalCommands != 5 {
		t.Errorf("total commands = %d, want 5", result.TotalCommands)
	}
	if result.SupportedCount != 4 {
		t.Errorf("supported count = %d, want 4", result.SupportedCount)
	}
	if result.UnsupportedCount != 1 {
		t.Errorf("unsupported count = %d, want 1", result.UnsupportedCount)
	}

	found := false
	for _, stat := range result.Unsupported {
		if stat.Name == "cat" && stat.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Error("expected 'cat' in unsupported stats")
	}
}

func TestScanSkipsProviderErrors(t *testing.T) {
	result := scan([]harness.Provider{
		stubProvider{err: io.EOF},
	}, map[string]struct{}{}, harness.Query{All: true})

	if result.SessionsScanned != 0 {
		t.Errorf("sessions scanned = %d, want 0", result.SessionsScanned)
	}
	if result.TotalCommands != 0 {
		t.Errorf("total commands = %d, want 0", result.TotalCommands)
	}
}

func TestMapToStats(t *testing.T) {
	m := map[string]int{
		"git":  10,
		"make": 5,
		"go":   10,
	}
	stats := mapToStats(m)

	if len(stats) != 3 {
		t.Fatalf("expected 3 stats, got %d", len(stats))
	}
	if stats[0].Name != "git" && stats[0].Name != "go" {
		t.Errorf("first stat should be git or go, got %s", stats[0].Name)
	}
	if stats[0].Count != 10 {
		t.Errorf("first stat count = %d, want 10", stats[0].Count)
	}
	if stats[2].Name != "make" || stats[2].Count != 5 {
		t.Errorf("last stat = %v, want {make 5}", stats[2])
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		all   bool
		since int
	}{
		{"defaults", nil, false, 7},
		{"all flag", []string{"--all"}, true, 7},
		{"since flag", []string{"--since", "14"}, false, 14},
		{"both flags", []string{"--all", "--since", "30"}, true, 30},
		{"since without value", []string{"--since"}, false, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := parseArgs(tt.args)
			if opts.All != tt.all {
				t.Errorf("All = %v, want %v", opts.All, tt.all)
			}
			if opts.Since != tt.since {
				t.Errorf("Since = %d, want %d", opts.Since, tt.since)
			}
		})
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = old
	return buf.String()
}

func TestPrintResultEmpty(t *testing.T) {
	output := captureStdout(t, func() {
		printResult(Result{SessionsScanned: 3, TotalCommands: 0})
	})
	if !strings.Contains(output, "No Bash commands found") {
		t.Errorf("expected 'No Bash commands found' in output, got: %q", output)
	}
}

func TestPrintResultWithData(t *testing.T) {
	result := Result{
		SessionsScanned:  10,
		TotalCommands:    100,
		SupportedCount:   83,
		UnsupportedCount: 17,
		Supported: []CommandStat{
			{Name: "git", Count: 50},
			{Name: "go", Count: 33},
		},
		Unsupported: []CommandStat{
			{Name: "cat", Count: 17},
		},
	}
	output := captureStdout(t, func() {
		printResult(result)
	})
	for _, want := range []string{"Command", "Count", "git", "83%"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output, got: %q", want, output)
		}
	}
}

func TestPrintResultTruncatesLongNames(t *testing.T) {
	longName := strings.Repeat("a", 200)
	result := Result{
		SessionsScanned:  1,
		TotalCommands:    1,
		SupportedCount:   1,
		UnsupportedCount: 0,
		Supported: []CommandStat{
			{Name: longName, Count: 1},
		},
	}
	output := captureStdout(t, func() {
		printResult(result)
	})
	if strings.Contains(output, longName) {
		t.Errorf("expected long name (%d chars) to be truncated in output", len(longName))
	}
	if !strings.Contains(output, "...") {
		t.Errorf("expected truncated form ending in '...' in output")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "two", "three"); got != "two" {
		t.Fatalf("firstNonEmpty() = %q, want %q", got, "two")
	}
}

func TestQueryCutoffPlumbing(t *testing.T) {
	query := harness.NewQuery(false, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if query.Since == 0 {
		t.Fatal("expected query cutoff to be set")
	}
}
