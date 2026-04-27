package harness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/edouard-claude/snip/internal/hook"
)

func NewQuery(all bool, cutoff time.Time) Query {
	cwd, _ := os.Getwd()
	return Query{
		All:         all,
		Since:       cutoff.UnixNano(),
		CWD:         cwd,
		ProjectRoot: detectProjectRoot(cwd),
	}
}

func detectProjectRoot(cwd string) string {
	if cwd == "" {
		return ""
	}

	path := filepath.Clean(cwd)
	for {
		gitPath := filepath.Join(path, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return path
		}

		parent := filepath.Dir(path)
		if parent == path {
			return cwd
		}
		path = parent
	}
}

func matchesProject(query Query, projectRoot string) bool {
	if query.All {
		return true
	}

	target := query.ProjectRoot
	if target == "" {
		target = query.CWD
	}
	if target == "" || projectRoot == "" {
		return false
	}

	target = filepath.Clean(target)
	projectRoot = filepath.Clean(projectRoot)

	return target == projectRoot || isChildPath(target, projectRoot) || isChildPath(projectRoot, target)
}

func isChildPath(parent, child string) bool {
	if parent == child {
		return true
	}
	return strings.HasPrefix(child, parent+string(os.PathSeparator))
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func scanJSONL(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	for scanner.Scan() {
		if err := fn(scanner.Bytes()); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func parseTimestamp(value string) int64 {
	if value == "" {
		return 0
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts.UnixNano()
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts.UnixNano()
	}
	return 0
}

func normalizeUnixTimestamp(ts int64) int64 {
	if ts <= 0 {
		return 0
	}

	// Session stores vary between seconds, milliseconds, microseconds, and
	// nanoseconds. Normalize them before applying a single cutoff.
	switch {
	case ts < 1_000_000_000_000:
		return ts * int64(time.Second)
	case ts < 1_000_000_000_000_000:
		return ts * int64(time.Millisecond)
	case ts < 1_000_000_000_000_000_000:
		return ts * int64(time.Microsecond)
	default:
		return ts
	}
}

func afterCutoff(ts int64, cutoff int64) bool {
	if ts == 0 || cutoff == 0 {
		return true
	}
	return ts >= cutoff
}

// ExtractBaseCommand normalizes a shell command to its base executable name.
func ExtractBaseCommand(cmd string) string {
	firstLine := cmd
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstSegment := hook.ExtractFirstSegment(firstLine)
	_, _, bareCmd := hook.ParseSegment(firstSegment)
	base := hook.BaseCommand(bareCmd)

	if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
		base = base[idx+1:]
	}

	return strings.Trim(base, "'\"")
}

func marshalRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func commandFromJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var args map[string]any
	if err := json.Unmarshal(raw, &args); err == nil {
		return commandFromMap(args)
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}

	return ""
}

func commandFromJSONString(text string) string {
	return commandFromJSON(json.RawMessage(text))
}

func commandFromMap(data map[string]any) string {
	if cmd, ok := data["command"].(string); ok {
		return cmd
	}
	if cmd, ok := data["cmd"].(string); ok {
		return cmd
	}
	if args, ok := data["args"].(map[string]any); ok {
		if cmd := commandFromMap(args); cmd != "" {
			return cmd
		}
	}
	return ""
}

func genericCommandFromObject(data map[string]any) (string, string, json.RawMessage) {
	toolName, _ := data["toolName"].(string)
	if toolName == "" {
		toolName, _ = data["name"].(string)
	}

	if cmd := commandFromMap(data); cmd != "" {
		return toolName, cmd, marshalRaw(data)
	}

	for _, key := range []string{"arguments", "toolArgs", "input", "payload", "data"} {
		raw := marshalRaw(data[key])
		if cmd := commandFromJSON(raw); cmd != "" {
			return toolName, cmd, raw
		}
	}

	return "", "", nil
}
