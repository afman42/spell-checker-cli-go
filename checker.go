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

type CheckResult struct {
	FilePath string
	Typos    []MisspelledWord
}

func runConcurrentChecker(rootPath string, dictionary map[string]struct{}, excludePatterns []string) (map[string][]MisspelledWord, error) {
	jobs := make(chan string, 100)
	results := make(chan CheckResult, 100)
	var wg sync.WaitGroup

	numWorkers := runtime.NumCPU()
	for i := 0; i < numWorkers; i++ {
		go worker(&wg, jobs, results, dictionary)
	}

	go func() {
		filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if !info.IsDir() {
				wg.Add(1)
				jobs <- path
			}
			return nil
		})
		close(jobs)
	}()

	allTypos := make(map[string][]MisspelledWord)
	resultsWg := sync.WaitGroup{}
	resultsWg.Add(1)
	go func() {
		defer resultsWg.Done()
		for result := range results {
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
	}()

	wg.Wait()
	close(results)
	resultsWg.Wait()

	return allTypos, nil
}

func worker(wg *sync.WaitGroup, jobs <-chan string, results chan<- CheckResult, dictionary map[string]struct{}) {
	for path := range jobs {
		typos := checkFile(path, dictionary)
		results <- CheckResult{FilePath: path, Typos: typos}
		wg.Done()
	}
}

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
					Column:     start + 1, // DEFINITIVE FIX: 1-based column from 0-based index.
				})
			}
		}
	}
	return misspelledWords
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
	n, _ := file.Read(buffer)
	buffer = buffer[:n]
	if bytes.Contains(buffer, []byte{0}) {
		return true, nil
	}
	return false, nil
}
