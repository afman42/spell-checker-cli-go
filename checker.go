package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var wordRegex = regexp.MustCompile(`\w+`)

// MisspelledWord holds the details of a spelling error.
type MisspelledWord struct {
	Word       string
	LineNumber int
	Column     int
}

// findTypos is a high-level function to check a path (file or directory).
func findTypos(path string, dictionary map[string]struct{}, excludePatterns []string) (map[string][]MisspelledWord, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	allTypos := make(map[string][]MisspelledWord)
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			filePath := filepath.Join(path, entry.Name())
			exclude, _ := shouldExclude(filePath, excludePatterns)
			if exclude {
				fmt.Printf("Skipping excluded file: %s\n", filePath)
				continue
			}
			typos := checkFile(filePath, dictionary)
			if len(typos) > 0 {
				allTypos[filePath] = typos
			}
		}
	} else {
		exclude, _ := shouldExclude(path, excludePatterns)
		if exclude {
			fmt.Printf("Skipping excluded file: %s\n", path)
		} else {
			typos := checkFile(path, dictionary)
			if len(typos) > 0 {
				allTypos[path] = typos
			}
		}
	}
	return allTypos, nil
}

// checkFile contains the logic to spell-check a single file.
func checkFile(filePath string, dictionary map[string]struct{}) []MisspelledWord {
	isBinary, err := isLikelyBinary(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not read file %s: %v\n", filePath, err)
		return nil
	}
	if isBinary {
		fmt.Fprintf(os.Stdout, "Skipping likely binary file: %s\n", filePath)
		return nil
	}
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
