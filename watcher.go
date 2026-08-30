package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debouncePeriod = 200 * time.Millisecond

func runWatcherWithContext(ctx context.Context, rootPath string, dictionary map[string]struct{}, excludePatterns []string, outW, errW io.Writer) error {
	concurrentDict := NewConcurrentDictionary(dictionary)
	fmt.Fprintln(outW, "Performing initial scan...")
	allTypos, err := runConcurrentCheckerWithDictAndContext(ctx, rootPath, concurrentDict, excludePatterns, false, scanOptions{})
	if err != nil {
		return fmt.Errorf("initial scan failed: %w", err)
	}
	if len(allTypos) == 0 {
		fmt.Fprintln(outW, "  No typos found.")
	} else {
		if err := generateTextReport(outW, allTypos); err != nil {
			fmt.Fprintf(errW, "Error generating report: %v\n", err)
		}
		fmt.Fprintf(outW, "\n%d file(s) have typos.\n", len(allTypos))
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer watcher.Close()
	if err := addDirsToWatcher(watcher, rootPath, excludePatterns); err != nil {
		return fmt.Errorf("failed to watch directories: %w", err)
	}
	fmt.Fprintln(outW, "\nWatching for changes... (Ctrl+C to stop)")
	eventCh := make(chan string, 100)
	go debounceAndProcessWithContext(ctx, eventCh, concurrentDict, outW, errW)
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			handleWatchEvent(event, watcher, eventCh, excludePatterns)
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(errW, "Watch error: %v\n", err)
		}
	}
}

func addDirsToWatcher(watcher *fsnotify.Watcher, rootPath string, excludePatterns []string) error {
	patterns := mergeDefaultExcludes(excludePatterns)
	return filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		excluded, err := shouldExclude(path, patterns)
		if err != nil {
			return err
		}
		if excluded {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}
func handleWatchEvent(event fsnotify.Event, watcher *fsnotify.Watcher, eventCh chan<- string, excludePatterns []string) {
	// Editors save in many ways: direct writes, or write-temp-then-rename
	// (atomic save). Cover Write, Create, Rename, and Remove so saves and
	// deletions are handled and stale watches don't leak.
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
		return
	}

	info, err := os.Stat(event.Name)
	if err != nil {
		// Removed or renamed away; drop any stale watch on it, then nothing
		// left to check.
		_ = watcher.Remove(event.Name)
		return
	}

	patterns := mergeDefaultExcludes(excludePatterns)

	// A new directory: watch it (and any subtree it already contains).
	if info.IsDir() {
		excluded, err := shouldExclude(event.Name, patterns)
		if err == nil && !excluded {
			if err := addDirsToWatcher(watcher, event.Name, patterns); err != nil {
				fmt.Fprintf(os.Stderr, "Error watching new directory %s: %v\n", event.Name, err)
			}
		}
		return
	}

	if gate, _ := classifyFile(event.Name, patterns); gate != fileOK {
		return
	}

	// Non-blocking: if the queue is full (the debounce loop is busy with a
	// large batch), drop the event rather than stall the fsnotify loop — a
	// blocked main loop can overflow fsnotify's internal buffer and lose the
	// whole event stream. Editors emit multiple events per save, so the next
	// one re-triggers the re-check.
	select {
	case eventCh <- event.Name:
	default:
		fmt.Fprintf(os.Stderr, "Watch: event queue full, dropping %s\n", event.Name)
	}
}

func debounceAndProcess(eventCh <-chan string, dict *ConcurrentDictionary, out io.Writer) {
	debounceAndProcessWithContext(context.Background(), eventCh, dict, out, os.Stderr)
}

func debounceAndProcessWithContext(ctx context.Context, eventCh <-chan string, dict *ConcurrentDictionary, out io.Writer, errW io.Writer) {
	pending := make(map[string]struct{})
	var timer *time.Timer
	for {
		if timer == nil {
			select {
			case <-ctx.Done():
				return
			case path := <-eventCh:
				pending[path] = struct{}{}
				timer = time.NewTimer(debouncePeriod)
				continue
			}
		}
		select {
		case <-ctx.Done():
			if len(pending) > 0 {
				processBatch(pending, dict, out)
			}
			if timer != nil {
				timer.Stop()
			}
			return
		case path := <-eventCh:
			pending[path] = struct{}{}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer = time.NewTimer(debouncePeriod)
		case <-timer.C:
			processBatch(pending, dict, out)
			pending = make(map[string]struct{})
			timer = nil
		}
	}
}

func processBatch(files map[string]struct{}, dict *ConcurrentDictionary, out io.Writer) {
	timestamp := time.Now().Format("15:04:05")
	// Sort paths for deterministic batch output.
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		typos, err := checkFile(path, dict)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] Error checking %s: %v\n", timestamp, path, err)
			continue
		}
		if len(typos) == 0 {
			fmt.Fprintf(out, "[%s] %s - no typos\n", timestamp, path)
			continue
		}
		fmt.Fprintf(out, "[%s] %s\n", timestamp, path)
		for _, m := range typos {
			fmt.Fprintf(out, "  - %s\n", formatTypoLine(m, m.Word, strings.Join(m.Suggestions, ", ")))
		}
	}
}
