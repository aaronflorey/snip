package discover

import (
	"bytes"
	"encoding/json"
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
	debug  []harness.DebugRecord
	err    error
	stream func(harness.Query, func(harness.Event) error) error
}

func (p stubProvider) ID() string { return p.id }

func (p stubProvider) Stream(query harness.Query, yield func(harness.Event) error) error {
	if p.stream != nil {
		return p.stream(query, yield)
	}
	if p.err != nil {
		return p.err
	}
	for _, record := range p.debug {
		if query.Debug != nil {
			query.Debug(record)
		}
	}
	for _, event := range p.events {
		if err := yield(event); err != nil {
			return err
		}
	}
	return nil
}

func TestScanCollectsDebugRecords(t *testing.T) {
	result := scan([]harness.Provider{
		stubProvider{
			id:    "opencode",
			debug: []harness.DebugRecord{{Provider: "opencode", Code: "summary", Message: "rows=3 yielded=1"}},
		},
	}, map[string]struct{}{}, harness.Query{All: true, Debug: func(harness.DebugRecord) {}})

	if len(result.Debug) != 1 {
		t.Fatalf("expected 1 debug record, got %d", len(result.Debug))
	}
	if result.Debug[0].Provider != "opencode" {
		t.Fatalf("unexpected debug provider: %+v", result.Debug[0])
	}
}

