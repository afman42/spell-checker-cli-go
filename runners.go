package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// runConcurrentCheckerWithDict is the core scanner using an already-built
// ConcurrentDictionary, so the BK-tree is constructed only once per run.
func runConcurrentCheckerWithDict(rootPath string, concurrentDict *ConcurrentDictionary, excludePatterns []string, verbose bool) (CheckResults, error) {
	return runConcurrentCheckerWithDictAndContext(context.Background(), rootPath, concurrentDict, excludePatterns, verbose, scanOptions{})
}

func runConcurrentCheckerWithDictAndContext(ctx context.Context, rootPath string, concurrentDict *ConcurrentDictionary, excludePatterns []string, verbose bool, opts scanOptions) (CheckResults, error) {
	allFiles, err := collectFilesWithContext(ctx, rootPath, excludePatterns, verbose)
	if err != nil {
		return nil, err
	}
	return runCheckerOnFilesWithContext(ctx, allFiles, concurrentDict, verbose, opts)
}

func runCheckerOnFilesWithContext(ctx context.Context, files []string, concurrentDict *ConcurrentDictionary, verbose bool, opts scanOptions) (CheckResults, error) {
	if len(files) == 0 {
		return make(CheckResults), nil
	}

	totalFiles := len(files)
	numWorkers := runtime.NumCPU()
	jobBuf := numWorkers * 10
	if jobBuf < 100 {
		jobBuf = 100
	}
	jobs := make(chan string, jobBuf)
	results := make(chan CheckResult, jobBuf)

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go workerWithContext(ctx, &wg, jobs, results, concurrentDict, opts, nil)
	}

	go func() {
		defer close(jobs)
		for _, path := range files {
			select {
			case <-ctx.Done():
				return
			case jobs <- path:
			}
		}
	}()

	// Sink goroutine: closes results when all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Progress bar (stderr, terminal only)
	showProgress := totalFiles > 1 && isStderrTerminal()
	var processed atomic.Int64 // total results received (success + error)
	var errored atomic.Int64   // results that had errors
	progressDone := make(chan struct{})
	if showProgress {
		go renderProgressBar(totalFiles, &processed, &errored, progressDone)
	}

	allTypos := make(CheckResults)
	var errs []error
	for result := range results {
		processed.Add(1)
		if result.Err != nil {
			errored.Add(1)
			errs = append(errs, result.Err)
			continue
		}
		if len(result.Typos) > 0 {
			allTypos[result.FilePath] = result.Typos
		}
	}

	close(progressDone)
	if showProgress {
		fmt.Fprint(os.Stderr, "\r"+strings.Repeat(" ", 80)+"\r")
	}

	// Surface every worker failure, not just the first. Successful files are
	// still returned so the user gets a report even when some files error out.
	if len(errs) > 0 {
		return allTypos, errors.Join(errs...)
	}
	return allTypos, nil
}

// runGitDiffChecker restricts the scan to files changed relative to a git ref
// (--git-diff). The ref is resolved by gitDiffFiles; the resulting file list is
// filtered by the same exclude + binary rules as a directory walk so ignored
// or binary files are still skipped. Runs the same worker pool as the walk.
func runGitDiffChecker(ref string, rootPath string, concurrentDict *ConcurrentDictionary, excludePatterns []string, verbose bool) (CheckResults, error) {
	return runGitDiffCheckerWithContext(context.Background(), ref, rootPath, concurrentDict, excludePatterns, verbose, scanOptions{})
}

