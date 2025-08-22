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
)

//go:embed dictionary.csv
var dictionaryData []byte

func loadDictionary(customPath string) (map[string]struct{}, error) {
	var reader io.Reader
	if customPath != "" {
		fmt.Printf("Loading custom dictionary from: %s\n", customPath)
		file, err := os.Open(customPath)
		if err != nil {
			return nil, fmt.Errorf("could not open custom dictionary: %w", err)
		}
		reader = file
	} else {
		fmt.Println("Loading dictionary from embedded data.")
		reader = bytes.NewReader(dictionaryData)
	}
	return parseDictionary(reader)
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