func TestApplyMinCount(t *testing.T) {
	result := applyMinCount(Result{
		SessionsScanned: 3,
		Supported: []CommandStat{
			{Name: "git", Count: 6},
			{Name: "go", Count: 4},
		},
		Unsupported: []CommandStat{
			{Name: "cat", Count: 5},
			{Name: "sed", Count: 2},
		},
		SupportedCount:   10,
		UnsupportedCount: 7,
		TotalCommands:    17,
	}, 5)

	if result.MinCount != 5 {
		t.Fatalf("MinCount = %d, want 5", result.MinCount)
	}
	if result.TotalCommands != 11 {
		t.Fatalf("TotalCommands = %d, want 11", result.TotalCommands)
	}
	if result.SupportedCount != 6 {
		t.Fatalf("SupportedCount = %d, want 6", result.SupportedCount)
	}
	if result.UnsupportedCount != 5 {
		t.Fatalf("UnsupportedCount = %d, want 5", result.UnsupportedCount)
	}
	if len(result.Supported) != 1 || result.Supported[0].Name != "git" {
		t.Fatalf("unexpected supported stats: %+v", result.Supported)
	}
	if len(result.Unsupported) != 1 || result.Unsupported[0].Name != "cat" {
		t.Fatalf("unexpected unsupported stats: %+v", result.Unsupported)
	}
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

func TestScanRunsProvidersConcurrently(t *testing.T) {
	firstStarted := make(chan struct{}, 1)
	secondStarted := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	done := make(chan Result, 1)

	providers := []harness.Provider{
		stubProvider{stream: func(query harness.Query, yield func(harness.Event) error) error {
			firstStarted <- struct{}{}
			<-releaseFirst
			return yield(harness.Event{Provider: "claude", SessionID: "session-1", ToolCall: &harness.ToolCall{Command: "git status"}})
		}},
		stubProvider{stream: func(query harness.Query, yield func(harness.Event) error) error {
			secondStarted <- struct{}{}
			return yield(harness.Event{Provider: "opencode", SessionID: "session-2", ToolCall: &harness.ToolCall{Command: "cat file.txt"}})
		}},
	}

	go func() {
		done <- scan(providers, map[string]struct{}{"git": {}}, harness.Query{All: true})
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first provider did not start")
	}

	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second provider did not start before first provider finished")
	}

	close(releaseFirst)

	select {
	case result := <-done:
		if result.TotalCommands != 2 {
			t.Fatalf("total commands = %d, want 2", result.TotalCommands)
		}
		if result.SupportedCount != 1 {
			t.Fatalf("supported count = %d, want 1", result.SupportedCount)
		}
		if result.UnsupportedCount != 1 {
			t.Fatalf("unsupported count = %d, want 1", result.UnsupportedCount)
		}
	case <-time.After(time.Second):
		t.Fatal("scan did not finish")
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
		name     string
		args     []string
		all      bool
		debug    bool
		json     bool
		minCount int
		since    int
	}{
		{"defaults", nil, false, false, false, 5, 7},
		{"all flag", []string{"--all"}, true, false, false, 5, 7},
		{"debug flag", []string{"--debug"}, false, true, false, 5, 7},
		{"json flag", []string{"--json"}, false, false, true, 5, 7},
		{"since flag", []string{"--since", "14"}, false, false, false, 5, 14},
		{"both flags", []string{"--all", "--debug", "--json", "--since", "30"}, true, true, true, 5, 30},
		{"since without value", []string{"--since"}, false, false, false, 5, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := parseArgs(tt.args)
			if opts.All != tt.all {
				t.Errorf("All = %v, want %v", opts.All, tt.all)
			}
			if opts.Debug != tt.debug {
				t.Errorf("Debug = %v, want %v", opts.Debug, tt.debug)
			}
			if opts.JSON != tt.json {
				t.Errorf("JSON = %v, want %v", opts.JSON, tt.json)
			}
			if opts.MinCount != tt.minCount {
				t.Errorf("MinCount = %d, want %d", opts.MinCount, tt.minCount)
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

func TestPrintResultEmptyAfterMinCount(t *testing.T) {
	output := captureStdout(t, func() {
		printResult(Result{SessionsScanned: 3, TotalCommands: 0, MinCount: 5})
	})
	if !strings.Contains(output, "minimum count threshold of 5") {
		t.Errorf("expected threshold message in output, got: %q", output)
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

func TestPrintDebug(t *testing.T) {
	query := harness.Query{CWD: "/work/repo", ProjectRoot: "/work/repo"}
	result := Result{Debug: []harness.DebugRecord{{Provider: "opencode", Code: "summary", Message: "rows=3 yielded=1"}}}
	output := captureStdout(t, func() {
		printDebug(Options{Debug: true, Since: 7}, query, result)
	})
	for _, want := range []string{"Discover Debug", "[opencode] summary: rows=3 yielded=1", "/work/repo"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output, got %q", want, output)
		}
	}
}

func TestPrintJSON(t *testing.T) {
	query := harness.Query{CWD: "/work/repo", ProjectRoot: "/work/repo"}
	result := Result{
		SessionsScanned:  2,
		TotalCommands:    11,
		SupportedCount:   6,
		UnsupportedCount: 5,
		MinCount:         5,
		Supported:        []CommandStat{{Name: "git", Count: 6}},
		Unsupported:      []CommandStat{{Name: "cat", Count: 5}},
		Debug:            []harness.DebugRecord{{Provider: "opencode", Code: "summary", Message: "rows=11 yielded=11"}},
	}

	output := captureStdout(t, func() {
		if err := printJSON(Options{Debug: true, JSON: true, Since: 7, MinCount: 5}, query, result); err != nil {
			t.Fatalf("printJSON() error = %v", err)
		}
	})

	var payload struct {
		Scope            string                `json:"scope"`
		MinCount         int                   `json:"min_count"`
		TotalCommands    int                   `json:"total_commands"`
		SupportedCount   int                   `json:"supported_count"`
		UnsupportedCount int                   `json:"unsupported_count"`
		CoveragePercent  float64               `json:"coverage_percent"`
		Supported        []CommandStat         `json:"supported"`
		Unsupported      []CommandStat         `json:"unsupported"`
		Debug            []harness.DebugRecord `json:"debug"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput=%s", err, output)
	}
	if payload.Scope != "/work/repo" {
		t.Fatalf("Scope = %q, want %q", payload.Scope, "/work/repo")
	}
	if payload.MinCount != 5 {
		t.Fatalf("MinCount = %d, want 5", payload.MinCount)
	}
	if payload.TotalCommands != 11 {
		t.Fatalf("TotalCommands = %d, want 11", payload.TotalCommands)
	}
	if payload.SupportedCount != 6 || payload.UnsupportedCount != 5 {
		t.Fatalf("unexpected counts: %+v", payload)
	}
	if len(payload.Supported) != 1 || payload.Supported[0].Name != "git" {
		t.Fatalf("unexpected supported payload: %+v", payload.Supported)
	}
	if len(payload.Unsupported) != 1 || payload.Unsupported[0].Name != "cat" {
		t.Fatalf("unexpected unsupported payload: %+v", payload.Unsupported)
	}
	if len(payload.Debug) != 1 || payload.Debug[0].Provider != "opencode" {
		t.Fatalf("unexpected debug payload: %+v", payload.Debug)
	}
}
