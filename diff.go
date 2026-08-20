package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitDiffFiles returns the list of files changed relative to a git ref.
// If ref is "staged", it runs `git diff --cached --name-only`; otherwise
// `git diff --name-only <ref>`.  Returns the files that exist on disk and
// are tracked.  Errors from git are returned to the caller.
func gitDiffFiles(ref string) ([]string, error) {
	if ref == "" {
		return nil, fmt.Errorf("git-diff: empty ref")
	}
	var cmd *exec.Cmd
	if ref == "staged" {
		cmd = exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=ACMR")
	} else {
		cmd = exec.Command("git", "diff", "--name-only", ref, "--diff-filter=ACMR")
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		// Common: not in a git repo, or ref doesn't exist.
		return nil, fmt.Errorf("git diff failed: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := os.Stat(line); err == nil {
			files = append(files, line)
		}
	}
	return files, nil
}
