package harness

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCopilotProviderStream(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session-1", "events.jsonl")
	writeFile(t, path, []byte(
		`{"type":"session.start","data":{"sessionId":"session-1","context":{"cwd":"/work/repo","gitRoot":"/work/repo"}}}`+"\n"+
			`{"type":"tool.execution_start","timestamp":"2026-04-27T12:36:48.000Z","data":{"toolName":"bash","arguments":{"command":"git status"}}}`+"\n",
	))

	provider := CopilotProvider{RootDir: root}
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
	if events[0].ToolCall == nil || events[0].ToolCall.Command != "git status" {
		t.Fatalf("unexpected tool call: %+v", events[0].ToolCall)
	}
}

func TestCopilotProviderStreamPrefiltersSessionsFromStore(t *testing.T) {
	if !SQLiteAvailable {
		t.Skip("sqlite support is disabled")
	}

	root := filepath.Join(t.TempDir(), "session-state")
	writeFile(t, filepath.Join(root, "match", "events.jsonl"), []byte(
		`{"type":"session.start","data":{"sessionId":"match","context":{"cwd":"/work/repo","gitRoot":"/work/repo"}}}`+"\n"+
			`{"type":"tool.execution_start","timestamp":"2026-04-27T12:36:48.000Z","data":{"toolName":"bash","arguments":{"command":"git status"}}}`+"\n",
	))
	writeFile(t, filepath.Join(root, "skip", "events.jsonl"), []byte(
		`{"type":"session.start","data":{"sessionId":"skip","context":{"cwd":"/other/repo","gitRoot":"/other/repo"}}}`+"\n"+
			`{"type":"tool.execution_start","timestamp":"2026-04-27T12:36:48.000Z","data":{"toolName":"bash","arguments":{"command":"should not be read"}}}`+"\n",
	))

	db, err := sql.Open("sqlite", filepath.Join(filepath.Dir(root), "session-store.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, cwd TEXT, repository TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, cwd, repository) VALUES ('match', '/work/repo', ''), ('skip', '/other/repo', '')`); err != nil {
		t.Fatal(err)
	}

	var records []DebugRecord
	provider := CopilotProvider{RootDir: root}
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
	if events[0].SessionID != "match" {
		t.Fatalf("expected matched session, got %q", events[0].SessionID)
	}
	if events[0].ToolCall == nil || events[0].ToolCall.Command != "git status" {
		t.Fatalf("unexpected tool call: %+v", events[0].ToolCall)
	}
	if strings.Contains(events[0].Source, "skip") {
		t.Fatalf("unexpected source path: %s", events[0].Source)
	}
}

func hasSummaryWithFiles(records []DebugRecord, want string) bool {
	for _, record := range records {
		if record.Code == "summary" && strings.Contains(record.Message, want) {
			return true
		}
	}
	return false
}

func hasDebugCode(records []DebugRecord, want string) bool {
	for _, record := range records {
		if record.Code == want {
			return true
		}
	}
	return false
}
