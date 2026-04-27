package discover

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/edouard-claude/snip/internal/config"
	"github.com/edouard-claude/snip/internal/display"
	"github.com/edouard-claude/snip/internal/filter"
	"github.com/edouard-claude/snip/internal/harness"
	"github.com/edouard-claude/snip/internal/utils"
)

// CommandStat tracks command occurrence counts.
type CommandStat struct {
	Name  string
	Count int
}

// Result holds the discover analysis output.
type Result struct {
	SessionsScanned  int
	TotalCommands    int
	Supported        []CommandStat
	Unsupported      []CommandStat
	SupportedCount   int
	UnsupportedCount int
}

// Options configures the discover scan.
type Options struct {
	All   bool
	Since int // days
}

// Run executes the discover command with the given CLI args.
func Run(args []string) error {
	opts := parseArgs(args)

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
	result := scan(harness.DefaultProviders(), cmdSet, query)
	if result.SessionsScanned == 0 {
		fmt.Fprintln(os.Stderr, "snip discover: no harness session data matched this repository; try --all to scan every project")
		return nil
	}

	printResult(result)
	return nil
}

// parseArgs extracts --all and --since flags from args.
func parseArgs(args []string) Options {
	opts := Options{Since: 7}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all":
			opts.All = true
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

// scan processes all harness events and classifies command usage.
func scan(providers []harness.Provider, supportedCmds map[string]struct{}, query harness.Query) Result {
	supported := make(map[string]int)
	unsupported := make(map[string]int)
	sessions := make(map[string]struct{})
	totalCmds := 0

	for _, provider := range providers {
		err := provider.Stream(query, func(event harness.Event) error {
			if event.ToolCall == nil || event.ToolCall.Command == "" {
				return nil
			}

			cmd := harness.ExtractBaseCommand(event.ToolCall.Command)
			if cmd == "" {
				return nil
			}

			totalCmds++
			sessions[event.Provider+":"+firstNonEmpty(event.SessionID, event.Source)] = struct{}{}
			if _, ok := supportedCmds[cmd]; ok {
				supported[cmd]++
			} else {
				unsupported[cmd]++
			}
			return nil
		})
		if err != nil {
			continue
		}
	}

	return Result{
		SessionsScanned:  len(sessions),
		TotalCommands:    totalCmds,
		Supported:        mapToStats(supported),
		Unsupported:      mapToStats(unsupported),
		SupportedCount:   sumMap(supported),
		UnsupportedCount: sumMap(unsupported),
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
		fmt.Println("  No Bash commands found in the scanned sessions.")
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
