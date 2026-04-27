package harness

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGeminiProviderStream(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "tmp", "project-a")
	writeFile(t, filepath.Join(projectDir, ".project_root"), []byte("/work/repo\n"))
	writeFile(t, filepath.Join(projectDir, "chats", "chat-1.jsonl"), []byte(`{"sessionId":"chat-1","timestamp":"2026-04-27T12:36:48.000Z","toolName":"bash","arguments":{"command":"pnpm test"}}`+"\n"))

	provider := GeminiProvider{RootDir: root}
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
	if events[0].ToolCall == nil || events[0].ToolCall.Command != "pnpm test" {
		t.Fatalf("unexpected tool call: %+v", events[0].ToolCall)
	}
}
