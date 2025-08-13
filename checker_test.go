package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Test for shouldExclude remains the same as it was correct.
func TestShouldExclude(t *testing.T) {
	testCases := []struct {
		name      string
		filePath  string
		patterns  []string
		want      bool
		expectErr bool
	}{
		{"exact match", "report.log", []string{"report.log"}, true, false},
		{"wildcard match", "report.log", []string{"*.log"}, true, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := shouldExclude(tc.filePath, tc.patterns)
			if (err != nil) != tc.expectErr {
				t.Fatalf("shouldExclude() error = %v, wantErr %v", err, tc.expectErr)
			}
			if got != tc.want {
				t.Errorf("shouldExclude() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckFile(t *testing.T) {
	mockDictionary := map[string]struct{}{
		"hello": {}, "world": {}, "this": {}, "is": {}, "a": {}, "test": {}, "go": {}, "of": {},
	}
	tempDir := t.TempDir()

	t.Run("file with typos", func(t *testing.T) {
		content := "Hello world, this is a tst of go."
		filePath := filepath.Join(tempDir, "test1.txt")
		os.WriteFile(filePath, []byte(content), 0644)

		typos := checkFile(filePath, mockDictionary)

		if len(typos) != 1 {
			t.Fatalf("Expected 1 typo, but got %d", len(typos))
		}

		// Let's check the single typo found
		foundTypo := typos[0]
		if foundTypo.Word != "tst" {
			t.Errorf("Expected typo to be 'tst', but got '%s'", foundTypo.Word)
		}
		if foundTypo.LineNumber != 1 {
			t.Errorf("Expected line number to be 1, but got %d", foundTypo.LineNumber)
		}

		expectedColumn := 24
		if foundTypo.Column != expectedColumn {
			t.Errorf("Expected column to be %d, but got %d", expectedColumn, foundTypo.Column)
		}
	})

	t.Run("file with no typos", func(t *testing.T) {
		content := "hello world this is a test"
		filePath := filepath.Join(tempDir, "test2.txt")
		os.WriteFile(filePath, []byte(content), 0644)

		typos := checkFile(filePath, mockDictionary)
		if len(typos) != 0 {
			t.Errorf("Expected 0 typos, but got %d", len(typos))
		}
	})
}
