package main

import (
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
