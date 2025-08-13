package main

import (
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

// loadDictionary handles the logic of choosing between the embedded and a custom dictionary.
func loadDictionary(customPath string) (map[string]struct{}, error) {
	var reader io.Reader
	if customPath != "" {
		// User provided a custom dictionary path.
		fmt.Printf("Loading custom dictionary from: %s\n", customPath)
		file, err := os.Open(customPath)
		if err != nil {
			return nil, fmt.Errorf("could not open custom dictionary: %w", err)
		}
		// Note: The file will be closed when the main function exits.
		// For larger apps, manage this more carefully.
		reader = file
	} else {
		// Default case: use the embedded dictionary.
		fmt.Println("Loading dictionary from embedded data.")
		reader = bytes.NewReader(dictionaryData)
	}
	return parseDictionary(reader)
}

// parseDictionary reads from any io.Reader and creates the dictionary map.
func parseDictionary(reader io.Reader) (map[string]struct{}, error) {
	dictionary := make(map[string]struct{})
	csvReader := csv.NewReader(reader)
	_, err := csvReader.Read() // Skip header
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
