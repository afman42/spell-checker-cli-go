package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FixResult summarizes what was changed (or would be changed) in one file.
type FixResult struct {
	FilePath string
	Fixes    int // number of words replaced
	Skipped  int // typos with no suggestion (left untouched)
}

// fixKey identifies a specific typo occurrence by line and word.
type fixKey struct {
	line int
	word string
}

// runFixer applies the top suggestion for each typo across all flagged files.
// When dryRun is true, nothing is written; the function only reports what it
// would change. Files are rewritten using a temp-file + rename so a failure
// never leaves a half-written file.
//
// Returns the total number of fixed and skipped typos so callers (e.g. main)
// can decide an exit code: a real fix that leaves skipped typos behind still
// counts as "typos remain" and should fail CI.
func runFixer(results map[string][]MisspelledWord, dryRun bool) (totalFixed, totalSkipped int, err error) {
	if len(results) == 0 {
		fmt.Println("No typos to fix.")
		return 0, 0, nil
	}

	paths := make([]string, 0, len(results))
	for p := range results {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	filesChanged := 0
	for _, path := range paths {
		res, err := fixFile(path, results[path], dryRun)
		if err != nil {
			return totalFixed, totalSkipped, fmt.Errorf("fixing %s: %w", path, err)
		}
		totalFixed += res.Fixes
		totalSkipped += res.Skipped
		if res.Fixes > 0 {
			filesChanged++
		}
		reportFixFile(res, dryRun)
	}

	verb := "Fixed"
	if dryRun {
		verb = "Would fix"
	}
	fmt.Printf("\n%s %d typo(s) across %d file(s).", verb, totalFixed, filesChanged)
	if totalSkipped > 0 {
		fmt.Printf(" %d typo(s) had no suggestion and were left unchanged.", totalSkipped)
	}
	fmt.Println()
	return totalFixed, totalSkipped, nil
}

func reportFixFile(res FixResult, dryRun bool) {
	if res.Fixes == 0 && res.Skipped == 0 {
		return
	}
	prefix := "fixed"
	if dryRun {
		prefix = "would fix"
	}
	fmt.Printf("%s: %s %d, skipped %d\n", res.FilePath, prefix, res.Fixes, res.Skipped)
}

// fixFile rewrites a single file, replacing each typo with its top suggestion.
// It re-tokenizes line by line (using the same word regex as the checker) so
// replacements stay correct regardless of multibyte/rune column math.
func fixFile(path string, typos []MisspelledWord, dryRun bool) (FixResult, error) {
	res := FixResult{FilePath: path}

	repl := make(map[fixKey]string, len(typos))
	for _, t := range typos {
		if len(t.Suggestions) == 0 {
			res.Skipped++
			continue
		}
		repl[fixKey{t.LineNumber, t.Word}] = t.Suggestions[0]
	}

	in, err := os.Open(path)
	if err != nil {
		return res, err
	}
	defer in.Close()

	var out strings.Builder
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		out.WriteString(replaceLine(scanner.Text(), lineNumber, repl, &res))
		out.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return res, err
	}

	if dryRun || res.Fixes == 0 {
		return res, nil
	}
	return res, writeAtomic(path, out.String())
}

// replaceLine rebuilds a single line, substituting any token that matches a
// recorded (line, word) typo with its replacement.
func replaceLine(line string, lineNumber int, repl map[fixKey]string, res *FixResult) string {
	matches := wordRegex.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return line
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		word := line[m[0]:m[1]]
		if r, ok := repl[fixKey{lineNumber, word}]; ok {
			b.WriteString(line[last:m[0]])
			b.WriteString(r)
			last = m[1]
			res.Fixes++
		}
	}
	b.WriteString(line[last:])
	return b.String()
}

// writeAtomic writes content to a temp file in the same directory, then renames
// it over the target so readers never see a partial write. The temp file is
// fsynced before the rename so a crash doesn't leave a renamed-but-empty file.
func writeAtomic(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".spellfix-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op if rename succeeds

	if _, err := io.WriteString(tmp, content); err != nil {
		tmp.Close()
		return err
	}
	// Durability: flush the temp file's data to disk before swapping it in.
	// Without this, a crash after rename can leave the target file empty
	// because the rename is durable but the data write is not.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// Preserve original permissions where possible.
	if info, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmpName, info.Mode())
	}
	return os.Rename(tmpName, path)
}
