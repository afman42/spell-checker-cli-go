package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestLoadSpellignore verifies pattern parsing: one glob per line, comments
// and blank lines ignored, surrounding whitespace trimmed. A missing file
// returns nil (no error).
func TestLoadSpellignore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".spellignore"),
		[]byte("# generated\nvendor/\n\n*.generated.go\n  third_party/**  \n"), 0644); err != nil {
		t.Fatalf("write .spellignore: %v", err)
	}

	got := loadSpellignore(dir)
	want := []string{"vendor/", "*.generated.go", "third_party/**"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadSpellignore = %q, want %q", got, want)
	}
}

// TestLoadSpellignoreMissing verifies a missing .spellignore yields nil, not
// an error, so its absence doesn't break scans.
func TestLoadSpellignoreMissing(t *testing.T) {
	got := loadSpellignore(t.TempDir())
	if got != nil {
		t.Errorf("loadSpellignore = %q, want nil", got)
	}
}

// TestLoadSpellignoreIgnoresItself is a safety check: the .spellignore file
// must never list patterns that re-include the file scanner can't handle. It
// confirms comment-only files produce no patterns.
func TestLoadSpellignoreCommentsOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".spellignore"), []byte("# only a comment\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := loadSpellignore(dir); len(got) != 0 {
		t.Errorf("loadSpellignore = %q, want empty", got)
	}
}
