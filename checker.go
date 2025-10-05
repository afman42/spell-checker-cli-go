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
)

var wordRegex = regexp.MustCompile(`[a-zA-Z']+(?:-[a-zA-Z']+)*`)

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
	jobs := make(chan string, 100)
	results := make(chan CheckResult, 100)
	var wg sync.WaitGroup
	walkErrCh := make(chan error, 1)

	numWorkers := runtime.NumCPU()
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(&wg, jobs, results, dictionary)
	}

	go func() {
		defer close(jobs)
		walkErrCh <- filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error accessing path %q: %v\n", path, err)
				return err
			}

			if info.IsDir() {
				exclude, err := shouldExclude(path, excludePatterns)
				if err != nil {
					// Log the error and continue, assuming the directory is not excluded
					fmt.Fprintf(os.Stderr, "Error checking exclude pattern on directory %q: %v\n", path, err)
					return nil
				}
				if exclude {
					// --- IMPROVEMENT: Conditionally print skipped directory ---
					if verbose {
						fmt.Printf("Skipping excluded directory: %s\n", path)
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
					fmt.Printf("Skipping excluded file: %s\n", path)
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
					fmt.Printf("Skipping binary file: %s\n", path)
				}
				return nil
			}
			jobs <- path
			return nil
		})
		close(walkErrCh)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	allTypos := make(map[string][]MisspelledWord)
	var workerErr error
	for result := range results {
		if result.Err != nil && workerErr == nil {
			workerErr = result.Err
		}
		if len(result.Typos) > 0 {
			allTypos[result.FilePath] = result.Typos
		}
	}

	var walkErr error
	if err, ok := <-walkErrCh; ok {
		walkErr = err
	}

	if workerErr != nil {
		return nil, workerErr
	}
	if walkErr != nil {
		return nil, walkErr
	}

	return allTypos, nil
}
func worker(wg *sync.WaitGroup, jobs <-chan string, results chan<- CheckResult, dictionary map[string]struct{}) {
	defer wg.Done()
	for path := range jobs {
		typos, err := checkFile(path, dictionary)
		results <- CheckResult{FilePath: path, Typos: typos, Err: err}
	}
}

func checkFile(filePath string, dictionary map[string]struct{}) ([]MisspelledWord, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", filePath, err)
	}
	defer file.Close()

	var misspelledWords []MisspelledWord
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		allMatchesIndices := wordRegex.FindAllStringIndex(line, -1)
		for _, indices := range allMatchesIndices {
			start := indices[0]
			word := line[indices[0]:indices[1]]
			if !isWordCorrect(word, dictionary) {
				suggestions := generateSuggestions(word, dictionary)
				misspelledWords = append(misspelledWords, MisspelledWord{
					Word:        word,
					LineNumber:  lineNumber,
					Column:      start + 1,
					Suggestions: suggestions,
				})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed scanning %s: %w", filePath, err)
	}

	return misspelledWords, nil
}

func isWordCorrect(word string, dictionary map[string]struct{}) bool {
	_, exists := dictionary[strings.ToLower(word)]
	return exists
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
