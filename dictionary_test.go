package main

import (
	"os"
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

func TestLoadPersonalDictionary(t *testing.T) {
	// 1. Create a pre-existing dictionary.
	existingDict := map[string]struct{}{
		"hello": {},
		"world": {},
	}

	// 2. Create a temporary personal dictionary file.
	content := `
	  Qopper
	FluxCapacitor
	# This is a comment
	bigcorp-api

	` // Includes whitespace, comments, and empty lines to test robustness.
	tmpFile, err := os.CreateTemp("", "personal-dict-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// 3. Run the function to load and merge the words.
	count, err := loadPersonalDictionary(tmpFile.Name(), existingDict)
	if err != nil {
		t.Fatalf("loadPersonalDictionary failed: %v", err)
	}

	// 4. Assertions.
	if count != 3 {
		t.Errorf("Expected to load 3 words, but got %d", count)
	}

	expectedWords := []string{"hello", "world", "qopper", "fluxcapacitor", "bigcorp-api"}
	if len(existingDict) != len(expectedWords) {
		t.Errorf("Expected final dictionary size to be %d, but got %d", len(expectedWords), len(existingDict))
	}

	for _, word := range expectedWords {
		if _, ok := existingDict[word]; !ok {
			t.Errorf("Expected dictionary to contain '%s', but it did not", word)
		}
	}
}
