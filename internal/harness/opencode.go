package harness

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type OpenCodeProvider struct {
	DBPath string
}

func (OpenCodeProvider) ID() string { return "opencode" }

func (p OpenCodeProvider) Stream(query Query, yield func(Event) error) error {
	if !SQLiteAvailable {
		emitDebug(query, p.ID(), "unavailable", "sqlite support is disabled in lite builds")
		return nil
	}

	dbPath := p.DBPath
	if dbPath == "" {
		dbPath = firstExistingPath(
			filepath.Join(homeDir(), ".local", "share", "opencode", "opencode.db"),
			filepath.Join(homeDir(), ".local", "share", "opencode", "opencode-prod.db"),
		)
	}
	if dbPath == "" {
		emitDebug(query, p.ID(), "unavailable", "no opencode sqlite database found")
		return nil
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		emitDebug(query, p.ID(), "error", "open %s: %v", dbPath, err)
		return nil
	}
	defer func() { _ = db.Close() }()

	statement, args := buildOpenCodeQuery(query)
	rows, err := db.Query(statement, args...)
	if err != nil {
		emitDebug(query, p.ID(), "error", "query %s: %v", dbPath, err)
		return nil
	}
	defer func() { _ = rows.Close() }()

	rowsSeen := 0
	scopeSkips := 0
	cutoffSkips := 0
	commandSkips := 0
	yielded := 0

	for rows.Next() {
		var sessionID, projectRoot, toolName, command string
		var timestamp int64
		var metadata []byte
		if err := rows.Scan(&sessionID, &projectRoot, &timestamp, &toolName, &command, &metadata); err != nil {
			continue
		}
		rowsSeen++
		timestamp = normalizeUnixTimestamp(timestamp)
		if command == "" {
			commandSkips++
			continue
		}
		if !matchesProject(query, projectRoot) {
			scopeSkips++
			continue
		}
		if !afterCutoff(timestamp, query.Since) {
			cutoffSkips++
			continue
		}
		yielded++

		if err := yield(Event{
			Provider:    "opencode",
			SessionID:   sessionID,
			ProjectRoot: projectRoot,
			Timestamp:   timestamp,
			Source:      dbPath,
			Kind:        "tool_call",
			ToolCall: &ToolCall{
				Name:      toolName,
				Command:   command,
				Arguments: metadata,
			},
			Metadata: metadata,
		}); err != nil {
			return err
		}
	}
	if rowsSeen == 0 {
		emitDebug(query, p.ID(), "no_rows", "database found but returned no bash tool rows")
		return nil
	}
	emitDebug(query, p.ID(), "summary", "rows=%d yielded=%d skipped_scope=%d skipped_cutoff=%d skipped_command=%d db=%s", rowsSeen, yielded, scopeSkips, cutoffSkips, commandSkips, dbPath)

	return nil
}

func firstExistingPath(paths ...string) string {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func buildOpenCodeQuery(query Query) (string, []any) {
	var statement strings.Builder
	args := make([]any, 0, 16)

	statement.WriteString(`
		SELECT part.session_id, session.directory, part.time_created,
		       json_extract(part.data, '$.tool'),
		       json_extract(part.data, '$.state.input.command'),
		       part.data
		FROM part
		JOIN session ON session.id = part.session_id
		WHERE json_extract(part.data, '$.type') = 'tool'
		  AND json_extract(part.data, '$.tool') = 'bash'`)

	if !query.All {
		target := openCodeQueryTarget(query)
		if target != "" {
			statement.WriteString(`
		  AND (`)

			scopeClauses := make([]string, 0, len(openCodeScopeParents(target))+2)
			scopeClauses = append(scopeClauses, "session.directory = ?", "session.directory LIKE ?")
			args = append(args, target, target+string(os.PathSeparator)+"%")

			for _, parent := range openCodeScopeParents(target) {
				scopeClauses = append(scopeClauses, "session.directory = ?")
				args = append(args, parent)
			}

			statement.WriteString(strings.Join(scopeClauses, " OR "))
			statement.WriteString(`)`)
		}
	}

	if cutoffClauses, cutoffArgs := openCodeCutoffSQL(query.Since); len(cutoffClauses) > 0 {
		statement.WriteString(`
		  AND (`)
		statement.WriteString(strings.Join(cutoffClauses, " OR "))
		statement.WriteString(`)`)
		args = append(args, cutoffArgs...)
	}

	statement.WriteString(`
		ORDER BY part.time_created ASC`)

	return statement.String(), args
}

func openCodeQueryTarget(query Query) string {
	target := query.ProjectRoot
	if target == "" {
		target = query.CWD
	}
	if target == "" {
		return ""
	}
	return filepath.Clean(target)
}

func openCodeScopeParents(target string) []string {
	parents := make([]string, 0, 8)
	for path := filepath.Dir(target); path != target; path = filepath.Dir(path) {
		parents = append(parents, path)
		next := filepath.Dir(path)
		if next == path {
			break
		}
	}
	return parents
}

func openCodeCutoffSQL(cutoff int64) ([]string, []any) {
	if cutoff == 0 {
		return nil, nil
	}

	clauses := []string{
		"(part.time_created < 1000000000000 AND part.time_created >= ?)",
		"(part.time_created >= 1000000000000 AND part.time_created < 1000000000000000 AND part.time_created >= ?)",
		"(part.time_created >= 1000000000000000 AND part.time_created < 1000000000000000000 AND part.time_created >= ?)",
		"(part.time_created >= 1000000000000000000 AND part.time_created >= ?)",
	}
	args := []any{
		openCodeCutoffUnit(cutoff, time.Second),
		openCodeCutoffUnit(cutoff, time.Millisecond),
		openCodeCutoffUnit(cutoff, time.Microsecond),
		cutoff,
	}
	return clauses, args
}

func openCodeCutoffUnit(cutoff int64, unit time.Duration) int64 {
	step := int64(unit)
	return (cutoff + step - 1) / step
}
