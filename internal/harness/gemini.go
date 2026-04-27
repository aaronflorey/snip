package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type GeminiProvider struct {
	RootDir string
}

func (GeminiProvider) ID() string { return "gemini" }

func (p GeminiProvider) Stream(query Query, yield func(Event) error) error {
	root := p.RootDir
	if root == "" {
		root = filepath.Join(homeDir(), ".gemini")
	}

	for _, base := range []string{filepath.Join(root, "tmp"), filepath.Join(root, "history")} {
		projectDirs, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, projectDir := range projectDirs {
			if !projectDir.IsDir() {
				continue
			}
			path := filepath.Join(base, projectDir.Name())
			projectRoot := readGeminiProjectRoot(path)
			if !matchesProject(query, projectRoot) {
				continue
			}

			files, _ := filepath.Glob(filepath.Join(path, "chats", "*.jsonl"))
			for _, file := range files {
				if err := streamGeminiFile(file, projectRoot, query, yield); err != nil {
					continue
				}
			}
		}
	}

	return nil
}

func readGeminiProjectRoot(path string) string {
	data, err := os.ReadFile(filepath.Join(path, ".project_root"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func streamGeminiFile(path, projectRoot string, query Query, yield func(Event) error) error {
	return scanJSONL(path, func(line []byte) error {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil
		}

		toolName, command, args := genericCommandFromObject(entry)
		if command == "" {
			return nil
		}

		timestamp := parseTimestamp(firstString(entry, "timestamp", "createdAt", "time"))
		if !afterCutoff(timestamp, query.Since) {
			return nil
		}

		return yield(Event{
			Provider:    "gemini",
			SessionID:   defaultString(firstString(entry, "sessionId", "session_id", "id"), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))),
			ProjectRoot: projectRoot,
			Timestamp:   timestamp,
			Source:      path,
			Kind:        "tool_call",
			ToolCall: &ToolCall{
				Name:      toolName,
				Command:   command,
				Arguments: args,
			},
			Metadata: line,
		})
	})
}
