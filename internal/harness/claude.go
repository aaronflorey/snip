package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type ClaudeProvider struct {
	BaseDir string
}

func (ClaudeProvider) ID() string { return "claude" }

func (p ClaudeProvider) Stream(query Query, yield func(Event) error) error {
	base := p.BaseDir
	if base == "" {
		base = filepath.Join(homeDir(), ".claude", "projects")
	}

	projectDirs, err := p.projectDirs(base, query)
	if err != nil || len(projectDirs) == 0 {
		return err
	}

	for _, dir := range projectDirs {
		for _, path := range claudeSessionFiles(dir) {
			if err := scanJSONL(path, func(line []byte) error {
				event, ok := parseClaudeLine(line, path, dir)
				if !ok || !afterCutoff(event.Timestamp, query.Since) {
					return nil
				}
				return yield(event)
			}); err != nil {
				continue
			}
		}
	}

	return nil

}

func (p ClaudeProvider) projectDirs(base string, query Query) ([]string, error) {
	if base == "" {
		return nil, nil
	}

	if query.All {
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		var dirs []string
		for _, entry := range entries {
			if entry.IsDir() {
				dirs = append(dirs, filepath.Join(base, entry.Name()))
			}
		}
		return dirs, nil
	}

	projectRoot := query.ProjectRoot
	if projectRoot == "" {
		projectRoot = query.CWD
	}
	if projectRoot == "" {
		return nil, nil
	}

	projectDir := filepath.Join(base, strings.ReplaceAll(projectRoot, string(os.PathSeparator), "-"))
	if _, err := os.Stat(projectDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	return []string{projectDir}, nil
}

func claudeSessionFiles(projectDir string) []string {
	var files []string
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		path := filepath.Join(projectDir, entry.Name())
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			files = append(files, path)
		}
		if entry.IsDir() {
			subagentDir := filepath.Join(path, "subagents")
			subEntries, err := os.ReadDir(subagentDir)
			if err != nil {
				continue
			}
			for _, subEntry := range subEntries {
				if !subEntry.IsDir() && strings.HasSuffix(subEntry.Name(), ".jsonl") {
					files = append(files, filepath.Join(subagentDir, subEntry.Name()))
				}
			}
		}
	}

	return files
}

type claudeLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	Message   *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message,omitempty"`
}

type claudeContentItem struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
}

type claudeBashInput struct {
	Command string `json:"command"`
}

func parseClaudeLine(line []byte, sourcePath, projectDir string) (Event, bool) {
	var entry claudeLine
	if err := json.Unmarshal(line, &entry); err != nil {
		return Event{}, false
	}
	if entry.Type != "assistant" || entry.Message == nil || entry.Message.Role != "assistant" {
		return Event{}, false
	}

	var content []claudeContentItem
	if err := json.Unmarshal(entry.Message.Content, &content); err != nil {
		return Event{}, false
	}

	for _, item := range content {
		if item.Type != "tool_use" || item.Name != "Bash" {
			continue
		}

		var input claudeBashInput
		if err := json.Unmarshal(item.Input, &input); err != nil || input.Command == "" {
			continue
		}

		return Event{
			Provider:    "claude",
			SessionID:   strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath)),
			ProjectRoot: projectDir,
			Timestamp:   parseTimestamp(entry.Timestamp),
			Source:      sourcePath,
			Kind:        "tool_call",
			ToolCall: &ToolCall{
				Name:      "Bash",
				Command:   input.Command,
				Arguments: item.Input,
			},
			Metadata: line,
		}, true
	}

	return Event{}, false
}
