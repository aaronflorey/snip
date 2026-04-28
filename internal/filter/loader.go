package filter

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronflorey/snip/internal/trust"
)

// isYAMLFile returns true if the filename ends with .yaml or .yml.
func isYAMLFile(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

// EmbeddedFS is set by the main package to provide embedded filter files.
// This avoids go:embed constraints on internal packages.
var EmbeddedFS *embed.FS

// LoadEmbedded loads all embedded YAML filter files.
func LoadEmbedded() ([]Filter, error) {
	if EmbeddedFS == nil {
		return nil, nil
	}

	// Try "filters" subdir first (when embedded from root), then "." (flat)
	dir := "filters"
	entries, err := EmbeddedFS.ReadDir(dir)
	if err != nil {
		dir = "."
		entries, err = EmbeddedFS.ReadDir(dir)
		if err != nil {
			return nil, nil
		}
	}

	var filters []Filter
	for _, entry := range entries {
		if entry.IsDir() || !isYAMLFile(entry.Name()) {
			continue
		}
		path := entry.Name()
		if dir != "." {
			path = dir + "/" + entry.Name()
		}
		data, err := EmbeddedFS.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read embedded filter %s: %w", entry.Name(), err)
		}
		f, err := ParseFilter(data)
		if err != nil {
			return nil, fmt.Errorf("parse embedded filter %s: %w", entry.Name(), err)
		}
		filters = append(filters, *f)
	}
	return filters, nil
}

// LoadUserFilters loads all YAML files from a directory.
func LoadUserFilters(dir string) ([]Filter, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read filter dir: %w", err)
	}

	var filters []Filter
	for _, entry := range entries {
		if entry.IsDir() || !isYAMLFile(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read user filter %s: %w", entry.Name(), err)
		}
		f, err := ParseFilter(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "snip: skipping invalid filter %s: %v\n", entry.Name(), err)
			continue
		}
		filters = append(filters, *f)
	}
	return filters, nil
}

// LoadUserFiltersTrusted loads YAML files from a directory, checking each
// file against the trust store. Untrusted files are skipped with a warning.
func LoadUserFiltersTrusted(dir string, store trust.Store) ([]Filter, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read filter dir: %w", err)
	}

	var filters []Filter
	for _, entry := range entries {
		if entry.IsDir() || !isYAMLFile(entry.Name()) {
			continue
		}
		filePath := filepath.Join(dir, entry.Name())
		if !trust.IsTrusted(store, filePath) {
			fmt.Fprintf(os.Stderr, "snip: skipping untrusted filter %s (run 'snip trust %s' to trust)\n", filePath, filePath)
			continue
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read user filter %s: %w", entry.Name(), err)
		}
		f, err := ParseFilter(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "snip: skipping invalid filter %s: %v\n", entry.Name(), err)
			continue
		}
		filters = append(filters, *f)
	}
	return filters, nil
}

// LoadAll loads filters from multiple user directories and embedded filters,
// merging by name. Later directories override earlier ones; all user filters
// override embedded filters. Project-local directories (not under
// ~/.config/snip/) are checked against the trust store loaded from disk.
func LoadAll(userDirs []string) ([]Filter, error) {
	return LoadAllWithStore(userDirs, nil)
}

// LoadAllWithStore loads filters like LoadAll but accepts an explicit trust
// store. If store is nil, the trust store is loaded from disk on first use.
// Pass a non-nil (possibly empty) store to skip disk I/O (useful for tests).
func LoadAllWithStore(userDirs []string, store trust.Store) ([]Filter, error) {
	embedded, err := LoadEmbedded()
	if err != nil {
		return nil, err
	}

	byName := make(map[string]int) // name -> index in result
	var result []Filter

	for _, f := range embedded {
		byName[f.Name] = len(result)
		result = append(result, f)
	}

	// Load trust store once for all project-local dirs (lazy, from disk)
	var storeLoaded bool
	if store != nil {
		storeLoaded = true
	}

	for _, dir := range userDirs {
		var user []Filter
		if trust.IsGlobalDir(dir) {
			user, err = LoadUserFilters(dir)
		} else {
			// Lazy-load trust store on first project-local dir
			if !storeLoaded {
				store, err = trust.Load()
				if err != nil {
					// If trust store is unreadable, treat all project-local
					// filters as untrusted (safe default).
					fmt.Fprintf(os.Stderr, "snip: cannot load trust store: %v\n", err)
					store = make(trust.Store)
				}
				storeLoaded = true
			}
			user, err = LoadUserFiltersTrusted(dir, store)
		}
		if err != nil {
			return nil, err
		}
		for _, f := range user {
			if idx, exists := byName[f.Name]; exists {
				result[idx] = f
			} else {
				byName[f.Name] = len(result)
				result = append(result, f)
			}
		}
	}

	return result, nil
}
