// SPDX-License-Identifier: MIT
package welcome

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const pathSep = string(os.PathSeparator)

// resolveWorkingDirPath expands ~ and makes raw absolute, relative to cwd.
// Empty raw resolves to cwd (".").
func resolveWorkingDirPath(cwd string, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "."
	}
	trimmed = expandHome(trimmed)
	if filepath.IsAbs(trimmed) {
		return trimmed, nil
	}
	return filepath.Join(cwd, trimmed), nil
}

// workingDirExists resolves raw and returns the absolute path if it is an
// existing directory; otherwise a descriptive error.
func workingDirExists(cwd string, raw string) (string, error) {
	path, err := resolveWorkingDirPath(cwd, raw)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("working directory not found: %s", path)
		}
		return "", fmt.Errorf("checking working directory %s: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", path)
	}
	return path, nil
}

// dirSuggestions returns completion candidates for the text `value` as if the
// user were typing a working directory path. Each returned suggestion is a
// superstring of `value` that swaps the trailing path segment for a matching
// directory name and appends a path separator so it is ready to drop into
// the input field as-is.
func dirSuggestions(cwd string, value string) []string {
	// Split the literal value the user typed (NOT the expanded form) into
	// (valuePrefix, namePrefix). This preserves trailing separators which
	// filepath.Clean would otherwise strip, and keeps the ~-prefixed form
	// intact so suggestions start with the exact text the user sees.
	valuePrefix := ""
	namePrefix := value
	if idx := strings.LastIndex(value, pathSep); idx >= 0 {
		valuePrefix = value[:idx+1]
		namePrefix = value[idx+1:]
	}

	// Figure out which directory on disk to enumerate by expanding just the
	// directory portion of the typed value.
	expandedBase := expandHome(valuePrefix)
	var baseDir string
	switch {
	case expandedBase == "":
		baseDir = cwd
	case filepath.IsAbs(expandedBase):
		baseDir = expandedBase
	default:
		baseDir = filepath.Join(cwd, expandedBase)
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}

	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if namePrefix != "" && !strings.HasPrefix(name, namePrefix) {
			continue
		}
		out = append(out, valuePrefix+name+pathSep)
	}
	sort.Strings(out)
	return out
}

// longestCommonPrefix returns the longest string that is a prefix of every
// element in items. Returns "" when items is empty.
func longestCommonPrefix(items []string) string {
	if len(items) == 0 {
		return ""
	}
	prefix := items[0]
	for _, s := range items[1:] {
		n := len(prefix)
		if len(s) < n {
			n = len(s)
		}
		i := 0
		for i < n && prefix[i] == s[i] {
			i++
		}
		prefix = prefix[:i]
		if prefix == "" {
			return ""
		}
	}
	return prefix
}
