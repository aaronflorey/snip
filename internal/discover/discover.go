package discover

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aaronflorey/snip/internal/config"
	"github.com/aaronflorey/snip/internal/display"
	"github.com/aaronflorey/snip/internal/filter"
	"github.com/aaronflorey/snip/internal/harness"
	"github.com/aaronflorey/snip/internal/utils"
)

// CommandStat tracks command occurrence counts.
type CommandStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Result holds the discover analysis output.
type Result struct {
	SessionsScanned  int
	TotalCommands    int
	Supported        []CommandStat
	Unsupported      []CommandStat
	SupportedCount   int
	UnsupportedCount int
	MinCount         int
	Debug            []harness.DebugRecord
}

// Options configures the discover scan.
type Options struct {
	All      bool
	Debug    bool
	JSON     bool
	MinCount int
	Since    int // days
}

const defaultMinCount = 5

// Run executes the discover command with the given CLI args.
func Run(args []string) error {
	return RunWithOptions(parseArgs(args))
}

// RunWithOptions executes discover with parsed options.
func RunWithOptions(opts Options) error {
	if opts.Since <= 0 {
		opts.Since = 7
	}
	if opts.MinCount <= 0 {
		opts.MinCount = defaultMinCount
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	filters, err := filter.LoadAll(cfg.Filters.Dirs())
	if err != nil {
		return fmt.Errorf("load filters: %w", err)
	}

	registry := filter.NewRegistry(filters)
	supportedCmds := registry.Commands()
	cmdSet := make(map[string]struct{}, len(supportedCmds))
	for _, cmd := range supportedCmds {
		cmdSet[cmd] = struct{}{}
	}

	query := harness.NewQuery(opts.All, time.Now().AddDate(0, 0, -opts.Since))
	if !opts.Debug {
		query.Debug = nil
	}
	result := applyMinCount(scan(harness.DefaultProviders(), cmdSet, query), opts.MinCount)
	if opts.JSON {
		return printJSON(opts, query, result)
	}
	if opts.Debug {
		printDebug(opts, query, result)
	}
	if result.SessionsScanned == 0 {
		fmt.Fprintln(os.Stderr, "snip discover: no harness session data matched this repository; try --all to scan every project")
		return nil
	}

	printResult(result)
	return nil
}

// parseArgs extracts --all and --since flags from args.
func parseArgs(args []string) Options {
	opts := Options{Since: 7, MinCount: defaultMinCount}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all":
			opts.All = true
		case "--debug":
			opts.Debug = true
		case "--json":
			opts.JSON = true
		case "--since":
			if i+1 < len(args) {
				n := 0
				for _, c := range args[i+1] {
					if c >= '0' && c <= '9' {
						n = n*10 + int(c-'0')
					} else {
						break
					}
				}
				if n > 0 {
					opts.Since = n
				}
				i++
			}
		}
	}
	return opts
}

func applyMinCount(result Result, minCount int) Result {
	if minCount <= 1 {
		result.MinCount = minCount
		return result
	}

	result.Supported = filterStatsByMinCount(result.Supported, minCount)
	result.Unsupported = filterStatsByMinCount(result.Unsupported, minCount)
	result.SupportedCount = sumStats(result.Supported)
	result.UnsupportedCount = sumStats(result.Unsupported)
	result.TotalCommands = result.SupportedCount + result.UnsupportedCount
	result.MinCount = minCount
	return result
}

func filterStatsByMinCount(stats []CommandStat, minCount int) []CommandStat {
	filtered := make([]CommandStat, 0, len(stats))
	for _, stat := range stats {
		if stat.Count >= minCount {
			filtered = append(filtered, stat)
		}
	}
	return filtered
}

func sumStats(stats []CommandStat) int {
	total := 0
	for _, stat := range stats {
		total += stat.Count
	}
	return total
}

