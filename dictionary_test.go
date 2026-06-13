package main

import (
	"os"
	"path/filepath"
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

func TestLoadDictionaryCustomFile(t *testing.T) {
	tempFile, err := os.CreateTemp(t.TempDir(), "dict-*.csv")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	content := "word,definition\nHello,world\nFoobar,test"
	if _, err := tempFile.WriteString(content); err != nil {
		t.Fatalf("failed to write temp dictionary: %v", err)
	}
	tempFile.Close()

	dict, err := loadDictionary(tempFile.Name())
	if err != nil {
		t.Fatalf("loadDictionary returned error: %v", err)
	}

	if len(dict) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(dict))
	}
	if _, ok := dict["hello"]; !ok {
		t.Fatalf("expected 'hello' in dictionary")
	}
	if _, ok := dict["foobar"]; !ok {
		t.Fatalf("expected 'foobar' in dictionary")
	}
}

// TestLoadDictionaryEmbedded verifies the embedded dictionary loads and is non-trivial.
func TestLoadDictionaryEmbedded(t *testing.T) {
	dict, err := loadDictionary("")
	if err != nil {
		t.Fatalf("loadDictionary(\"\") returned error: %v", err)
	}
	if len(dict) < 1000 {
		t.Errorf("expected embedded dictionary to have many words, got %d", len(dict))
	}
}

// TestLoadDictionaryMissingFile verifies a helpful error for a nonexistent path.
func TestLoadDictionaryMissingFile(t *testing.T) {
	_, err := loadDictionary(filepath.Join(t.TempDir(), "does-not-exist.csv"))
	if err == nil {
		t.Fatal("expected error for missing dictionary file, got nil")
	}
}

// TestParseDictionaryHeaderOnly verifies a header-only CSV yields an empty dict (no error).
func TestParseDictionaryHeaderOnly(t *testing.T) {
	dict, err := parseDictionary(strings.NewReader("word,definition\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dict) != 0 {
		t.Errorf("expected empty dictionary, got %d entries", len(dict))
	}
}

// TestParseDictionaryDuplicateAndCase verifies case-folding and de-duplication.
func TestParseDictionaryDuplicateAndCase(t *testing.T) {
	dict, err := parseDictionary(strings.NewReader("word\nHello\nhello\nHELLO\nWorld"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dict) != 2 {
		t.Errorf("expected 2 unique words after case-folding, got %d (%v)", len(dict), dict)
	}
	if _, ok := dict["hello"]; !ok {
		t.Error("expected 'hello' present")
	}
}

// TestLoadPersonalDictionaryMissingFile verifies an error for a missing personal dict.
func TestLoadPersonalDictionaryMissingFile(t *testing.T) {
	dict := map[string]struct{}{}
	if _, err := loadPersonalDictionary(filepath.Join(t.TempDir(), "nope.txt"), dict); err == nil {
		t.Fatal("expected error for missing personal dictionary, got nil")
	}
}

// TestLoadPersonalDictionaryCommentsOnly verifies a file of only comments/blanks
// adds nothing and reports a zero count.
func TestLoadPersonalDictionaryCommentsOnly(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "personal-*.txt")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	if _, err := tmp.WriteString("# just a comment\n\n   \n# another\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	tmp.Close()

	dict := map[string]struct{}{"existing": {}}
	count, err := loadPersonalDictionary(tmp.Name(), dict)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 words added, got %d", count)
	}
	if len(dict) != 1 {
		t.Errorf("expected dictionary unchanged (size 1), got %d", len(dict))
	}
}
