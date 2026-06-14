package main

import (
	"os"
	"path/filepath"
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
	if err := runFixer(map[string][]MisspelledWord{}, false); err != nil {
		t.Errorf("expected nil error for empty results, got %v", err)
	}
}

// TestWriteAtomic verifies content is written and replaces the original.
func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writeAtomic(path, "new content"); err != nil {
		t.Fatalf("writeAtomic: %v", err)
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
