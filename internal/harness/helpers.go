package harness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aaronflorey/snip/internal/hook"
	"mvdan.cc/sh/v3/syntax"
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

func emitDebug(query Query, provider, code, format string, args ...any) {
	if query.Debug == nil {
		return
	}
	query.Debug(DebugRecord{
		Provider: provider,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
	})
}

// ExtractBaseCommand normalizes a shell command to its base executable name.
func ExtractBaseCommand(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return ""
	}

	if base, ok := extractBaseCommandFast(trimmed); ok {
		return base
	}

	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(trimmed), "discover")
	if err != nil || file == nil {
		return extractBaseCommandFallback(trimmed)
	}

	return extractCommandFromStmts(file.Stmts)
}

func extractBaseCommandFast(cmd string) (string, bool) {
	if !isSimpleCommandFastPath(cmd) {
		return "", false
	}

	_, _, bareCmd := hook.ParseSegment(cmd)
	args, ok := splitSimpleCommandArgs(bareCmd)
	if !ok || len(args) == 0 {
		return "", false
	}

	if requiresASTCommandParse(args[0]) {
		return "", false
	}

	return resolveCommandArgs(args), true
}

func isSimpleCommandFastPath(cmd string) bool {
	if strings.TrimSpace(cmd) == "" {
		return false
	}
	if segment := strings.TrimSpace(hook.ExtractFirstSegment(cmd)); segment != cmd {
		return false
	}

	var quote byte
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]

		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}

		switch ch {
		case '\'', '"':
			quote = ch
		case '$', '`', '\\', '(', ')', '{', '}', '<', '>', '!', '#', '\n', '\r':
			return false
		}
	}

	return quote == 0
}

func splitSimpleCommandArgs(cmd string) ([]string, bool) {
	var args []string
	var builder strings.Builder
	var quote byte

	flush := func() {
		if builder.Len() == 0 {
			return
		}
		args = append(args, builder.String())
		builder.Reset()
	}

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]

		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			builder.WriteByte(ch)
			continue
		}

		switch ch {
		case '\'', '"':
			quote = ch
		case ' ', '\t':
			flush()
		default:
			builder.WriteByte(ch)
		}
	}

	if quote != 0 {
		return nil, false
	}

	flush()
	return args, len(args) > 0
}

func requiresASTCommandParse(name string) bool {
	switch normalizeCommandName(name) {
	case "", "bash", "builtin", "bunx", "command", "dash", "env", "for", "if", "mise", "npx", "pnpm", "rtk", "sh", "snip", "sudo", "time", "while", "yarn", "zsh":
		return true
	default:
		return false
	}
}

func extractCommandFromStmts(stmts []*syntax.Stmt) string {
	for _, stmt := range stmts {
		if cmd := extractCommandFromStmt(stmt); cmd != "" {
			return cmd
		}
	}
	return ""
}

func extractCommandFromStmt(stmt *syntax.Stmt) string {
	if stmt == nil || stmt.Cmd == nil {
		return ""
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		return extractCommandFromCall(cmd)
	case *syntax.BinaryCmd:
		if left := extractCommandFromStmt(cmd.X); left != "" {
			return left
		}
		return extractCommandFromStmt(cmd.Y)
	case *syntax.Block:
		return extractCommandFromStmts(cmd.Stmts)
	case *syntax.Subshell:
		return extractCommandFromStmts(cmd.Stmts)
	case *syntax.IfClause:
		if thenCmd := extractCommandFromStmts(cmd.Then); thenCmd != "" {
			return thenCmd
		}
		if cmd.Else != nil {
			return extractCommandFromIfClause(cmd.Else)
		}
	case *syntax.WhileClause:
		return extractCommandFromStmts(cmd.Do)
	case *syntax.ForClause:
		return extractCommandFromStmts(cmd.Do)
	case *syntax.CaseClause:
		for _, item := range cmd.Items {
			if caseCmd := extractCommandFromStmts(item.Stmts); caseCmd != "" {
				return caseCmd
			}
		}
	case *syntax.TimeClause:
		return extractCommandFromStmt(cmd.Stmt)
	case *syntax.CoprocClause:
		return extractCommandFromStmt(cmd.Stmt)
	case *syntax.FuncDecl:
		return ""
	}

	return ""
}

