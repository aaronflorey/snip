package harness

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
)

type CopilotProvider struct {
	RootDir string
}

func (CopilotProvider) ID() string { return "copilot" }

func (p CopilotProvider) Stream(query Query, yield func(Event) error) error {
	root := p.RootDir
	if root == "" {
		root = filepath.Join(homeDir(), ".copilot", "session-state")
	}

	files, ok := p.candidateFiles(root, query)
	if !ok {
		files, _ = filepath.Glob(filepath.Join(root, "*", "events.jsonl"))
	}
	if len(files) == 0 {
		if ok {
			emitDebug(query, p.ID(), "summary", "files=0 yielded=0 skipped_scope=0 skipped_cutoff=0 skipped_command=0")
		} else {
			emitDebug(query, p.ID(), "unavailable", "no copilot session-state events.jsonl files found under %s", root)
		}
		return nil
	}
	stats := &providerDebugStats{}
	for _, path := range files {
		stats.Files++
		if err := streamCopilotFile(path, query, stats, yield); err != nil {
			emitDebug(query, p.ID(), "scan_error", "scan %s: %v", path, err)
			continue
		}
	}
	emitDebug(query, p.ID(), "summary", "files=%d yielded=%d skipped_scope=%d skipped_cutoff=%d skipped_command=%d", stats.Files, stats.Yielded, stats.ScopeSkips, stats.CutoffSkips, stats.CommandSkips)
	return nil
}

func (p CopilotProvider) candidateFiles(root string, query Query) ([]string, bool) {
	if query.All || !SQLiteAvailable {
		return nil, false
	}

	dbPath := filepath.Join(filepath.Dir(root), "session-store.db")
	if firstExistingPath(dbPath) == "" {
		return nil, false
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, false
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`SELECT id, cwd, repository FROM sessions`)
	if err != nil {
		return nil, false
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[string]struct{})
	var files []string
	for rows.Next() {
		var sessionID, cwd, repository string
		if err := rows.Scan(&sessionID, &cwd, &repository); err != nil {
			continue
		}
		if sessionID == "" {
			continue
		}
		if !matchesProject(query, cwd) && !matchesProject(query, repository) {
			continue
		}
		path := filepath.Join(root, sessionID, "events.jsonl")
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}

	return files, true
}

type copilotLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type copilotSessionStart struct {
	SessionID string `json:"sessionId"`
	Context   struct {
		CWD     string `json:"cwd"`
		GitRoot string `json:"gitRoot"`
	} `json:"context"`
}

type copilotToolExec struct {
	ToolName  string          `json:"toolName"`
	Arguments json.RawMessage `json:"arguments"`
}

type copilotHookStart struct {
	Input struct {
		ToolName string          `json:"toolName"`
		ToolArgs json.RawMessage `json:"toolArgs"`
	} `json:"input"`
}

func streamCopilotFile(path string, query Query, stats *providerDebugStats, yield func(Event) error) error {
	var sessionID string
	var projectRoot string

	return scanJSONL(path, func(line []byte) error {
		var entry copilotLine
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil
		}

		switch entry.Type {
		case "session.start":
			var start copilotSessionStart
			if err := json.Unmarshal(entry.Data, &start); err == nil {
				sessionID = start.SessionID
				projectRoot = defaultString(start.Context.GitRoot, start.Context.CWD)
			}
			return nil
		case "tool.execution_start":
			var exec copilotToolExec
			if err := json.Unmarshal(entry.Data, &exec); err != nil {
				return nil
			}
			return yieldCopilotEvent(path, query, stats, sessionID, projectRoot, entry.Timestamp, exec.ToolName, exec.Arguments, entry.Data, yield)
		case "hook.start":
			var hookStart copilotHookStart
			if err := json.Unmarshal(entry.Data, &hookStart); err != nil {
				return nil
			}
			return yieldCopilotEvent(path, query, stats, sessionID, projectRoot, entry.Timestamp, hookStart.Input.ToolName, hookStart.Input.ToolArgs, entry.Data, yield)
		}

		return nil
	})
}

func yieldCopilotEvent(path string, query Query, stats *providerDebugStats, sessionID, projectRoot, timestamp, toolName string, args json.RawMessage, metadata json.RawMessage, yield func(Event) error) error {
	if !matchesProject(query, projectRoot) {
		stats.ScopeSkips++
		return nil
	}
	command := commandFromJSON(args)
	if command == "" {
		stats.CommandSkips++
		return nil
	}
	ts := parseTimestamp(timestamp)
	if !afterCutoff(ts, query.Since) {
		stats.CutoffSkips++
		return nil
	}
	stats.Yielded++

	return yield(Event{
		Provider:    "copilot",
		SessionID:   defaultString(sessionID, filepath.Base(filepath.Dir(path))),
		ProjectRoot: projectRoot,
		Timestamp:   ts,
		Source:      path,
		Kind:        "tool_call",
		ToolCall: &ToolCall{
			Name:      toolName,
			Command:   command,
			Arguments: args,
		},
		Metadata: metadata,
	})
}
