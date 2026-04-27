package harness

import (
	"path/filepath"
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
