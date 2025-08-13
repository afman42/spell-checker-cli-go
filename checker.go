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
	"unicode"
)

var wordRegex = regexp.MustCompile(`\w+`)

type MisspelledWord struct {
	Word       string
	LineNumber int
	Column     int
}

// CheckResult holds the result of a single file check.
type CheckResult struct {
	FilePath string
	Typos    []MisspelledWord
}

// runConcurrentChecker is the new orchestrator for the entire spell-checking process.
func runConcurrentChecker(rootPath string, dictionary map[string]struct{}, excludePatterns []string) (map[string][]MisspelledWord, error) {
	// --- Setup Channels and WaitGroup ---
	jobs := make(chan string, 100)
	results := make(chan CheckResult, 100)
	var wg sync.WaitGroup

	// --- Start Worker Goroutines ---
	numWorkers := runtime.NumCPU() // Use a worker for each CPU core
	for i := 0; i < numWorkers; i++ {
		go worker(&wg, jobs, results, dictionary)
	}

	// --- Start a goroutine to walk the file path and send jobs ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// Skip directories, excluded files, and binary files
			if !info.IsDir() {
				exclude, _ := shouldExclude(path, excludePatterns)
				isBinary, _ := isLikelyBinary(path)
				if exclude {
					fmt.Printf("Skipping excluded file: %s\n", path)
					return nil
				}
				if isBinary {
					fmt.Printf("Skipping likely binary file: %s\n", path)
					return nil
				}
				wg.Add(1)
				jobs <- path
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error walking path: %v\n", err)
		}
		close(jobs) // Close jobs channel when walking is done
	}()

	// --- Start a goroutine to collect results ---
	allTypos := make(map[string][]MisspelledWord)
	go func() {
		for result := range results {
			if len(result.Typos) > 0 {
				allTypos[result.FilePath] = result.Typos
			}
		}
	}()

	// --- Wait for all jobs to finish and close results channel ---
	wg.Wait()
	close(results)

	return allTypos, nil
}

// worker is a goroutine that pulls file paths from the jobs channel and processes them.
func worker(wg *sync.WaitGroup, jobs <-chan string, results chan<- CheckResult, dictionary map[string]struct{}) {
	for path := range jobs {
		typos := checkFile(path, dictionary)
		results <- CheckResult{FilePath: path, Typos: typos}
		wg.Done()
	}
}

// checkFile now just focuses on checking one file. The skipping logic has been moved up.
func checkFile(filePath string, dictionary map[string]struct{}) []MisspelledWord {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file %s: %v\n", filePath, err)
		return nil
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
			start, end := indices[0], indices[1]
			word := line[start:end]
			if !isWordCorrect(word, dictionary) {
				misspelledWords = append(misspelledWords, MisspelledWord{Word: word, LineNumber: lineNumber, Column: start + 1})
			}
		}
	}
	return misspelledWords
}

// --- isWordCorrect, shouldExclude, isLikelyBinary remain the same ---
func isWordCorrect(word string, dictionary map[string]struct{}) bool {
	_, exists := dictionary[strings.ToLower(word)]
	return exists
}

func shouldExclude(filePath string, patterns []string) (bool, error) {
	if len(patterns) == 0 {
		return false, nil
	}
	fileName := filepath.Base(filePath)
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, fileName)
		if err != nil {
			return false, fmt.Errorf("invalid exclude pattern '%s': %w", pattern, err)
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
	nonPrintable := 0
	for _, b := range buffer {
		if !unicode.IsPrint(rune(b)) && !unicode.IsSpace(rune(b)) {
			nonPrintable++
		}
	}
	if n > 0 && float64(nonPrintable)/float64(n) > 0.3 {
		return true, nil
	}
	return false, nil
}
