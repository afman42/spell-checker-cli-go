package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// loadSpellignore reads a .spellignore file (one glob pattern per line, #
// comments and blank lines ignored) from the working directory and returns
// its patterns.  Missing file → empty slice (no error).  The patterns are
// later merged with --exclude and built-in excludes by collectFiles.
func loadSpellignore(cwd string) []string {
	path := filepath.Join(cwd, ".spellignore")
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			// A missing file is normal; anything else (permissions,
			// corruption) must not silently drop user exclusions.
			fmt.Fprintf(os.Stderr, "Warning: could not read .spellignore: %v\n", err)
		}
		return nil
	}
	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}
