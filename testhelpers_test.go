package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errTestSentinel stands in for classifyFile failures in logGate tests.
var errTestSentinel = errors.New("test gate error")

// Shared test helpers. Same-package *_test.go files share one namespace, so
// these live here exactly once instead of being copy-pasted per test file.

// writeTempFile creates name under a fresh TempDir with content and returns
// its path. The canonical temp-file boilerplate used across ~30 tests.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// tinyDict builds a ConcurrentDictionary from words. Replaces the ~18 inline
// map[string]struct{} one-off dictionaries scattered across test files.
func tinyDict(words ...string) *ConcurrentDictionary {
	m := make(map[string]struct{}, len(words))
	for _, w := range words {
		m[w] = struct{}{}
	}
	return NewConcurrentDictionary(m)
}

func TestWriteTempFile(t *testing.T) {
	path := writeTempFile(t, "doc.txt", "hello wrld\n")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello wrld\n" {
		t.Errorf("content = %q", got)
	}
}

func TestTinyDict(t *testing.T) {
	cd := tinyDict("hello", "world")
	if !cd.Contains("hello") || !cd.Contains("WORLD") {
		t.Error("expected case-insensitive contains")
	}
	if cd.Contains("wrld") {
		t.Error("did not expect wrld in dict")
	}
}

func TestLogGate(t *testing.T) {
	var sb strings.Builder
	// fileOK passes through with no output.
	if !logGate(&sb, fileOK, "a.txt", nil, false) {
		t.Error("fileOK should proceed")
	}
	if sb.String() != "" {
		t.Errorf("fileOK should log nothing, got %q", sb.String())
	}
	// Skips log only when verbose.
	sb.Reset()
	if logGate(&sb, fileExcluded, "a.log", nil, false) {
		t.Error("excluded should not proceed")
	}
	if sb.String() != "" {
		t.Errorf("non-verbose skip should log nothing, got %q", sb.String())
	}
	sb.Reset()
	if logGate(&sb, fileExcluded, "a.log", nil, true) {
		t.Error("excluded should not proceed")
	}
	if !strings.Contains(sb.String(), "Skipping excluded file: a.log") {
		t.Errorf("verbose skip missing, got %q", sb.String())
	}
	sb.Reset()
	if logGate(&sb, fileBinary, "a.bin", nil, true) {
		t.Error("binary should not proceed")
	}
	if !strings.Contains(sb.String(), "Skipping binary file: a.bin") {
		t.Errorf("verbose binary skip missing, got %q", sb.String())
	}
	// Errors always log, even non-verbose (directory-walk policy).
	sb.Reset()
	if logGate(&sb, fileExcludeErr, "a.txt", errTestSentinel, false) {
		t.Error("error gate should not proceed")
	}
	if !strings.Contains(sb.String(), "Error checking exclude pattern") {
		t.Errorf("exclude error missing, got %q", sb.String())
	}
	sb.Reset()
	if logGate(&sb, fileBinaryErr, "a.txt", errTestSentinel, false) {
		t.Error("error gate should not proceed")
	}
	if !strings.Contains(sb.String(), "Error checking if file is binary") {
		t.Errorf("binary error missing, got %q", sb.String())
	}
}

func TestMisspelledWordSuggestionString(t *testing.T) {
	m := MisspelledWord{Word: "wrld", Suggestions: []string{"world", "whorl"}}
	if got := m.SuggestionString(); got != "world, whorl" {
		t.Errorf("got %q", got)
	}
	var empty MisspelledWord
	if got := empty.SuggestionString(); got != "" {
		t.Errorf("empty should be %q, got %q", "", got)
	}
}

func TestRunGitErrorWrapsOp(t *testing.T) {
	t.Chdir(t.TempDir()) // not a git repository
	if _, err := runGit(t.Context(), "git diff", "diff", "--name-only", "HEAD"); err == nil ||
		!strings.Contains(err.Error(), "git diff failed") {
		t.Errorf("expected op-labeled error, got %v", err)
	}
}
