package harness

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeProviderStream(t *testing.T) {
	root := t.TempDir()
	projectRoot := "/work/repo"
	projectDir := filepath.Join(root, "-work-repo")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionFile := filepath.Join(projectDir, "session.jsonl")
	writeFile(t, sessionFile, []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]},"timestamp":"2026-04-01T10:00:00.000Z"}`+"\n"))

	provider := ClaudeProvider{BaseDir: root}
	query := Query{ProjectRoot: projectRoot, Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()}

	var events []Event
	err := provider.Stream(query, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ToolCall == nil || events[0].ToolCall.Command != "go test ./..." {
		t.Fatalf("unexpected tool call: %+v", events[0].ToolCall)
	}
}
