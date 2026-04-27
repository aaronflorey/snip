package harness

import (
	"database/sql"
	"os"
	"path/filepath"
)

type OpenCodeProvider struct {
	DBPath string
}

func (OpenCodeProvider) ID() string { return "opencode" }

func (p OpenCodeProvider) Stream(query Query, yield func(Event) error) error {
	if !SQLiteAvailable {
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
		return nil
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`
		SELECT part.session_id, session.directory, part.time_created,
		       json_extract(part.data, '$.tool'),
		       json_extract(part.data, '$.state.input.command'),
		       part.data
		FROM part
		JOIN session ON session.id = part.session_id
		WHERE json_extract(part.data, '$.type') = 'tool'
		  AND json_extract(part.data, '$.tool') = 'bash'
		ORDER BY part.time_created ASC`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var sessionID, projectRoot, toolName, command string
		var timestamp int64
		var metadata []byte
		if err := rows.Scan(&sessionID, &projectRoot, &timestamp, &toolName, &command, &metadata); err != nil {
			continue
		}
		timestamp = normalizeUnixTimestamp(timestamp)
		if command == "" || !matchesProject(query, projectRoot) || !afterCutoff(timestamp, query.Since) {
			continue
		}

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
