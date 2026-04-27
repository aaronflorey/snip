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
	stats := &providerDebugStats{}
	projectDirsSeen := 0

	for _, base := range []string{filepath.Join(root, "tmp"), filepath.Join(root, "history")} {
		projectDirs, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, projectDir := range projectDirs {
			if !projectDir.IsDir() {
				continue
			}
			projectDirsSeen++
			path := filepath.Join(base, projectDir.Name())
			projectRoot := readGeminiProjectRoot(path)
			if !matchesProject(query, projectRoot) {
				stats.ScopeSkips++
				continue
			}

			files, _ := filepath.Glob(filepath.Join(path, "chats", "*.jsonl"))
			stats.Files += len(files)
			for _, file := range files {
				if err := streamGeminiFile(file, projectRoot, query, stats, yield); err != nil {
					emitDebug(query, p.ID(), "scan_error", "scan %s: %v", file, err)
					continue
				}
			}
		}
	}
	if projectDirsSeen == 0 {
		emitDebug(query, p.ID(), "unavailable", "no gemini project directories found under %s", root)
		return nil
	}
	if stats.Files == 0 {
		emitDebug(query, p.ID(), "no_files", "project dirs=%d matched_scope_files=%d skipped_scope=%d", projectDirsSeen, stats.Files, stats.ScopeSkips)
		return nil
	}
	emitDebug(query, p.ID(), "summary", "project_dirs=%d files=%d yielded=%d skipped_scope=%d skipped_cutoff=%d skipped_command=%d", projectDirsSeen, stats.Files, stats.Yielded, stats.ScopeSkips, stats.CutoffSkips, stats.CommandSkips)

	return nil
}

func readGeminiProjectRoot(path string) string {
	data, err := os.ReadFile(filepath.Join(path, ".project_root"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func streamGeminiFile(path, projectRoot string, query Query, stats *providerDebugStats, yield func(Event) error) error {
	return scanJSONL(path, func(line []byte) error {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil
		}

		toolName, command, args := genericCommandFromObject(entry)
		if command == "" {
			stats.CommandSkips++
			return nil
		}

		timestamp := parseTimestamp(firstString(entry, "timestamp", "createdAt", "time"))
		if !afterCutoff(timestamp, query.Since) {
			stats.CutoffSkips++
			return nil
		}
		stats.Yielded++

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