// scan processes all harness events and classifies command usage.
func scan(providers []harness.Provider, supportedCmds map[string]struct{}, query harness.Query) Result {
	supported := make(map[string]int)
	unsupported := make(map[string]int)
	sessions := make(map[string]struct{})
	debug := make([]harness.DebugRecord, 0)

	type providerResult struct {
		supported   map[string]int
		unsupported map[string]int
		sessions    map[string]struct{}
		totalCmds   int
		debug       []harness.DebugRecord
	}

	results := make([]providerResult, len(providers))
	var wg sync.WaitGroup
	var debugMu sync.Mutex

	for i, provider := range providers {
		wg.Add(1)
		go func(i int, provider harness.Provider) {
			defer wg.Done()

			result := providerResult{
				supported:   make(map[string]int),
				unsupported: make(map[string]int),
				sessions:    make(map[string]struct{}),
				debug:       make([]harness.DebugRecord, 0),
			}

			providerQuery := query
			providerQuery.Debug = chainDebug(func(record harness.DebugRecord) {
				debugMu.Lock()
				defer debugMu.Unlock()
				if query.Debug != nil {
					query.Debug(record)
				}
			}, func(record harness.DebugRecord) {
				result.debug = append(result.debug, record)
			})

			err := provider.Stream(providerQuery, func(event harness.Event) error {
				if event.ToolCall == nil || event.ToolCall.Command == "" {
					return nil
				}

				cmd := harness.ExtractBaseCommand(event.ToolCall.Command)
				if cmd == "" {
					return nil
				}

				result.totalCmds++
				result.sessions[event.Provider+":"+firstNonEmpty(event.SessionID, event.Source)] = struct{}{}
				if _, ok := supportedCmds[cmd]; ok {
					result.supported[cmd]++
				} else {
					result.unsupported[cmd]++
				}
				return nil
			})
			if err != nil {
				return
			}

			results[i] = result
		}(i, provider)
	}

	wg.Wait()

	totalCmds := 0
	for _, result := range results {
		totalCmds += result.totalCmds
		for session := range result.sessions {
			sessions[session] = struct{}{}
		}
		for cmd, count := range result.supported {
			supported[cmd] += count
		}
		for cmd, count := range result.unsupported {
			unsupported[cmd] += count
		}
		debug = append(debug, result.debug...)
	}

	return Result{
		SessionsScanned:  len(sessions),
		TotalCommands:    totalCmds,
		Supported:        mapToStats(supported),
		Unsupported:      mapToStats(unsupported),
		SupportedCount:   sumMap(supported),
		UnsupportedCount: sumMap(unsupported),
		Debug:            debug,
	}
}