func runGitDiffCheckerWithContext(ctx context.Context, ref string, rootPath string, concurrentDict *ConcurrentDictionary, excludePatterns []string, verbose bool, opts scanOptions) (CheckResults, error) {
	raw, err := gitDiffFilesWithContext(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("git-diff: %w", err)
	}
	rootPath = filepath.ToSlash(filepath.Clean(rootPath))
	if rootPath != "." {
		filtered := raw[:0]
		for _, p := range raw {
			pNorm := filepath.ToSlash(filepath.Clean(p))
			if pNorm == rootPath || strings.HasPrefix(pNorm, rootPath+"/") {
				filtered = append(filtered, p)
			}
		}
		raw = filtered
	}
	patterns := mergeDefaultExcludes(excludePatterns)
	var files []string
	for _, p := range raw {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		gate, err := classifyFile(p, patterns)
		switch gate {
		case fileExcludeErr:
			if verbose {
				fmt.Fprintf(os.Stderr, "Error checking exclude pattern on %q: %v\n", p, err)
			}
		case fileExcluded:
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping excluded file: %s\n", p)
			}
		case fileBinaryErr:
			if verbose {
				fmt.Fprintf(os.Stderr, "Error checking if file is binary %q: %v\n", p, err)
			}
		case fileBinary:
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping binary file: %s\n", p)
			}
		default:
			files = append(files, p)
		}
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "git-diff: %d file(s) to check\n", len(files))
	}
	return runCheckerOnFilesWithContext(ctx, files, concurrentDict, verbose, opts)
}

func runGitDiffCheckerWithHunks(ctx context.Context, ref string, rootPath string, concurrentDict *ConcurrentDictionary, excludePatterns []string, verbose bool, changedLines ChangedLines, opts scanOptions) (CheckResults, error) {
	raw, err := gitDiffFilesWithContext(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("git-diff: %w", err)
	}
	rootPath = filepath.ToSlash(filepath.Clean(rootPath))
	if rootPath != "." {
		filtered := raw[:0]
		for _, p := range raw {
			pNorm := filepath.ToSlash(filepath.Clean(p))
			if pNorm == rootPath || strings.HasPrefix(pNorm, rootPath+"/") {
				filtered = append(filtered, p)
			}
		}
		raw = filtered
	}
	patterns := mergeDefaultExcludes(excludePatterns)
	var files []string
	for _, p := range raw {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		gate, err := classifyFile(p, patterns)
		switch gate {
		case fileExcludeErr:
			if verbose {
				fmt.Fprintf(os.Stderr, "Error checking exclude pattern on %q: %v\n", p, err)
			}
		case fileExcluded:
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping excluded file: %s\n", p)
			}
		case fileBinaryErr:
			if verbose {
				fmt.Fprintf(os.Stderr, "Error checking if file is binary %q: %v\n", p, err)
			}
		case fileBinary:
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping binary file: %s\n", p)
			}
		default:
			files = append(files, p)
		}
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "git-diff: %d file(s) to check\n", len(files))
	}
	return runCheckerOnFilesWithHunks(ctx, files, concurrentDict, verbose, changedLines, opts)
}