func extractCommandFromIfClause(clause *syntax.IfClause) string {
	if clause == nil {
		return ""
	}
	if thenCmd := extractCommandFromStmts(clause.Then); thenCmd != "" {
		return thenCmd
	}
	if clause.Else != nil {
		return extractCommandFromIfClause(clause.Else)
	}
	return ""
}

func extractCommandFromCall(call *syntax.CallExpr) string {
	if call == nil || len(call.Args) == 0 {
		return ""
	}

	args := literalArgs(call.Args)
	if len(args) == 0 {
		return ""
	}

	return resolveCommandArgs(args)
}

func literalArgs(words []*syntax.Word) []string {
	args := make([]string, 0, len(words))
	for _, word := range words {
		literal, ok := literalWord(word)
		if !ok {
			args = append(args, "")
			continue
		}
		args = append(args, literal)
	}
	return args
}

func literalWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}

	var builder strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			builder.WriteString(part.Value)
		case *syntax.SglQuoted:
			builder.WriteString(part.Value)
		case *syntax.DblQuoted:
			literal, ok := literalWordParts(part.Parts)
			if !ok {
				return "", false
			}
			builder.WriteString(literal)
		default:
			return "", false
		}
	}
	return builder.String(), true
}

func literalWordParts(parts []syntax.WordPart) (string, bool) {
	var builder strings.Builder
	for _, part := range parts {
		switch part := part.(type) {
		case *syntax.Lit:
			builder.WriteString(part.Value)
		case *syntax.SglQuoted:
			builder.WriteString(part.Value)
		case *syntax.DblQuoted:
			literal, ok := literalWordParts(part.Parts)
			if !ok {
				return "", false
			}
			builder.WriteString(literal)
		default:
			return "", false
		}
	}
	return builder.String(), true
}

func resolveCommandArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	name := normalizeCommandName(args[0])
	if name == "" {
		return ""
	}

	switch name {
	case "[", "test":
		return ""
	case "command":
		if unwrapped := unwrapCommandArgs(args[1:]); len(unwrapped) > 0 {
			return resolveCommandArgs(unwrapped)
		}
		return ""
	case "builtin":
		if unwrapped := skipLeadingOptions(args[1:]); len(unwrapped) > 0 {
			return resolveCommandArgs(unwrapped)
		}
		return ""
	case "env":
		if unwrapped := unwrapEnvArgs(args[1:]); len(unwrapped) > 0 {
			return resolveCommandArgs(unwrapped)
		}
		return ""
	case "sudo":
		if unwrapped := unwrapSudoArgs(args[1:]); len(unwrapped) > 0 {
			return resolveCommandArgs(unwrapped)
		}
		return ""
	case "rtk", "snip":
		if unwrapped := skipLeadingOptions(args[1:]); len(unwrapped) > 0 {
			return resolveCommandArgs(unwrapped)
		}
		return name
	case "npx", "bunx":
		if unwrapped := skipLeadingOptions(args[1:]); len(unwrapped) > 0 {
			return resolveCommandArgs(unwrapped)
		}
		return name
	case "pnpm":
		if unwrapped := unwrapSubcommandRunner(args[1:], "exec", "dlx"); len(unwrapped) > 0 {
			return resolveCommandArgs(unwrapped)
		}
		return name
	case "yarn":
		if unwrapped := unwrapSubcommandRunner(args[1:], "dlx", "exec"); len(unwrapped) > 0 {
			return resolveCommandArgs(unwrapped)
		}
		return name
	case "mise":
		if unwrapped := unwrapMiseArgs(args[1:]); len(unwrapped) > 0 {
			return resolveCommandArgs(unwrapped)
		}
		return name
	case "bash", "sh", "zsh", "dash":
		if nested := unwrapShellCommand(args[1:]); nested != "" {
			return nested
		}
	}

	if isIgnoredCommand(name) {
		return ""
	}

	return name
}

