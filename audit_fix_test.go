package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRunFixerContinuesPastBadFile verifies errors.Join semantics: one
// unreadable file no longer aborts the rest; counts still accumulate and the
// error names the bad path.
func TestRunFixerContinuesPastBadFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	if err := os.WriteFile(good, []byte("hello wrld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "missing.txt")
	dict := map[string]struct{}{"hello": {}, "world": {}}
	typos := detectTypos(t, good, dict)

	fixed, _, err := runFixer(map[string][]MisspelledWord{bad: typos, good: typos}, false)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "missing.txt") {
		t.Errorf("error should name the bad file, got %v", err)
	}
	if fixed != 1 {
		t.Errorf("good file should still be fixed, got %d", fixed)
	}
}

// TestRunFixerJoinsMultipleErrors verifies two bad files both surface in one
// errors.Join error.
func TestRunFixerJoinsMultipleErrors(t *testing.T) {
	typos := []MisspelledWord{{Word: "wrld", LineNumber: 1, Column: 7, Suggestions: []string{"world"}}}
	_, _, err := runFixer(map[string][]MisspelledWord{
		"nope-a.txt": typos,
		"nope-b.txt": typos,
	}, false)
	if err == nil {
		t.Fatal("expected joined error, got nil")
	}
	if !strings.Contains(err.Error(), "nope-a.txt") || !strings.Contains(err.Error(), "nope-b.txt") {
		t.Errorf("joined error should name both files, got %v", err)
	}
}

// TestValidationSentinels verifies errors.Is matching on config validation.
func TestValidationSentinels(t *testing.T) {
	if err := (&Config{DryRun: true}).Validate(); !errors.Is(err, ErrDryRunWithoutFix) {
		t.Errorf("want ErrDryRunWithoutFix, got %v", err)
	}
	if err := (&Config{OnlyChangedLines: true}).Validate(); !errors.Is(err, ErrOnlyChangedLinesWithoutDiff) {
		t.Errorf("want ErrOnlyChangedLinesWithoutDiff, got %v", err)
	}
	if err := validateGitRef(""); !errors.Is(err, ErrEmptyGitRef) {
		t.Errorf("want ErrEmptyGitRef, got %v", err)
	}
	if err := validateGitRef("-x"); !errors.Is(err, ErrInvalidGitRef) {
		t.Errorf("want ErrInvalidGitRef, got %v", err)
	}
}

// TestWarmupBuildsTree verifies Warmup pays the cold-start build once so the
// worker pool never races it.
func TestWarmupBuildsTree(t *testing.T) {
	dict := make(map[string]struct{}, 151)
	for i := range 150 {
		dict["filler"+strconv.Itoa(i)] = struct{}{}
	}
	dict["world"] = struct{}{}
	cd := NewConcurrentDictionary(dict)
	cd.Warmup()
	if cd.bkTree == nil {
		t.Fatal("expected BK-tree after Warmup")
	}
}

// TestDebounceExitsOnClosedChannel verifies the eventCh close fix: closing the
// channel flushes pending and returns instead of stranding the goroutine.
func TestDebounceExitsOnClosedChannel(t *testing.T) {
	dir := t.TempDir()
	typoFile := filepath.Join(dir, "typo.txt")
	if err := os.WriteFile(typoFile, []byte("hello wrld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cd := NewConcurrentDictionary(map[string]struct{}{"hello": {}, "world": {}})
	sb := &syncBuf{}
	ch := make(chan string, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		debounceAndProcess(ch, cd, sb)
	}()
	ch <- typoFile
	close(ch)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("debounce goroutine stranded on closed eventCh")
	}
	if got := sb.String(); !strings.Contains(got, "wrld") {
		t.Errorf("expected flushed batch for %s, got:\n%s", typoFile, got)
	}
}

// TestHelpFlagExitsZero verifies -h/--help prints usage plus the full pflag
// list on stdout and exits 0 instead of the fatal path (exit 2).
func TestHelpFlagExitsZero(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		var stdout, stderr strings.Builder
		code := runWithContext(t.Context(), []string{flag}, &stdout, &stderr)
		if code != exitOK {
			t.Errorf("%s: got exit %d, want %d", flag, code, exitOK)
		}
		out := stdout.String()
		for _, want := range []string{"Usage:", "--fix", "--git-diff", "--watch"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: expected %q in help output, got %q", flag, want, out)
			}
		}
		if strings.Contains(stderr.String(), "Fatal error") {
			t.Errorf("%s: unexpected fatal error on stderr: %q", flag, stderr.String())
		}
	}
}

// TestUnknownFlagExitsError verifies a genuinely bad flag still exits 2.
func TestUnknownFlagExitsError(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runWithContext(t.Context(), []string{"--bogus-flag"}, &stdout, &stderr)
	if code != exitError {
		t.Errorf("got exit %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "Fatal error") {
		t.Errorf("expected fatal error on stderr, got %q", stderr.String())
	}
}
