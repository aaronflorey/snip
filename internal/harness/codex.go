package harness

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

type CodexProvider struct {
	RootDir string
}

func (CodexProvider) ID() string { return "codex" }

func (p CodexProvider) Stream(query Query, yield func(Event) error) error {
	root := p.RootDir
	if root == "" {
		root = filepath.Join(homeDir(), ".codex", "sessions")
	}

	files, _ := filepath.Glob(filepath.Join(root, "*", "*", "*", "rollout-*.jsonl"))
	for _, path := range files {
		if err := streamCodexFile(path, query, yield); err != nil {
			continue
		}
	}
	return nil
}

type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexSessionMeta struct {
	ID  string `json:"id"`
	CWD string `json:"cwd"`
}

type codexResponseItem struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Arguments string `json:"arguments"`
	CallID    string `json:"call_id"`
}

func streamCodexFile(path string, query Query, yield func(Event) error) error {
	var sessionID string
	var projectRoot string

	return scanJSONL(path, func(line []byte) error {
		var entry codexLine
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil
		}

		switch entry.Type {
		case "session_meta", "turn_context":
			var meta codexSessionMeta
			if err := json.Unmarshal(entry.Payload, &meta); err == nil {
				if meta.ID != "" {
					sessionID = meta.ID
				}
				if meta.CWD != "" {
					projectRoot = meta.CWD
				}
			}
			return nil
		case "response_item":
			var item codexResponseItem
			if err := json.Unmarshal(entry.Payload, &item); err != nil || item.Type != "function_call" {
				return nil
			}
			if !matchesProject(query, projectRoot) {
				return nil
			}
			command := commandFromJSONString(item.Arguments)
			if command == "" {
				return nil
			}
			ts := parseTimestamp(entry.Timestamp)
			if !afterCutoff(ts, query.Since) {
				return nil
			}

			toolName := item.Name
			if item.Namespace != "" {
				toolName = strings.TrimPrefix(item.Namespace+"__"+item.Name, "__")
			}
			return yield(Event{
				Provider:    "codex",
				SessionID:   defaultString(sessionID, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))),
				ProjectRoot: projectRoot,
				Timestamp:   ts,
				Source:      path,
				Kind:        "tool_call",
				ToolCall: &ToolCall{
					Name:      toolName,
					Command:   command,
					Arguments: json.RawMessage(item.Arguments),
				},
				Metadata: entry.Payload,
			})
		}

		return nil
	})
}
