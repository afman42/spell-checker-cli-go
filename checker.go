package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var (
	// wordRegex tokenizes words. It matches Unicode letters, contractions
	// (don't, it's, it’s), and hyphenated words (state-of-the-art). Leading or
	// trailing apostrophes/quotes are not captured, and runs of only
	// punctuation produce no match.
	wordRegex = regexp.MustCompile(`\p{L}+(?:['’\-]\p{L}+)*`)
)

// ConcurrentDictionary provides thread-safe read access to the dictionary
// and caches a BK-tree for efficient fuzzy suggestions.
type ConcurrentDictionary struct {
	dict   map[string]struct{}
	bkTree *BKTree
}

// NewConcurrentDictionary creates a new dictionary wrapper and pre-builds
// a BK-tree for fast fuzzy matching when the dictionary is large enough.
func NewConcurrentDictionary(dict map[string]struct{}) *ConcurrentDictionary {
	cd := &ConcurrentDictionary{
		dict: dict,
	}
	if len(dict) >= 100 {
		cd.bkTree = NewBKTree(dict)
	}
	return cd
}

// Contains checks if a word exists in the dictionary
func (cd *ConcurrentDictionary) Contains(word string) bool {
	_, exists := cd.dict[strings.ToLower(word)]
	return exists
}

// GetDict returns the underlying dictionary map (for suggestions)
func (cd *ConcurrentDictionary) GetDict() map[string]struct{} {
	return cd.dict
}

// Suggest returns ranked spelling suggestions using the cached BK-tree (fast
// path) or falls back to brute-force for small dictionaries.
func (cd *ConcurrentDictionary) Suggest(word string) []string {
	if len(word) > maxSuggestionWordLength {
		return nil
	}
	if cd.bkTree != nil {
		return rankSuggestions(cd.bkTree.Search(strings.ToLower(word), levenshteinThreshold))
	}
	return simpleGenerateSuggestions(word, cd.dict)
}

type MisspelledWord struct {
	Word        string
	LineNumber  int
	Column      int
	Suggestions []string
}

type CheckResult struct {
	FilePath string
	Typos    []MisspelledWord
	Err      error
}

func runConcurrentChecker(rootPath string, dictionary map[string]struct{}, excludePatterns []string, verbose bool) (map[string][]MisspelledWord, error) {
	return runConcurrentCheckerWithDict(rootPath, NewConcurrentDictionary(dictionary), excludePatterns, verbose)
}

// runConcurrentCheckerWithDict is the core scanner using an already-built
// ConcurrentDictionary, so the BK-tree is constructed only once per run.
func runConcurrentCheckerWithDict(rootPath string, concurrentDict *ConcurrentDictionary, excludePatterns []string, verbose bool) (map[string][]MisspelledWord, error) {
	// Phase 1: Collect all file paths (filters applied)
	allFiles, err := collectFiles(rootPath, excludePatterns, verbose)
	if err != nil {
		return nil, err
	}
	if len(allFiles) == 0 {
		return make(map[string][]MisspelledWord), nil
	}

	totalFiles := len(allFiles)
	numWorkers := runtime.NumCPU()
	jobBuf := numWorkers * 10
	if jobBuf < 100 {
		jobBuf = 100
	}
	jobs := make(chan string, jobBuf)
	results := make(chan CheckResult, jobBuf)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(&wg, jobs, results, concurrentDict)
	}

	// Sender goroutine: pushes collected file paths to workers
	go func() {
		for _, path := range allFiles {
			jobs <- path
		}
		close(jobs)
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

	allTypos := make(map[string][]MisspelledWord)
	var workerErr error
	for result := range results {
		processed.Add(1)
		if result.Err != nil {
			errored.Add(1)
			if workerErr == nil {
				workerErr = result.Err
			}
		}
		if len(result.Typos) > 0 {
			allTypos[result.FilePath] = result.Typos
		}
	}

	close(progressDone)
	if showProgress {
		fmt.Fprint(os.Stderr, "\r"+strings.Repeat(" ", 80)+"\r")
	}

	if workerErr != nil {
		return nil, workerErr
	}
	return allTypos, nil
}

// collectFiles walks rootPath and returns a list of file paths that should be
// checked (excludes, binary files, and directories are filtered out).
func collectFiles(rootPath string, excludePatterns []string, verbose bool) ([]string, error) {
	var files []string
	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error accessing path %q: %v\n", path, err)
			return err
		}

		if info.IsDir() {
			exclude, err := shouldExclude(path, excludePatterns)
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

		exclude, err := shouldExclude(path, excludePatterns)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking exclude pattern on %q: %v\n", path, err)
			return nil
		}
		if exclude {
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping excluded file: %s\n", path)
			}
			return nil
		}

		isBinary, err := isLikelyBinary(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking if file is binary %q: %v\n", path, err)
			return nil
		}
		if isBinary {
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping binary file: %s\n", path)
			}
			return nil
		}

		files = append(files, path)
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
func worker(wg *sync.WaitGroup, jobs <-chan string, results chan<- CheckResult, dictionary *ConcurrentDictionary) {
	defer wg.Done()
	for path := range jobs {
		typos, err := checkFile(path, dictionary)
		results <- CheckResult{FilePath: path, Typos: typos, Err: err}
	}
}

func checkFile(filePath string, dictionary *ConcurrentDictionary) ([]MisspelledWord, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", filePath, err)
	}
	defer file.Close()

	misspelledWords, err := scanForTypos(file, dictionary)
	if err != nil {
		return nil, fmt.Errorf("failed scanning %s: %w", filePath, err)
	}
	return misspelledWords, nil
}

// scanForTypos reads a stream line by line and returns misspelled words with
// 1-based line and (rune) column positions.
func scanForTypos(r io.Reader, dictionary *ConcurrentDictionary) ([]MisspelledWord, error) {
	var misspelledWords []MisspelledWord
	scanner := bufio.NewScanner(r)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		for _, indices := range wordRegex.FindAllStringIndex(line, -1) {
			word := line[indices[0]:indices[1]]
			if !dictionary.Contains(word) {
				misspelledWords = append(misspelledWords, MisspelledWord{
					Word:        word,
					LineNumber:  lineNumber,
					Column:      utf8.RuneCountInString(line[:indices[0]]) + 1,
					Suggestions: dictionary.Suggest(word),
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return misspelledWords, nil
}

func shouldExclude(filePath string, patterns []string) (bool, error) {
	fileName := filepath.Base(filePath)
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, fileName)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func isLikelyBinary(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false, err
	}
	buffer = buffer[:n]
	if bytes.Contains(buffer, []byte{0}) {
		return true, nil
	}
	return false, nil
}

// checkStdin processes text input from stdin
func checkStdin(dictionary *ConcurrentDictionary) ([]MisspelledWord, error) {
	misspelledWords, err := scanForTypos(os.Stdin, dictionary)
	if err != nil {
		return nil, fmt.Errorf("error reading stdin: %w", err)
	}
	return misspelledWords, nil
}
