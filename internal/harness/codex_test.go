package harness

import (
	"path/filepath"
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
