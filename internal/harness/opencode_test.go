//go:build !lite

package harness

import (
	"database/sql"
	"path/filepath"
	"strings"
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

func TestOpenCodeProviderStreamPushesScopeIntoSQL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stmts := []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT);`,
		`CREATE TABLE part (session_id TEXT, time_created INTEGER, data TEXT);`,
		`INSERT INTO session (id, directory) VALUES ('exact', '/work/repo');`,
		`INSERT INTO session (id, directory) VALUES ('child', '/work/repo/internal');`,
		`INSERT INTO session (id, directory) VALUES ('parent', '/work');`,
		`INSERT INTO session (id, directory) VALUES ('other', '/other/repo');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('exact', 1777269296976, '{"type":"tool","tool":"bash","state":{"input":{"command":"go test ./exact"}}}');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('child', 1777269296977, '{"type":"tool","tool":"bash","state":{"input":{"command":"go test ./child"}}}');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('parent', 1777269296978, '{"type":"tool","tool":"bash","state":{"input":{"command":"go test ./parent"}}}');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('other', 1777269296979, '{"type":"tool","tool":"bash","state":{"input":{"command":"go test ./other"}}}');`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	var debug []DebugRecord
	provider := OpenCodeProvider{DBPath: path}
	query := Query{
		ProjectRoot: "/work/repo",
		Since:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano(),
		Debug: func(record DebugRecord) {
			debug = append(debug, record)
		},
	}

	var commands []string
	if err := provider.Stream(query, func(event Event) error {
		commands = append(commands, event.ToolCall.Command)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if got, want := len(commands), 3; got != want {
		t.Fatalf("expected %d events, got %d (%v)", want, got, commands)
	}
	if strings.Join(commands, ",") != "go test ./exact,go test ./child,go test ./parent" {
		t.Fatalf("unexpected commands: %v", commands)
	}

	summary := debug[len(debug)-1].Message
	if !strings.Contains(summary, "rows=3") {
		t.Fatalf("expected SQL-filtered row count in debug summary, got %q", summary)
	}
	if !strings.Contains(summary, "skipped_scope=0") {
		t.Fatalf("expected no Go-side scope skips after SQL filtering, got %q", summary)
	}
}

func TestOpenCodeProviderStreamPushesCutoffIntoSQLAcrossUnits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stmts := []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT);`,
		`CREATE TABLE part (session_id TEXT, time_created INTEGER, data TEXT);`,
		`INSERT INTO session (id, directory) VALUES ('seconds', '/work/repo');`,
		`INSERT INTO session (id, directory) VALUES ('milliseconds', '/work/repo');`,
		`INSERT INTO session (id, directory) VALUES ('microseconds', '/work/repo');`,
		`INSERT INTO session (id, directory) VALUES ('nanoseconds', '/work/repo');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('seconds', 1767225599, '{"type":"tool","tool":"bash","state":{"input":{"command":"before-seconds"}}}');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('seconds', 1767225600, '{"type":"tool","tool":"bash","state":{"input":{"command":"after-seconds"}}}');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('milliseconds', 1767225599999, '{"type":"tool","tool":"bash","state":{"input":{"command":"before-milliseconds"}}}');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('milliseconds', 1767225600000, '{"type":"tool","tool":"bash","state":{"input":{"command":"after-milliseconds"}}}');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('microseconds', 1767225599999999, '{"type":"tool","tool":"bash","state":{"input":{"command":"before-microseconds"}}}');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('microseconds', 1767225600000000, '{"type":"tool","tool":"bash","state":{"input":{"command":"after-microseconds"}}}');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('nanoseconds', 1767225599999999999, '{"type":"tool","tool":"bash","state":{"input":{"command":"before-nanoseconds"}}}');`,
		`INSERT INTO part (session_id, time_created, data) VALUES ('nanoseconds', 1767225600000000000, '{"type":"tool","tool":"bash","state":{"input":{"command":"after-nanoseconds"}}}');`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	var debug []DebugRecord
	provider := OpenCodeProvider{DBPath: path}
	query := Query{
		ProjectRoot: "/work/repo",
		Since:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano(),
		Debug: func(record DebugRecord) {
			debug = append(debug, record)
		},
	}

	var commands []string
	if err := provider.Stream(query, func(event Event) error {
		commands = append(commands, event.ToolCall.Command)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if strings.Join(commands, ",") != "after-seconds,after-milliseconds,after-microseconds,after-nanoseconds" {
		t.Fatalf("unexpected commands after cutoff filtering: %v", commands)
	}

	summary := debug[len(debug)-1].Message
	if !strings.Contains(summary, "rows=4") {
		t.Fatalf("expected SQL-filtered cutoff row count in debug summary, got %q", summary)
	}
	if !strings.Contains(summary, "skipped_cutoff=0") {
		t.Fatalf("expected no Go-side cutoff skips after SQL filtering, got %q", summary)
	}
}