func runCheckerOnFilesWithHunks(ctx context.Context, files []string, concurrentDict *ConcurrentDictionary, verbose bool, changedLines ChangedLines, opts scanOptions) (CheckResults, error) {
	if len(files) == 0 {
		return make(CheckResults), nil
	}
	if changedLines != nil {
		filtered := files[:0]
		for _, f := range files {
			if _, ok := changedLines[f]; ok {
				filtered = append(filtered, f)
			} else if verbose {
				fmt.Fprintf(os.Stderr, "Skipping file with no changed lines: %s\n", f)
			}
		}
		files = filtered
		if len(files) == 0 {
			return make(CheckResults), nil
		}
	}
	totalFiles := len(files)
	numWorkers := runtime.NumCPU()
	jobBuf := numWorkers * 10
	if jobBuf < 100 {
		jobBuf = 100
	}
	jobs := make(chan string, jobBuf)
	results := make(chan CheckResult, jobBuf)
	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go workerWithContext(ctx, &wg, jobs, results, concurrentDict, opts, changedLines)
	}
	go func() {
		defer close(jobs)
		for _, path := range files {
			select {
			case <-ctx.Done():
				return
			case jobs <- path:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	showProgress := totalFiles > 1 && isStderrTerminal()
	var processed atomic.Int64
	var errored atomic.Int64
	progressDone := make(chan struct{})
	if showProgress {
		go renderProgressBar(totalFiles, &processed, &errored, progressDone)
	}
	allTypos := make(CheckResults)
	var errs []error
	for result := range results {
		processed.Add(1)
		if result.Err != nil {
			errored.Add(1)
			errs = append(errs, result.Err)
			continue
		}
		if len(result.Typos) > 0 {
			allTypos[result.FilePath] = result.Typos
		}
	}
	close(progressDone)
	if showProgress {
		fmt.Fprint(os.Stderr, "\r"+strings.Repeat(" ", 80)+"\r")
	}
	if len(errs) > 0 {
		return allTypos, errors.Join(errs...)
	}
	return allTypos, nil
}

// collectFiles walks rootPath and returns a list of file paths that should be
// checked (excludes, binary files, and directories are filtered out).
func collectFiles(rootPath string, excludePatterns []string, verbose bool) ([]string, error) {
	return collectFilesWithContext(context.Background(), rootPath, excludePatterns, verbose)
}

func collectFilesWithContext(ctx context.Context, rootPath string, excludePatterns []string, verbose bool) ([]string, error) {
	var files []string
	patterns := mergeDefaultExcludes(excludePatterns)
	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			// A failed root means nothing can be scanned: report it. A failed
			// subdirectory is just one inaccessible subtree — skip it and
			// keep scanning the rest, matching the per-file error aggregation
			// that already keeps partial results.
			if path == rootPath {
				return err
			}
			fmt.Fprintf(os.Stderr, "Error accessing path %q (skipping): %v\n", path, err)
			return nil
		}

		if info.IsDir() {
			exclude, err := shouldExclude(path, patterns)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error checking exclude pattern on directory %q: %v\n", path, err)
				return nil
			}
			if exclude {
				if verbose {
					fmt.Fprintf(os.Stderr, "Skipping excluded directory: %s\n", path)
				}
				return filepath.SkipDir
			}
			return nil
		}

		gate, err := classifyFile(path, patterns)
		switch gate {
		case fileExcludeErr:
			fmt.Fprintf(os.Stderr, "Error checking exclude pattern on %q: %v\n", path, err)
		case fileExcluded:
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping excluded file: %s\n", path)
			}
		case fileBinaryErr:
			fmt.Fprintf(os.Stderr, "Error checking if file is binary %q: %v\n", path, err)
		case fileBinary:
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping binary file: %s\n", path)
			}
		default:
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// renderProgressBar prints a live progress bar to stderr until all files are done.
func renderProgressBar(total int, processed, errored *atomic.Int64, done <-chan struct{}) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p := int(processed.Load())
			if p >= total {
				printProgressBar(total, total, int(errored.Load()))
				return
			}
			printProgressBar(p, total, int(errored.Load()))
		case <-done:
			printProgressBar(total, total, int(errored.Load()))
			return
		}
	}
}

func printProgressBar(current, total, errored int) {
	const barWidth = 30
	percent := float64(current) / float64(total) * 100
	filled := int(float64(barWidth) * float64(current) / float64(total))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	errSuffix := ""
	if errored > 0 {
		errSuffix = fmt.Sprintf(" (%d errored)", errored)
	}
	fmt.Fprintf(os.Stderr, "\r  %3.0f%% |%s| %d/%d files%s", percent, bar, current, total, errSuffix)
}

func isStderrTerminal() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
func workerWithContext(ctx context.Context, wg *sync.WaitGroup, jobs <-chan string, results chan<- CheckResult, dictionary *ConcurrentDictionary, opts scanOptions, changedLines ChangedLines) {
	defer wg.Done()
	for path := range jobs {
		fileOpts := opts
		if changedLines != nil {
			if set, ok := changedLines[path]; ok {
				fileOpts.ChangedLines = set
			} else {
				select {
				case <-ctx.Done():
					return
				case results <- CheckResult{FilePath: path, Typos: nil, Err: nil}:
				}
				continue
			}
		}
		var typos []MisspelledWord
		var err error
		if changedLines == nil && fileOpts.ChangedLines == nil && opts.MinWordLength == 0 && !opts.Verbose {
			typos, err = checkFileFunc(path, dictionary)
		} else {
			typos, err = checkFileWithOptions(path, dictionary, fileOpts)
		}
		select {
		case <-ctx.Done():
			return
		case results <- CheckResult{FilePath: path, Typos: typos, Err: err}:
		}
	}
}
