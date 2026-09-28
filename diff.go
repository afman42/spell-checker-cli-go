package main

import (
	"bytes"
	"context"
	"errors"
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
//
// ErrEmptyGitRef and ErrInvalidGitRef are returned for bad refs; match them
// with errors.Is.
var (
	ErrEmptyGitRef   = errors.New("git-diff: empty ref")
	ErrInvalidGitRef = errors.New("git-diff: invalid ref")
)

func validateGitRef(ref string) error {
	if ref == "" {
		return ErrEmptyGitRef
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidGitRef, ref)
	}
	return nil
}

func gitDiffFiles(ref string) ([]string, error) {
	return gitDiffFilesWithContext(context.Background(), ref)
}

// runGit executes git with args, capturing stdout. Stderr is folded into the
// returned error so callers surface git's own message (bad ref, not a repo).
func runGit(ctx context.Context, op string, args ...string) (string, error) {
	var out, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %s: %w", op, strings.TrimSpace(errBuf.String()), err)
	}
	return out.String(), nil
}

func gitDiffFilesWithContext(ctx context.Context, ref string) ([]string, error) {
	if err := validateGitRef(ref); err != nil {
		return nil, err
	}
	var args []string
	if ref == "staged" {
		args = []string{"diff", "--cached", "--name-only", "--diff-filter=ACMR"}
	} else {
		args = []string{"diff", "--name-only", ref, "--diff-filter=ACMR"}
	}
	out, err := runGit(ctx, "git diff", args...)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return []string{}, nil
	}
	files := []string{}
	for _, line := range strings.Split(trimmed, "\n") {
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

// hunkHeaderRE parses git diff hunk headers: @@ -oldStart[,oldCount] +newStart[,newCount] @@
var hunkHeaderRE = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// ChangedLines maps file path -> set of added line numbers (1-based, new-file side).
// It is read-only after parseChangedLines; concurrent reads are safe, writes
// require external synchronization.
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
			// m[1]/m[2] are \d+ per hunkHeaderRE, Atoi cannot fail.
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
	if err := validateGitRef(ref); err != nil {
		return "", err
	}
	var args []string
	if ref == "staged" {
		args = []string{"diff", "--cached", "--unified=0"}
	} else {
		args = []string{"diff", "--unified=0", ref}
	}
	return runGit(ctx, "git diff hunks", args...)
}
