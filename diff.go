package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// gitDiffFiles returns the list of files changed relative to a git ref.
// If ref is "staged", it runs `git diff --cached --name-only`; otherwise
// `git diff --name-only <ref>`.  Returns the files that exist on disk and
// are tracked.  Errors from git are returned to the caller.
func gitDiffFiles(ref string) ([]string, error) {
	return gitDiffFilesWithContext(context.Background(), ref)
}

func gitDiffFilesWithContext(ctx context.Context, ref string) ([]string, error) {
	if ref == "" {
		return nil, fmt.Errorf("git-diff: empty ref")
	}
	if strings.HasPrefix(ref, "-") {
		return nil, fmt.Errorf("git-diff: invalid ref %q", ref)
	}
	var cmd *exec.Cmd
	if ref == "staged" {
		cmd = exec.CommandContext(ctx, "git", "diff", "--cached", "--name-only", "--diff-filter=ACMR")
	} else {
		cmd = exec.CommandContext(ctx, "git", "diff", "--name-only", ref, "--diff-filter=ACMR")
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff failed: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	trimmed := strings.TrimSpace(out.String())
	if trimmed == "" {
		return []string{}, nil
	}
	var files []string
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := os.Stat(line); err == nil {
			files = append(files, line)
		}
	}
	if files == nil {
		files = []string{}
	}
	return files, nil
}

// hunkHeaderRE parses git diff hunk headers: @@ -oldStart[,oldCount] +newStart[,newCount] @@
var hunkHeaderRE = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// ChangedLines maps file path -> set of added line numbers (1-based, new-file side).
type ChangedLines map[string]map[int]struct{}

// parseChangedLines parses unified diff output (git diff --unified=0) and
// returns the set of added line numbers per file.
func parseChangedLines(diffText string) ChangedLines {
	if strings.TrimSpace(diffText) == "" {
		return nil
	}
	result := make(ChangedLines)
	var curFile string
	for _, rawLine := range strings.Split(diffText, "\n") {
		if strings.HasPrefix(rawLine, "+++ b/") {
			curFile = strings.TrimPrefix(rawLine, "+++ b/")
			continue
		}
		if m := hunkHeaderRE.FindStringSubmatch(rawLine); m != nil {
			start, _ := strconv.Atoi(m[1])
			count := 1
			if m[2] != "" {
				count, _ = strconv.Atoi(m[2])
			}
			if curFile != "" && count > 0 {
				if result[curFile] == nil {
					result[curFile] = make(map[int]struct{})
				}
				for i := 0; i < count; i++ {
					result[curFile][start+i] = struct{}{}
				}
			}
			continue
		}
	}
	return result
}

// gitDiffHunks returns the unified diff with zero context lines for the given ref.
func gitDiffHunks(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("git-diff: empty ref")
	}
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("git-diff: invalid ref %q", ref)
	}
	var cmd *exec.Cmd
	if ref == "staged" {
		cmd = exec.CommandContext(ctx, "git", "diff", "--cached", "--unified=0")
	} else {
		cmd = exec.CommandContext(ctx, "git", "diff", "--unified=0", ref)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git diff hunks failed: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}
