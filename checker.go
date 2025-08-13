package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

var wordRegex = regexp.MustCompile(`\w+`)

type MisspelledWord struct {
	Word       string
	LineNumber int
	Column     int
}

// CheckResult holds the result of checking a single file.
type CheckResult struct {
	FilePath string
	Typos    []MisspelledWord
}

// runConcurrentChecker is the main orchestrator for the concurrent process.
func runConcurrentChecker(rootPath string, dictionary map[string]struct{}, excludePatterns []string) (map[string][]MisspelledWord, error) {
	// 1. Set up channels for jobs and results.
	jobs := make(chan string, 100)
	results := make(chan CheckResult, 100)
	var wg sync.WaitGroup

	// 2. Start the worker pool.
	numWorkers := runtime.NumCPU() // Use one worker per available CPU core.
	for i := 0; i < numWorkers; i++ {
		wg.Add(1) // Add a worker to the wait group.
		go worker(&wg, jobs, results, dictionary)
	}

	// 3. Start a goroutine to walk the file system and send jobs.
	go func() {
		filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if !info.IsDir() {
				jobs <- path
			}
			return nil
		})
		close(jobs) // CRITICAL: Close the jobs channel when the walk is done.
	}()

	// 4. Start a goroutine to wait for all workers to finish, then close the results channel.
	//    This is key to preventing the race condition.
	go func() {
		wg.Wait()
		close(results)
	}()

	// 5. Collect all results from the results channel until it's closed.
	allTypos := make(map[string][]MisspelledWord)
	for result := range results {
		// Filter out skipped files here, after they've been processed.
		exclude, _ := shouldExclude(result.FilePath, excludePatterns)
		if exclude {
			fmt.Printf("Skipping excluded file: %s\n", result.FilePath)
			continue
		}
		isBinary, _ := isLikelyBinary(result.FilePath)
		if isBinary {
			fmt.Printf("Skipping likely binary file: %s\n", result.FilePath)
			continue
		}

		if len(result.Typos) > 0 {
			allTypos[result.FilePath] = result.Typos
		}
	}

	return allTypos, nil
}

// worker is a goroutine that pulls file paths from the jobs channel and processes them.
func worker(wg *sync.WaitGroup, jobs <-chan string, results chan<- CheckResult, dictionary map[string]struct{}) {
	defer wg.Done() // Signal that this worker is finished when it exits.
	for path := range jobs {
		typos := checkFile(path, dictionary)
		results <- CheckResult{FilePath: path, Typos: typos}
	}
}

// checkFile contains the core logic to find typos in one file.
func checkFile(filePath string, dictionary map[string]struct{}) []MisspelledWord {
	file, err := os.Open(filePath)
	if err != nil {
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
			start := indices[0]
			word := line[indices[0]:indices[1]]
			if !isWordCorrect(word, dictionary) {
				misspelledWords = append(misspelledWords, MisspelledWord{
					Word:       word,
					LineNumber: lineNumber,
					Column:     start + 1,
				})
			}
		}
	}
	return misspelledWords
}

// --- Helper functions remain the same ---

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
	n, _ := file.Read(buffer)
	buffer = buffer[:n]
	if bytes.Contains(buffer, []byte{0}) {
		return true, nil
	}
	return false, nil
}
