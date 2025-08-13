package main

import (
	"strings"
	"testing"
)

func TestParseDictionary(t *testing.T) {
	t.Run("parses a valid CSV dictionary", func(t *testing.T) {
		// Create a sample CSV as a string
		csvData := `word,definition
hello,"a greeting"
world,"the earth"
Golang,"a programming language"`

		reader := strings.NewReader(csvData)
		dict, err := parseDictionary(reader)

		if err != nil {
			t.Fatalf("Expected no error, but got %v", err)
		}

		if len(dict) != 3 {
			t.Errorf("Expected dictionary length to be 3, but got %d", len(dict))
		}

		// Check for a word (should be stored in lowercase)
		if _, ok := dict["hello"]; !ok {
			t.Error("Expected 'hello' to be in the dictionary")
		}
		if _, ok := dict["golang"]; !ok {
			t.Error("Expected 'golang' to be in the dictionary (case-insensitive)")
		}
		if _, ok := dict["goodbye"]; ok {
			t.Error("Expected 'goodbye' to not be in the dictionary")
		}
	})

	t.Run("returns an error for empty input", func(t *testing.T) {
		reader := strings.NewReader("")
		_, err := parseDictionary(reader)
		if err == nil {
			t.Fatal("Expected an error for empty input, but got nil")
		}
	})
}
