package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FixResult summarizes what was changed (or would be changed) in one file.
type FixResult struct {
	FilePath string
	Fixes    int // number of words replaced
	Skipped  int // typos with no suggestion (left untouched)
}

// fixKey identifies a specific typo occurrence by line and column (1-based
// rune column, matching MisspelledWord.Column). Keying by column — not just
// word — ensures --fix replaces only the flagged occurrence, not every token
// on the line that happens to spell the same (e.g. inside inline code or an
// identifier fragment the checker deliberately skipped).
type fixKey struct {
	line   int
	column int // 1-based rune column, same as MisspelledWord.Column
}

// runFixer applies the top suggestion for each typo across all flagged files.
// When dryRun is true, nothing is written; the function only reports what it
// would change. Files are rewritten using a temp-file + rename so a failure
// never leaves a half-written file.
//
// Returns the total number of fixed and skipped typos so callers (e.g. main)
// can decide an exit code: a real fix that leaves skipped typos behind still
// counts as "typos remain" and should fail CI.
func runFixer(results CheckResults, dryRun bool) (totalFixed, totalSkipped int, err error) {
	if len(results) == 0 {
		fmt.Println("No typos to fix.")
		return 0, 0, nil
	}

	paths := sortedResultPaths(results)

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

// buildFixRepl maps each typo's (line, column) to its top suggestion, counting
// uncorrectable typos (no suggestion) as skipped. Shared by fixFile and
// fixStdin so the fixer never diverges between file and stdin modes.
func buildFixRepl(typos []MisspelledWord, res *FixResult) map[fixKey]string {
	repl := make(map[fixKey]string, len(typos))
	for _, t := range typos {
		if len(t.Suggestions) == 0 {
			res.Skipped++
			continue
		}
		repl[fixKey{t.LineNumber, t.Column}] = t.Suggestions[0]
	}
	return repl
}

// fixFile rewrites a single file, replacing each typo with its top suggestion.
// It re-tokenizes line by line (using the same word regex as the checker) so
// replacements stay correct regardless of multibyte/rune column math.
func fixFile(path string, typos []MisspelledWord, dryRun bool) (FixResult, error) {
	res := FixResult{FilePath: path}
	repl := buildFixRepl(typos, &res)

	in, err := os.Open(path)
	if err != nil {
		return res, err
	}
	defer in.Close()

	var out strings.Builder
	// Iterate with the same lineReader and maxLineLen as scanForTypos so the
	// fixer re-tokenizes exactly the lines the checker flagged, keeping line
	// numbers aligned. Over-long lines (which the checker skips) are streamed
	// into out verbatim so the rebuilt file is byte-identical.
	lr := newLineReader(in, maxLineLen)
	lr.setOverlong(&out)
	if err := rewriteLines(lr, &out, repl, &res); err != nil {
		return res, err
	}

	if dryRun || res.Fixes == 0 {
		return res, nil
	}
	return res, writeAtomic(path, out.String())
}

// fixStdin applies the top suggestion for each typo in piped stdin and writes
// the corrected stream to stdout. Same re-tokenization and case-preserving
// replacement as fixFile, but in-memory (no file path). The corrected text is
// always printed (a preview); nothing is persisted anywhere.
func fixStdin(r io.Reader, typos []MisspelledWord) (fixed, skipped int, err error) {
	res := FixResult{FilePath: "<stdin>"}
	repl := buildFixRepl(typos, &res)
	var out strings.Builder
	lr := newLineReader(r, maxLineLen)
	lr.setOverlong(&out)
	if err := rewriteLines(lr, &out, repl, &res); err != nil {
		return 0, 0, err
	}
	if _, err := io.WriteString(os.Stdout, out.String()); err != nil {
		return 0, 0, err
	}
	return res.Fixes, res.Skipped, nil
}

// rewriteLines streams every line of lr through replaceLine into out. Shared
// by fixFile and fixStdin so the rebuild rules cannot drift: lines are
// re-tokenized with the same regex, over-long lines arrive via lr's overlong
// sink, and trailing newlines are preserved only where the source had them.
func rewriteLines(lr *lineReader, out *strings.Builder, repl map[fixKey]string, res *FixResult) error {
	for {
		line, lineNumber, err := lr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// lineReader returns the newline for complete lines; strip it so the
		// rebuilt file keeps single terminators. Only re-add it when the
		// source line actually had one — a file with no trailing newline
		// must not gain one.
		hadNL := strings.HasSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\n")
		out.WriteString(replaceLine(line, lineNumber, repl, res))
		if hadNL {
			out.WriteByte('\n')
		}
	}
}

func replaceLine(line string, lineNumber int, repl map[fixKey]string, res *FixResult) string {
	matches := wordRegex.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return line
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		// Compute 1-based rune column from the byte offset, matching
		// MisspelledWord.Column so only the exact flagged token is replaced.
		col := utf8.RuneCountInString(line[:m[0]]) + 1
		word := line[m[0]:m[1]]
		if r, ok := repl[fixKey{lineNumber, col}]; ok {
			b.WriteString(line[last:m[0]])
			b.WriteString(matchCase(word, r))
			last = m[1]
			res.Fixes++
		}
	}
	b.WriteString(line[last:])
	return b.String()
}

// matchCase applies the typo's capitalization to the replacement so --fix
// keeps "Teh" -> "The" and "TEH" -> "THE" instead of dropping to lowercase.
// Lowercase typo gets the lowercase suggestion untouched.
func matchCase(typo, suggestion string) string {
	if typo == strings.ToUpper(typo) {
		// ALL CAPS typo -> all-caps suggestion.
		return strings.ToUpper(suggestion)
	}
	// Title-case typo (first rune uppercase) -> uppercase the suggestion's
	// first rune, keep the rest. Use utf8 to stay safe on accented letters.
	if r, _ := utf8.DecodeRuneInString(typo); r != utf8.RuneError && unicode.IsUpper(r) {
		rs := []rune(suggestion)
		if len(rs) > 0 {
			rs[0] = unicode.ToUpper(rs[0])
			return string(rs)
		}
	}
	return suggestion
}

// writeFileAtomic writes content to a temp file in the target's directory,
// fsyncs it, closes it, then renames it over path so readers never see a
// partial write; the temp file is removed if any step fails. When
// preserveMode is true the existing target's permission bits are copied onto
// the replacement — a stat failure there propagates instead of silently
// skipping the chmod. After a successful rename the parent directory is
// fsynced best-effort so the rename itself is durable.
func writeFileAtomic(path, content string, preserveMode bool) error {
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

	if preserveMode {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.Chmod(tmpName, info.Mode()); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		dirFile.Close()
	}
	return nil
}

// writeAtomic writes content to a temp file in the same directory, then renames
// it over the target so readers never see a partial write. The temp file is
// fsynced before the rename so a crash doesn't leave a renamed-but-empty file,
// and the original file's permissions are preserved.
func writeAtomic(path, content string) error {
	return writeFileAtomic(path, content, true)
}
