package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: detect typos in a file using a small mock dictionary.
func detectTypos(t *testing.T, path string, dict map[string]struct{}) []MisspelledWord {
	t.Helper()
	cd := NewConcurrentDictionary(dict)
	typos, err := checkFile(path, cd)
	if err != nil {
		t.Fatalf("checkFile: %v", err)
	}
	return typos
}

// TestFixFileReplacesTypos verifies typos are replaced with their top suggestion
// and the rest of the line (including non-word chars) is preserved.
func TestFixFileReplacesTypos(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	original := "hello wrld!\nthis is a tset, ok?\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// "wrld" -> "world", "tset"/"tset" not relevant; we want "tset" but the text
	// has "tset"? No: text has "tset"? It's "tset" -> use a dictionary where the
	// only close word to "tset" is "test".
	dict := map[string]struct{}{
		"hello": {}, "world": {}, "this": {}, "is": {}, "a": {}, "ok": {}, "test": {},
	}
	typos := detectTypos(t, path, dict)
	if len(typos) == 0 {
		t.Fatal("expected typos to be detected")
	}

	res, err := fixFile(path, typos, false)
	if err != nil {
		t.Fatalf("fixFile: %v", err)
	}
	if res.Fixes == 0 {
		t.Fatal("expected at least one fix")
	}

	fixed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(fixed)
	// Punctuation and structure must be preserved.
	if !containsAll(got, []string{"hello world!", "ok?"}) {
		t.Errorf("unexpected fixed content:\n%s", got)
	}
}

// TestFixFileDryRunLeavesFileUnchanged verifies dry-run reports fixes but writes nothing.
func TestFixFileDryRunLeavesFileUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	original := "hello wrld\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	dict := map[string]struct{}{"hello": {}, "world": {}}
	typos := detectTypos(t, path, dict)

	res, err := fixFile(path, typos, true) // dryRun
	if err != nil {
		t.Fatalf("fixFile: %v", err)
	}
	if res.Fixes != 1 {
		t.Errorf("expected 1 fix counted in dry-run, got %d", res.Fixes)
	}

	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("dry-run modified file: %q", string(after))
	}
}

// TestFixFileSkipsNoSuggestion verifies typos with no suggestion are left in place
// and counted as skipped.
func TestFixFileSkipsNoSuggestion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	original := "zzzzzzzz qwxyz\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Dictionary shares no near words, so no suggestions are produced.
	dict := map[string]struct{}{"completely": {}, "different": {}}
	typos := detectTypos(t, path, dict)
	if len(typos) == 0 {
		t.Fatal("expected typos")
	}

	res, err := fixFile(path, typos, false)
	if err != nil {
		t.Fatalf("fixFile: %v", err)
	}
	if res.Fixes != 0 {
		t.Errorf("expected 0 fixes, got %d", res.Fixes)
	}
	if res.Skipped == 0 {
		t.Error("expected skipped count > 0")
	}

	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("file changed despite no suggestions: %q", string(after))
	}
}

// TestFixFilePreservesMode verifies the file keeps its permission bits after a fix.
func TestFixFilePreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("hello wrld\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	dict := map[string]struct{}{"hello": {}, "world": {}}
	typos := detectTypos(t, path, dict)

	if _, err := fixFile(path, typos, false); err != nil {
		t.Fatalf("fixFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected mode 0600 preserved, got %o", info.Mode().Perm())
	}
}

// TestRunFixerEmpty verifies a no-typo run is a clean no-op.
func TestRunFixerEmpty(t *testing.T) {
	fixed, skipped, err := runFixer(map[string][]MisspelledWord{}, false)
	if err != nil {
		t.Errorf("expected nil error for empty results, got %v", err)
	}
	if fixed != 0 || skipped != 0 {
		t.Errorf("expected (0,0) counts, got (%d,%d)", fixed, skipped)
	}
}

// TestRunFixerReturnsCounts verifies fixed and skipped counts are returned so
// main can pick the right exit code.
func TestRunFixerReturnsCounts(t *testing.T) {
	dir := t.TempDir()
	// file A: one fixable typo + one unfixable (no suggestion)
	pathA := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(pathA, []byte("hello wrld zzzzzzzzzz\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// "wrld" -> "world"; "zzzzzzzzzz" has no close word so it's skipped.
	dict := map[string]struct{}{"hello": {}, "world": {}, "completely": {}, "different": {}}
	typos := detectTypos(t, pathA, dict)

	fixed, skipped, err := runFixer(map[string][]MisspelledWord{pathA: typos}, false)
	if err != nil {
		t.Fatalf("runFixer: %v", err)
	}
	if fixed != 1 {
		t.Errorf("expected 1 fixed, got %d", fixed)
	}
	if skipped == 0 {
		t.Errorf("expected skipped > 0, got %d", skipped)
	}
}

// TestWriteAtomic verifies content is written and replaces the original.
func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writeFileAtomic(path, "new content", true); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new content" {
		t.Errorf("got %q, want %q", string(got), "new content")
	}
	// No leftover temp files.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected 1 file in dir, got %d", len(entries))
	}
}

