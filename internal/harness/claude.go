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
	if _, err := os.Stat(base); err != nil {
		if os.IsNotExist(err) {
			emitDebug(query, p.ID(), "unavailable", "base dir not found: %s", base)
			return nil
		}
		emitDebug(query, p.ID(), "error", "stat %s: %v", base, err)
		return err
	}

	projectDirs, err := p.projectDirs(base, query)
	if err != nil {
		emitDebug(query, p.ID(), "error", "resolve project dirs: %v", err)
		return err
	}
	if len(projectDirs) == 0 {
		emitDebug(query, p.ID(), "scope_miss", "no project directories matched %q", defaultString(query.ProjectRoot, query.CWD))
		return nil
	}

	filesSeen := 0
	parseSkips := 0
	cutoffSkips := 0
	yielded := 0

	for _, dir := range projectDirs {
		files := claudeSessionFiles(dir)
		filesSeen += len(files)
		for _, path := range files {
			if err := scanJSONL(path, func(line []byte) error {
				event, ok := parseClaudeLine(line, path, dir)
				if !ok {
					parseSkips++
					return nil
				}
				if !afterCutoff(event.Timestamp, query.Since) {
					cutoffSkips++
					return nil
				}
				yielded++
				return yield(event)
			}); err != nil {
				emitDebug(query, p.ID(), "scan_error", "scan %s: %v", path, err)
				continue
			}
		}
	}
	if filesSeen == 0 {
		emitDebug(query, p.ID(), "no_files", "project dirs matched but no session jsonl files were found")
		return nil
	}
	emitDebug(query, p.ID(), "summary", "projects=%d files=%d yielded=%d skipped_cutoff=%d skipped_parse=%d", len(projectDirs), filesSeen, yielded, cutoffSkips, parseSkips)

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
		if len(dirs) == 0 {
			return nil, nil
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
