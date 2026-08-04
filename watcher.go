package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debouncePeriod = 200 * time.Millisecond

func runWatcher(rootPath string, dictionary map[string]struct{}, excludePatterns []string) error {
	// Build the dictionary (and BK-tree) once, shared by the initial scan and
	// the live re-checks.
	concurrentDict := NewConcurrentDictionary(dictionary)

	// Initial scan
	fmt.Println("Performing initial scan...")
	allTypos, err := runConcurrentCheckerWithDict(rootPath, concurrentDict, excludePatterns, false)
	if err != nil {
		return fmt.Errorf("initial scan failed: %w", err)
	}

	if len(allTypos) == 0 {
		fmt.Println("  No typos found.")
	} else {
		generateTextReport(os.Stdout, allTypos)
		fmt.Printf("\n%d file(s) have typos.\n", len(allTypos))
	}

	// Set up fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer watcher.Close()

	if err := addDirsToWatcher(watcher, rootPath, excludePatterns); err != nil {
		return fmt.Errorf("failed to watch directories: %w", err)
	}

	fmt.Println("\nWatching for changes... (Ctrl+C to stop)")

	eventCh := make(chan string, 100)
	go debounceAndProcess(eventCh, concurrentDict)

	// Main event loop
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			handleWatchEvent(event, watcher, eventCh, excludePatterns)

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "Watch error: %v\n", err)
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

	// Respect exclude patterns
	excluded, err := shouldExclude(event.Name, patterns)
	if err != nil || excluded {
		return
	}

	// Skip binary files
	isBinary, err := isLikelyBinary(event.Name)
	if err != nil || isBinary {
		return
	}

	eventCh <- event.Name
}

func debounceAndProcess(eventCh <-chan string, dict *ConcurrentDictionary) {
	// Collect rapid events into a unique set, then process once the stream
	// goes quiet for debouncePeriod.
	pending := make(map[string]struct{})
	var timer *time.Timer

	for {
		if timer == nil {
			path := <-eventCh
			pending[path] = struct{}{}
			timer = time.NewTimer(debouncePeriod)
			continue
		}

		select {
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
			processBatch(pending, dict)
			pending = make(map[string]struct{})
			timer = nil
		}
	}
}

func processBatch(files map[string]struct{}, dict *ConcurrentDictionary) {
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
			fmt.Printf("[%s] %s - no typos\n", timestamp, path)
			continue
		}
		fmt.Printf("[%s] %s\n", timestamp, path)
		for _, m := range typos {
			fmt.Printf("  - %s\n", formatTypoLine(m, m.Word, strings.Join(m.Suggestions, ", ")))
		}
	}
}
