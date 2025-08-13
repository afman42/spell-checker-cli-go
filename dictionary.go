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