func chainDebug(fns ...func(harness.DebugRecord)) func(harness.DebugRecord) {
	callbacks := make([]func(harness.DebugRecord), 0, len(fns))
	for _, fn := range fns {
		if fn != nil {
			callbacks = append(callbacks, fn)
		}
	}
	if len(callbacks) == 0 {
		return nil
	}
	return func(record harness.DebugRecord) {
		for _, fn := range callbacks {
			fn(record)
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// mapToStats converts a map[string]int to a sorted slice of CommandStat.
func mapToStats(m map[string]int) []CommandStat {
	stats := make([]CommandStat, 0, len(m))
	for name, count := range m {
		stats = append(stats, CommandStat{Name: name, Count: count})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Count != stats[j].Count {
			return stats[i].Count > stats[j].Count
		}
		return stats[i].Name < stats[j].Name
	})
	return stats
}

func sumMap(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}

func printDebug(opts Options, query harness.Query, result Result) {
	fmt.Println()
	fmt.Println("  snip — Discover Debug")
	fmt.Println("  " + display.FormatSeparator(30))
	fmt.Println()

	scope := defaultScope(opts, query)
	fmt.Printf("  Scope                 %s\n", scope)
	fmt.Printf("  Since                 %d days\n", opts.Since)
	fmt.Printf("  Working directory     %s\n", firstNonEmpty(query.CWD, "(unknown)"))
	fmt.Printf("  Project root          %s\n", firstNonEmpty(query.ProjectRoot, "(unknown)"))
	fmt.Println()

	if len(result.Debug) == 0 {
		fmt.Println("  No provider diagnostics were emitted.")
		fmt.Println()
		return
	}

	for _, record := range result.Debug {
		fmt.Printf("  [%s] %s: %s\n", record.Provider, strings.ReplaceAll(record.Code, "_", " "), record.Message)
	}
	fmt.Println()
}

func defaultScope(opts Options, query harness.Query) string {
	if opts.All {
		return "all projects"
	}
	return firstNonEmpty(query.ProjectRoot, query.CWD, "(unknown)")
}

func printJSON(opts Options, query harness.Query, result Result) error {
	response := struct {
		Scope            string                `json:"scope"`
		All              bool                  `json:"all"`
		SinceDays        int                   `json:"since_days"`
		MinCount         int                   `json:"min_count"`
		WorkingDirectory string                `json:"working_directory,omitempty"`
		ProjectRoot      string                `json:"project_root,omitempty"`
		SessionsScanned  int                   `json:"sessions_scanned"`
		TotalCommands    int                   `json:"total_commands"`
		SupportedCount   int                   `json:"supported_count"`
		UnsupportedCount int                   `json:"unsupported_count"`
		CoveragePercent  float64               `json:"coverage_percent"`
		Supported        []CommandStat         `json:"supported"`
		Unsupported      []CommandStat         `json:"unsupported"`
		Debug            []harness.DebugRecord `json:"debug,omitempty"`
	}{
		Scope:            defaultScope(opts, query),
		All:              opts.All,
		SinceDays:        opts.Since,
		MinCount:         result.MinCount,
		WorkingDirectory: query.CWD,
		ProjectRoot:      query.ProjectRoot,
		SessionsScanned:  result.SessionsScanned,
		TotalCommands:    result.TotalCommands,
		SupportedCount:   result.SupportedCount,
		UnsupportedCount: result.UnsupportedCount,
		Supported:        result.Supported,
		Unsupported:      result.Unsupported,
	}
	if opts.Debug {
		response.Debug = result.Debug
	}
	if result.TotalCommands > 0 {
		response.CoveragePercent = float64(result.SupportedCount) / float64(result.TotalCommands) * 100
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(response)
}

// printResult outputs the discover report to stdout.
func printResult(r Result) {
	tty := display.IsTerminal()

	fmt.Println()
	if tty {
		fmt.Println(display.HeaderStyle.Render("  snip — Discover Report"))
		fmt.Println(display.DimStyle.Render("  " + display.FormatSeparator(30)))
	} else {
		fmt.Println("  snip — Discover Report")
		fmt.Println("  " + display.FormatSeparator(30))
	}
	fmt.Println()

	if r.TotalCommands == 0 {
		if r.MinCount > 1 {
			fmt.Printf("  No commands met the minimum count threshold of %d.\n", r.MinCount)
		} else {
			fmt.Println("  No Bash commands found in the scanned sessions.")
		}
		return
	}

	supportedPct := float64(r.SupportedCount) / float64(r.TotalCommands) * 100

	printKPI := func(label, value string, styled bool) {
		if tty {
			styledValue := value
			if !styled {
				styledValue = display.StatStyle.Render(value)
			}
			fmt.Printf("  %s  %s\n", display.DimStyle.Render(fmt.Sprintf("%-20s", label)), styledValue)
		} else {
			fmt.Printf("  %-20s  %s\n", label, value)
		}
	}

	printKPI("Sessions scanned", fmt.Sprintf("%d", r.SessionsScanned), false)
	printKPI("Commands found", fmt.Sprintf("%d", r.TotalCommands), false)
	printKPI("Filter coverage", display.ColorSavings(supportedPct), true)

	bar := display.ColorBar(r.SupportedCount, r.TotalCommands, 20)
	fmt.Println()
	if tty {
		fmt.Printf("  %s %s\n", bar, display.DimStyle.Render(fmt.Sprintf("%.0f%%", supportedPct)))
	} else {
		fmt.Printf("  %s %.0f%%\n", bar, supportedPct)
	}
	fmt.Println()

	// Subtract 9 to leave room for the Count column and separator; prevents
	// long Windows paths from overflowing into the adjacent column.
	maxCmd := display.TerminalWidth() - 9
	if maxCmd < 20 {
		maxCmd = 20
	}

	printSection := func(title string, count int, pct float64, stats []CommandStat) {
		if tty {
			fmt.Println(display.DimStyle.Render(fmt.Sprintf("  %s — %d commands (%.0f%%)", title, count, pct)))
		} else {
			fmt.Printf("  %s — %d commands (%.0f%%)\n", title, count, pct)
		}
		fmt.Println()

		if len(stats) > 0 {
			rows := make([][]string, 0, len(stats))
			for _, s := range stats {
				rows = append(rows, []string{utils.Truncate(s.Name, maxCmd), fmt.Sprintf("%d", s.Count)})
			}
			fmt.Print(display.FormatTable([]string{"Command", "Count"}, rows))
			fmt.Println()
		}
	}

	printSection("Supported", r.SupportedCount, supportedPct, r.Supported)
	printSection("Unsupported", r.UnsupportedCount, 100-supportedPct, r.Unsupported)

	if tty {
		fmt.Println(display.StatStyle.Render(fmt.Sprintf("  %.0f%% of your commands already have snip filters.", supportedPct)))
	} else {
		fmt.Printf("  %.0f%% of your commands already have snip filters.\n", supportedPct)
	}
	fmt.Println()
}
