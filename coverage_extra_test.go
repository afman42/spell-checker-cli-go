package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// captureStderr redirects os.Stderr while fn runs and returns what was written.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

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

// sampleResults builds a small CheckResults shared by the report tests.
func sampleResults() CheckResults {
	return CheckResults{
		"doc.txt": {
			{Word: "wrld", LineNumber: 1, Column: 7, Suggestions: []string{"world"}},
		},
	}
}

func TestLoadYAMLConfig(t *testing.T) {
	dir := t.TempDir()

	valid := filepath.Join(dir, "ok.yaml")
	if err := os.WriteFile(valid, []byte("exclude:\n  - \"*.log\"\nverbose: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadYAMLConfig(valid)
	if err != nil {
		t.Fatalf("valid config: %v", err)
	}
	if !cfg.Verbose || len(cfg.Exclude) != 1 || cfg.Exclude[0] != "*.log" {
		t.Errorf("unexpected parsed config: %+v", cfg)
	}

	malformed := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(malformed, []byte("exclude: [unclosed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadYAMLConfig(malformed); err == nil || !strings.Contains(err.Error(), "error parsing config file") {
		t.Errorf("expected parse error, got %v", err)
	}

	if _, err := loadYAMLConfig(filepath.Join(dir, "missing.yaml")); err == nil || !strings.Contains(err.Error(), "error reading config file") {
		t.Errorf("expected read error, got %v", err)
	}
}

func TestWriteReportFormats(t *testing.T) {
	results := sampleResults()
	cases := []struct {
		format OutputFormat
		want   string
	}{
		{FormatHTML, "<html"},
		{FormatJSON, `"summary"`},
		{FormatSarif, `"version"`},
		{"txt", "wrld"}, // default branch falls back to text
		{"bogus", "wrld"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		if err := writeReport(&buf, results, tc.format); err != nil {
			t.Fatalf("%s: %v", tc.format, err)
		}
		if !strings.Contains(buf.String(), tc.want) {
			t.Errorf("format %q: output missing %q:\n%s", tc.format, tc.want, buf.String())
		}
	}
}

func TestStoreBKTreeCacheRoundTrip(t *testing.T) {
	dict := map[string]struct{}{"hello": {}, "help": {}, "world": {}}
	tree := NewBKTree(dict)
	for w := range dict {
		tree.Add(w)
	}

	t.Cleanup(func() { _ = os.Remove(treeCachePath(dict)) })
	storeBKTreeCache(dict, tree)

	loaded := loadBKTreeCache(dict)
	if loaded == nil {
		t.Fatal("expected cached tree to load")
	}
	got := loaded.Search("helo", 1)
	found := false
	for _, sw := range got {
		if sw.word == "hello" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Search(helo) = %v, want hello among matches", got)
	}
}

func TestHandleWatchEventRouting(t *testing.T) {
	dir := t.TempDir()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	ch := make(chan string, 8)
	patterns := []string{"*.log"}

	// Normal text file: routed to the event channel.
	txt := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(txt, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	handleWatchEvent(fsnotify.Event{Name: txt, Op: fsnotify.Write}, watcher, ch, patterns)
	select {
	case got := <-ch:
		if got != txt {
			t.Errorf("got %q, want %q", got, txt)
		}
	default:
		t.Error("expected normal file event to be queued")
	}

	// Excluded file: dropped.
	logFile := filepath.Join(dir, "b.log")
	if err := os.WriteFile(logFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	handleWatchEvent(fsnotify.Event{Name: logFile, Op: fsnotify.Write}, watcher, ch, patterns)
	select {
	case got := <-ch:
		t.Errorf("excluded file was queued: %q", got)
	default:
	}

	// Vanished file (removed away): stale watch dropped, nothing queued.
	handleWatchEvent(fsnotify.Event{Name: filepath.Join(dir, "gone.txt"), Op: fsnotify.Remove}, watcher, ch, patterns)
	select {
	case got := <-ch:
		t.Errorf("vanished file was queued: %q", got)
	default:
	}

	// Chmod-only op: ignored.
	handleWatchEvent(fsnotify.Event{Name: txt, Op: fsnotify.Chmod}, watcher, ch, patterns)
	select {
	case got := <-ch:
		t.Errorf("chmod-only event was queued: %q", got)
	default:
	}

	// New subdirectory: added to the watch list without queueing a check.
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	stderr := captureStderr(t, func() {
		handleWatchEvent(fsnotify.Event{Name: sub, Op: fsnotify.Create}, watcher, ch, patterns)
	})
	if strings.Contains(stderr, "Error watching new directory") {
		t.Errorf("unexpected watch error: %s", stderr)
	}
}

func TestDebounceAndProcessFlushesBatch(t *testing.T) {
	dir := t.TempDir()
	typoFile := filepath.Join(dir, "typo.txt")
	if err := os.WriteFile(typoFile, []byte("hello wrld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cd := NewConcurrentDictionary(map[string]struct{}{"hello": {}, "world": {}})

	// debounceAndProcess writes through the injected writer, so the test can
	// capture batch output without swapping the os.Stdout global (which would
	// race the goroutine's fmt writes). Poll the thread-safe buffer until the
	// debounce timer fires and the batch flushes.
	sb := &syncBuf{}
	ch := make(chan string, 8)
	go debounceAndProcess(ch, cd, sb)

	ch <- typoFile
	ch <- typoFile // duplicate coalesced by the pending set

	deadline := time.Now().Add(3 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		got = sb.String()
		if strings.Contains(got, "no typos") || strings.Contains(got, "wrld") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// ponytail: debounceAndProcess goroutine leaks blocking on <-eventCh after
	// flush; fine for test process lifetime.
	if !strings.Contains(got, "no typos") && !strings.Contains(got, "wrld") {
		t.Errorf("expected batch result for %s, got:\n%s", typoFile, got)
	}
}

// syncBuf is a bytes.Buffer safe for one writer (the debounce goroutine) and
// one reader (the test), avoiding a data race on the captured output.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRenderProgressBarStopsOnDone(t *testing.T) {
	var processed, errored atomic.Int64
	processed.Store(2)
	done := make(chan struct{})
	close(done)

	stderr := captureStderr(t, func() {
		renderProgressBar(4, &processed, &errored, done)
	})
	// With the done channel already closed, the select always picks the done
	// case immediately, which renders the completed state (100% total/total).
	if !strings.Contains(stderr, "100%") || !strings.Contains(stderr, "4/4 files") {
		t.Errorf("unexpected progress output: %q", stderr)
	}
}

func TestPrintProgressBarErroredSuffix(t *testing.T) {
	stderr := captureStderr(t, func() {
		printProgressBar(10, 10, 3)
	})
	if !strings.Contains(stderr, "(3 errored)") {
		t.Errorf("expected errored suffix, got %q", stderr)
	}
}

// initGitRepo creates a committed fixture repo and returns its path.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write("clean.txt", "hello world\n")
	write(filepath.Join("sub", "clean2.txt"), "hello world\n")
	write("dirty.txt", "hello world\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "init")

	// Working-tree change after HEAD: only dirty.txt shows in diff vs HEAD.
	write("dirty.txt", "hello wrld\n")
	return dir
}

func TestGitDiffFilesErrors(t *testing.T) {
	if _, err := gitDiffFiles(""); err == nil || !strings.Contains(err.Error(), "empty ref") {
		t.Errorf("empty ref: expected error, got %v", err)
	}

	t.Chdir(t.TempDir()) // not a git repository
	if _, err := gitDiffFiles("HEAD"); err == nil || !strings.Contains(err.Error(), "git diff failed") {
		t.Errorf("non-repo dir: expected error, got %v", err)
	}
}

func TestRunGitDiffCheckerScansChangedFiles(t *testing.T) {
	repo := initGitRepo(t)
	cd := NewConcurrentDictionary(map[string]struct{}{"hello": {}, "world": {}})

	// gitDiffFiles shells out to git in the process working directory, so the
	// fixture repo must be the cwd and root paths are relative to it.
	t.Chdir(repo)

	results, err := runGitDiffChecker("HEAD", ".", cd, nil, true)
	if err != nil {
		t.Fatalf("runGitDiffChecker: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected only dirty.txt, got %v", results)
	}
	if _, ok := results["dirty.txt"]; !ok {
		t.Errorf("expected dirty.txt in results, got %v", results)
	}

	// Root-path filtering: restricting to sub/ hides dirty.txt entirely.
	results, err = runGitDiffChecker("HEAD", "sub", cd, nil, false)
	if err != nil {
		t.Fatalf("runGitDiffChecker(sub): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results under sub/, got %v", results)
	}
}

func TestReportFixFileVariants(t *testing.T) {
	res := FixResult{FilePath: "f.txt", Fixes: 2, Skipped: 1}

	out := captureStdout(t, func() { reportFixFile(res, false) })
	if out != "f.txt: fixed 2, skipped 1\n" {
		t.Errorf("fix mode: got %q", out)
	}

	out = captureStdout(t, func() { reportFixFile(res, true) })
	if out != "f.txt: would fix 2, skipped 1\n" {
		t.Errorf("dry-run mode: got %q", out)
	}

	silent := FixResult{FilePath: "clean.txt"}
	out = captureStdout(t, func() { reportFixFile(silent, false) })
	if out != "" {
		t.Errorf("zero-result should print nothing, got %q", out)
	}
}

func TestWriteFileAtomicPreserveModeAndErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.txt")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := writeFileAtomic(path, "new", true); err != nil {
		t.Fatalf("preserveMode write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Fatalf("content = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("mode = %v, want -rw------- preserved", info.Mode())
	}

	// Nonexistent parent directory must surface an error, not vanish.
	broken := filepath.Join(dir, "nope", "out.txt")
	if err := writeFileAtomic(broken, "x", false); err == nil {
		t.Error("expected error writing into missing directory")
	}
}

func TestRunRejectsBadFormat(t *testing.T) {
	var code int
	stderr := captureStderr(t, func() { code = run([]string{"--format", "xml", "."}) })
	if code != exitError {
		t.Errorf("bad-format exit = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "invalid format") {
		t.Errorf("expected validation error on stderr, got %q", stderr)
	}
}