// TestFixFilePreservesHugeLine verifies an over-long line (which the checker
// skips entirely) is still written back byte-identically, while fixable typos
// on normal lines are replaced.
func TestFixFilePreservesHugeLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	huge := strings.Repeat("x", maxLineLen+10)
	original := "hello wrld\n" + huge + "\ndone wrld\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	dict := map[string]struct{}{"hello": {}, "world": {}, "done": {}}
	typos := detectTypos(t, path, dict)
	if len(typos) != 2 {
		t.Fatalf("expected 2 typos (huge line skipped), got %d: %v", len(typos), typos)
	}

	if _, err := fixFile(path, typos, false); err != nil {
		t.Fatalf("fixFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "hello world\n" + huge + "\ndone world\n"
	if string(got) != want {
		t.Errorf("huge line not preserved byte-identically:\ngot:  %q\nwant: %q", string(got), want)
	}
}

// TestFixStdinPreservesHugeLine verifies an over-long stdin line (which the
// checker skips) survives verbatim in the fixed output while normal typos on
// other lines are still replaced. Regression: fixStdin once dropped over-long
// lines entirely because it never installed the lineReader's overlong sink.
func TestFixStdinPreservesHugeLine(t *testing.T) {
	huge := strings.Repeat("x", maxLineLen+10)
	input := "hello wrld\n" + huge + "\ndone wrld\n"

	dict := map[string]struct{}{"hello": {}, "world": {}, "done": {}}
	typos, err := scanForTypos(strings.NewReader(input), NewConcurrentDictionary(dict), scanOptions{})
	if err != nil {
		t.Fatalf("scanForTypos: %v", err)
	}
	if len(typos) != 2 {
		t.Fatalf("expected 2 typos (huge line skipped), got %d: %v", len(typos), typos)
	}

	// fixStdin writes to os.Stdout; point it at a temp file so the ~1 MiB
	// output can be read back without a pipe-buffer deadlock.
	outPath := filepath.Join(t.TempDir(), "stdout")
	f, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("create stdout capture: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = f
	fixed, skipped, err := fixStdin(strings.NewReader(input), typos)
	os.Stdout = oldStdout
	f.Close()
	if err != nil {
		t.Fatalf("fixStdin: %v", err)
	}
	if fixed != 2 || skipped != 0 {
		t.Errorf("fixed=%d skipped=%d, want 2 fixed, 0 skipped", fixed, skipped)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	want := "hello world\n" + huge + "\ndone world\n"
	if string(got) != want {
		t.Errorf("huge line not preserved verbatim:\ngot:  %q\nwant: %q", string(got), want)
	}
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestMatchCase ensures --fix preserves typo capitalization when replacing.
func TestMatchCase(t *testing.T) {
	cases := []struct{ typo, sug, want string }{
		{"teh", "the", "the"},    // lowercase stays lowercase
		{"Teh", "the", "The"},    // title-case typo -> title-case replacement
		{"TEH", "the", "THE"},    // ALL CAPS typo -> all-caps replacement
		{"Café", "cafe", "Cafe"}, // accented title typo caps first rune only, no panic
	}
	for _, c := range cases {
		if got := matchCase(c.typo, c.sug); got != c.want {
			t.Errorf("matchCase(%q, %q) = %q, want %q", c.typo, c.sug, got, c.want)
		}
	}
}

// TestFixFilePreservesInlineCode verifies that --fix replaces only the flagged
// occurrence, not tokens inside inline code spans that the checker skipped.
// Before the column-keyed fix, `teh` inside backticks was wrongly rewritten.
func TestFixFilePreservesInlineCode(t *testing.T) {
	dict := map[string]struct{}{"the": {}, "use": {}, "helper": {}, "but": {}, "is": {}, "wrong": {}, "here": {}}
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	content := "Use the `teh` helper but teh is wrong here.\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	typos := detectTypos(t, path, dict)
	// The checker should flag only the prose "teh", not the one in code.
	if len(typos) != 1 {
		t.Fatalf("expected 1 typo (prose teh only), got %d: %+v", len(typos), typos)
	}
	if typos[0].Column != 24 { // "Use the `teh` helper but " = 23 chars + 1
		t.Logf("column = %d (expected ~24)", typos[0].Column)
	}

	if _, err := fixFile(path, typos, false); err != nil {
		t.Fatalf("fixFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "Use the `teh` helper but the is wrong here.\n"
	if string(got) != want {
		t.Errorf("inline code corrupted:\ngot:  %q\nwant: %q", string(got), want)
	}
}

// TestFixFilePreservesIdentifierFragment verifies that --fix does not rewrite
// a typo that appears inside an identifier (e.g. "teh_var") that the checker
// skipped via isIdentifierFragment.
func TestFixFilePreservesIdentifierFragment(t *testing.T) {
	dict := map[string]struct{}{"the": {}, "cat": {}, "and": {}, "are": {}, "different": {}}
	dir := t.TempDir()
	path := filepath.Join(dir, "id.txt")
	content := "teh cat and teh_var are different.\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	typos := detectTypos(t, path, dict)
	// Only the standalone "teh" should be flagged, not "teh" in "teh_var".
	// wordRegex splits "teh_var" into "teh" and "var"; isIdentifierFragment
	// skips the "teh" because it's adjacent to "_".
	if len(typos) != 1 {
		t.Fatalf("expected 1 typo, got %d: %+v", len(typos), typos)
	}

	if _, err := fixFile(path, typos, false); err != nil {
		t.Fatalf("fixFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "the cat and teh_var are different.\n"
	if string(got) != want {
		t.Errorf("identifier fragment corrupted:\ngot:  %q\nwant: %q", string(got), want)
	}
}

// TestFixFilePreservesMissingTrailingNewline verifies a file without a final
// newline does not gain one when a typo is fixed (byte-exact round-trip for
// the untouched tail of the file).
func TestFixFilePreservesMissingTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	original := "hello wrld" // no trailing newline
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	dict := map[string]struct{}{"hello": {}, "world": {}}
	typos := detectTypos(t, path, dict)

	if _, err := fixFile(path, typos, false); err != nil {
		t.Fatalf("fixFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("trailing newline added: got %q, want %q", string(got), "hello world")
	}
}