func normalizeCommandName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "'\"")
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		name = name[idx+1:]
	}
	if !isCommandToken(name) {
		return ""
	}
	return name
}

func isCommandToken(name string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "$(") || strings.HasPrefix(name, "(") {
		return false
	}
	if strings.Contains(name, "=") {
		return false
	}
	return true
}

func isIgnoredCommand(name string) bool {
	switch name {
	case ".", ":", "alias", "bg", "bind", "break", "builtin", "cd", "command",
		"continue", "declare", "dirs", "disown", "echo", "enable", "eval", "exec",
		"exit", "export", "fc", "fg", "getopts", "hash", "help", "history", "jobs",
		"kill", "let", "local", "logout", "popd", "printf", "pushd", "pwd",
		"read", "readonly", "return", "set", "shift", "shopt", "source", "suspend",
		"times", "trap", "type", "typeset", "ulimit", "umask", "unalias", "unset",
		"wait", "which":
		return true
	default:
		return false
	}
}

func unwrapCommandArgs(args []string) []string {
	for len(args) > 0 {
		if args[0] == "--" {
			return args[1:]
		}
		if args[0] == "-v" || args[0] == "-V" {
			return nil
		}
		if args[0] == "-p" {
			args = args[1:]
			continue
		}
		if strings.HasPrefix(args[0], "-") {
			args = args[1:]
			continue
		}
		break
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

func unwrapEnvArgs(args []string) []string {
	for len(args) > 0 {
		if args[0] == "--" {
			return args[1:]
		}
		if strings.HasPrefix(args[0], "-") {
			args = args[1:]
			continue
		}
		if strings.Contains(args[0], "=") {
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func unwrapSudoArgs(args []string) []string {
	for len(args) > 0 {
		if args[0] == "--" {
			return args[1:]
		}
		if strings.HasPrefix(args[0], "-") {
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func unwrapSubcommandRunner(args []string, subcommands ...string) []string {
	if len(args) == 0 {
		return nil
	}
	for _, subcommand := range subcommands {
		if args[0] == subcommand {
			return skipLeadingOptions(args[1:])
		}
	}
	return nil
}

func unwrapMiseArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	if args[0] != "x" && args[0] != "exec" {
		return nil
	}
	args = args[1:]
	for len(args) > 0 {
		if args[0] == "--" {
			return args[1:]
		}
		args = args[1:]
	}
	return nil
}

func unwrapShellCommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			if i+1 >= len(args) {
				return ""
			}
			return ExtractBaseCommand(args[i+1])
		}
		if strings.HasPrefix(args[i], "-") && strings.Contains(args[i], "c") {
			if i+1 >= len(args) {
				return ""
			}
			return ExtractBaseCommand(args[i+1])
		}
		if strings.HasPrefix(args[i], "-") {
			continue
		}
		break
	}
	return ""
}

func skipLeadingOptions(args []string) []string {
	for len(args) > 0 {
		if args[0] == "--" {
			return args[1:]
		}
		if !strings.HasPrefix(args[0], "-") || args[0] == "-" {
			break
		}
		args = args[1:]
	}
	return args
}

func extractBaseCommandFallback(cmd string) string {
	firstLine := cmd
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstSegment := hook.ExtractFirstSegment(firstLine)
	_, _, bareCmd := hook.ParseSegment(firstSegment)
	base := hook.BaseCommand(bareCmd)
	base = normalizeCommandName(base)
	if isIgnoredCommand(base) {
		return ""
	}
	return base
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
