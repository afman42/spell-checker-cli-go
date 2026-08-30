package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// dictionaryData is a zstd-compressed, newline-delimited, lowercased word list.
// Regenerate it with `go run gen_dict.go` whenever dictionary.csv changes.
//
//go:embed dictionary.txt.zst
var dictionaryData []byte

func loadDictionary(customPath string) (map[string]struct{}, error) {
	if customPath != "" {
		file, err := os.Open(customPath)
		if err != nil {
			return nil, fmt.Errorf("could not open custom dictionary: %w", err)
		}
		defer file.Close()
		return parseDictionary(file)
	}

	return parseEmbeddedDictionary(dictionaryData)
}

// parseEmbeddedDictionary decompresses and reads the embedded zstd-compressed
// word list (one lowercase word per line).
func parseEmbeddedDictionary(data []byte) (map[string]struct{}, error) {
	zr, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("could not open embedded dictionary: %w", err)
	}
	defer zr.Close()

	dictionary := make(map[string]struct{})
	scanner := bufio.NewScanner(zr)
	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		if word != "" {
			dictionary[word] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading embedded dictionary: %w", err)
	}
	return dictionary, nil
}

func parseDictionary(reader io.Reader) (map[string]struct{}, error) {
	dictionary := make(map[string]struct{})
	csvReader := csv.NewReader(reader)
	_, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("could not read dictionary header: %w", err)
	}
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading dictionary record: %w", err)
		}
		if len(record) > 0 {
			dictionary[strings.ToLower(record[0])] = struct{}{}
		}
	}
	return dictionary, nil
}

func loadPersonalDictionary(path string, dictionary map[string]struct{}) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("could not open personal dictionary: %w", err)
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		// Ignore empty lines or comments
		if word != "" && !strings.HasPrefix(word, "#") {
			dictionary[strings.ToLower(word)] = struct{}{}
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error reading personal dictionary: %w", err)
	}

	return count, nil
}
