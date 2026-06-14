package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// captureStdout redirects os.Stdout while fn runs and returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	w.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return string(buf)
}

// TestProcessBatchReportsTypos verifies a file with a typo prints the word and
// a suggestion, while a clean file prints "no typos".
func TestProcessBatchReportsTypos(t *testing.T) {
	dir := t.TempDir()
	typoFile := filepath.Join(dir, "typo.txt")
	cleanFile := filepath.Join(dir, "clean.txt")
	if err := os.WriteFile(typoFile, []byte("hello wrld\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(cleanFile, []byte("hello world\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cd := NewConcurrentDictionary(map[string]struct{}{"hello": {}, "world": {}})

	out := captureStdout(t, func() {
		processBatch(map[string]struct{}{typoFile: {}}, cd)
	})
	if !strings.Contains(out, "wrld") {
		t.Errorf("expected typo word in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Did you mean") || !strings.Contains(out, "world") {
		t.Errorf("expected suggestion in output, got:\n%s", out)
	}

	out = captureStdout(t, func() {
		processBatch(map[string]struct{}{cleanFile: {}}, cd)
	})
	if !strings.Contains(out, "no typos") {
		t.Errorf("expected 'no typos' for clean file, got:\n%s", out)
	}
}

// TestProcessBatchTypoNoSuggestion verifies a typo with no near dictionary word
// is reported without a "Did you mean" prompt.
func TestProcessBatchTypoNoSuggestion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("zzzzzzzz\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cd := NewConcurrentDictionary(map[string]struct{}{"completely": {}, "different": {}})

	out := captureStdout(t, func() {
		processBatch(map[string]struct{}{path: {}}, cd)
	})
	if !strings.Contains(out, "appears to be a typo") {
		t.Errorf("expected typo report, got:\n%s", out)
	}
	if strings.Contains(out, "Did you mean") {
		t.Errorf("did not expect a suggestion prompt, got:\n%s", out)
	}
}

// TestProcessBatchEmptySet verifies processing an empty file set produces no output.
func TestProcessBatchEmptySet(t *testing.T) {
	cd := NewConcurrentDictionary(map[string]struct{}{"hello": {}})
	out := captureStdout(t, func() {
		processBatch(map[string]struct{}{}, cd)
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected no output for empty batch, got:\n%s", out)
	}
}

// TestAddDirsToWatcherSkipsExcluded verifies that excluded directories are not
// added to the watcher while the rest of the tree is.
func TestAddDirsToWatcherSkipsExcluded(t *testing.T) {
	dir := t.TempDir()
	mustMkdir := func(rel string) {
		if err := os.MkdirAll(filepath.Join(dir, rel), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}
	mustMkdir("src")
	mustMkdir("vendor/dep")
	mustMkdir("src/nested")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	defer watcher.Close()

	if err := addDirsToWatcher(watcher, dir, []string{"vendor"}); err != nil {
		t.Fatalf("addDirsToWatcher: %v", err)
	}

	watched := watcher.WatchList()
	for _, p := range watched {
		if strings.Contains(p, string(filepath.Separator)+"vendor") || strings.HasSuffix(p, "vendor") {
			t.Errorf("excluded 'vendor' directory should not be watched: %s", p)
		}
	}

	// The root and non-excluded subdirs should be present.
	wantWatched := []string{dir, filepath.Join(dir, "src"), filepath.Join(dir, "src", "nested")}
	for _, want := range wantWatched {
		found := false
		for _, p := range watched {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s to be watched, watch list: %v", want, watched)
		}
	}
}

// TestAddDirsToWatcherMissingRoot verifies a nonexistent root yields an error.
func TestAddDirsToWatcherMissingRoot(t *testing.T) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	defer watcher.Close()

	if err := addDirsToWatcher(watcher, filepath.Join(t.TempDir(), "nope"), nil); err == nil {
		t.Fatal("expected error for missing root path, got nil")
	}
}
