//go:build !lite

package harness

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCodeProviderStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stmts := []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT);`,
		`CREATE TABLE part (session_id TEXT, time_created INTEGER, data TEXT);`,
		`INSERT INTO session (id, directory) VALUES ('session-1', '/work/repo');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('session-1', 1777269296976, '{"type":"tool","tool":"bash","state":{"input":{"command":"go test ./..."}}}');`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	provider := OpenCodeProvider{DBPath: path}
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
	if events[0].ToolCall == nil || events[0].ToolCall.Command != "go test ./..." {
		t.Fatalf("unexpected tool call: %+v", events[0].ToolCall)
	}
}
