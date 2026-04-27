package harness

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexProviderStream(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "2026", "04", "27", "rollout-test.jsonl")
	writeFile(t, path, []byte(
		`{"timestamp":"2026-04-27T12:36:47.000Z","type":"session_meta","payload":{"id":"thread-1","cwd":"/work/repo"}}`+"\n"+
			`{"timestamp":"2026-04-27T12:36:48.000Z","type":"response_item","payload":{"type":"function_call","name":"bash","namespace":"shell","arguments":"{\"command\":\"npm test\"}","call_id":"call-1"}}`+"\n",
	))

	provider := CodexProvider{RootDir: root}
	query := Query{ProjectRoot: "/work/repo", Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()}

	var events []Event
	if err := provider.Stream(query, func(event Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ToolCall == nil || events[0].ToolCall.Command != "npm test" {
		t.Fatalf("unexpected tool call: %+v", events[0].ToolCall)
	}
}

func TestCodexProviderStreamPrefiltersSessionsFromStateDB(t *testing.T) {
	if !SQLiteAvailable {
		t.Skip("sqlite support is disabled")
	}

	root := filepath.Join(t.TempDir(), "sessions")
	matchPath := filepath.Join(root, "2026", "04", "27", "rollout-match.jsonl")
	writeFile(t, matchPath, []byte(
		`{"timestamp":"2026-04-27T12:36:47.000Z","type":"session_meta","payload":{"id":"thread-1","cwd":"/work/repo"}}`+"\n"+
			`{"timestamp":"2026-04-27T12:36:48.000Z","type":"response_item","payload":{"type":"function_call","name":"bash","namespace":"shell","arguments":"{\"command\":\"npm test\"}","call_id":"call-1"}}`+"\n",
	))
	skipPath := filepath.Join(root, "2026", "04", "27", "rollout-skip.jsonl")
	writeFile(t, skipPath, []byte(
		`{"timestamp":"2026-04-27T12:36:47.000Z","type":"session_meta","payload":{"id":"thread-2","cwd":"/other/repo"}}`+"\n"+
			`{"timestamp":"2026-04-27T12:36:48.000Z","type":"response_item","payload":{"type":"function_call","name":"bash","namespace":"shell","arguments":"{\"command\":\"should not be read\"}","call_id":"call-2"}}`+"\n",
	))

	db, err := sql.Open("sqlite", filepath.Join(filepath.Dir(root), "state_1.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE threads (rollout_path TEXT NOT NULL, cwd TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads (rollout_path, cwd) VALUES (?, ?), (?, ?)`, matchPath, "/work/repo", skipPath, "/other/repo"); err != nil {
		t.Fatal(err)
	}

	var records []DebugRecord
	provider := CodexProvider{RootDir: root}
	query := Query{
		ProjectRoot: "/work/repo",
		Since:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano(),
		Debug: func(record DebugRecord) {
			records = append(records, record)
		},
	}

	var events []Event
	if err := provider.Stream(query, func(event Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !hasSummaryWithFiles(records, "files=1") {
		t.Fatalf("expected summary to report one scanned file, got %+v", records)
	}
	if hasDebugCode(records, "scan_error") {
		t.Fatalf("expected prefilter to avoid scan errors, got %+v", records)
	}
	if events[0].SessionID != "thread-1" {
		t.Fatalf("expected matched session, got %q", events[0].SessionID)
	}
	if events[0].ToolCall == nil || events[0].ToolCall.Command != "npm test" {
		t.Fatalf("unexpected tool call: %+v", events[0].ToolCall)
	}
	if strings.Contains(events[0].Source, "skip") {
		t.Fatalf("unexpected source path: %s", events[0].Source)
	}
}
