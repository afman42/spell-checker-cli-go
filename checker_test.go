package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// Test for shouldExclude remains the same as it correctly tests the pattern logic.
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
		{"no match", "main.go", []string{"*.log"}, false, false},
		{"directory match", "node_modules", []string{"node_modules"}, true, false},
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

// REWRITTEN: TestCheckFile is now a table-driven test for better coverage and readability.
func TestCheckFile(t *testing.T) {
	// A common dictionary for all test cases.
	mockDictionary := map[string]struct{}{
		"hello": {}, "world": {}, "they're": {}, "a": {}, "test": {},
		"state-of-the-art": {}, "error": {},
	}

	testCases := []struct {
		name          string
		fileContent   string
		expectedTypos []MisspelledWord
	}{
		{
			name:          "file with no typos",
			fileContent:   "hello world",
			expectedTypos: nil, // Expect nil or an empty slice
		},
		{
			name:        "file with one typo and suggestions",
			fileContent: "hello wrld",
			expectedTypos: []MisspelledWord{
				{Word: "wrld", LineNumber: 1, Column: 7, Suggestions: []string{"world"}},
			},
		},
		{
			name:          "file with correct contraction",
			fileContent:   "they're a test",
			expectedTypos: nil,
		},
		{
			name:          "file with correct hyphenated word",
			fileContent:   "a state-of-the-art test",
			expectedTypos: nil,
		},
		{
			name:        "file with misspelled hyphenated word",
			fileContent: "a state-of-the-artt test",
			expectedTypos: []MisspelledWord{
				{Word: "state-of-the-artt", LineNumber: 1, Column: 3, Suggestions: []string{"state-of-the-art"}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a temporary file for the test case.
			tempDir := t.TempDir()
			filePath := filepath.Join(tempDir, "testfile.txt")
			if err := os.WriteFile(filePath, []byte(tc.fileContent), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			gotTypos := checkFile(filePath, mockDictionary)

			// Normalize for comparison: treat a nil slice and an empty slice as the same.
			if len(gotTypos) == 0 && len(tc.expectedTypos) == 0 {
				return // They are effectively equal, so we pass.
			}

			if !reflect.DeepEqual(gotTypos, tc.expectedTypos) {
				t.Errorf("checkFile() returned incorrect typos.\nGOT:\n%v\nWANT:\n%v", gotTypos, tc.expectedTypos)
			}
		})
	}
}

func TestRunConcurrentChecker(t *testing.T) {
	mockDictionary := map[string]struct{}{
		"hello": {}, "world": {}, "this": {}, "is": {}, "a": {}, "test": {}, "some": {}, "text": {}, "package": {},
	}
	tempDir := t.TempDir()

	// Helper to create test files and directories
	createFile := func(relPath, content string) {
		fullPath := filepath.Join(tempDir, relPath)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", relPath, err)
		}
	}

	// Create a realistic test directory structure
	createFile("file_with_typo.txt", "hello wrld")
	createFile("file_no_typo.txt", "hello world")
	createFile("report.log", "this is an errror")     // Should be excluded by pattern
	createFile("a_binary_file.bin", "hello\x00world") // Should be skipped as binary
	createFile("subdir/another.txt", "anothr typo")
	createFile("node_modules/package.json", "some tst here") // Should be skipped via directory exclusion

	// Define exclusion patterns
	excludePatterns := []string{"*.log", "*.bin", "node_modules"}

	// Run the concurrent checker on the temporary directory
	results, err := runConcurrentChecker(tempDir, mockDictionary, excludePatterns, false) // verbose=false
	if err != nil {
		t.Fatalf("runConcurrentChecker failed: %v", err)
	}

	// --- Assertions ---

	// We expect typos to be found in exactly 2 files
	if len(results) != 2 {
		t.Errorf("Expected results for 2 files, but got %d for files: %v", len(results), results)
	}

	// Check that the correct files are present in the results map
	expectedFilesWithTypos := []string{
		filepath.Join(tempDir, "file_with_typo.txt"),
		filepath.Join(tempDir, "subdir/another.txt"),
	}
	resultKeys := make([]string, 0, len(results))
	for k := range results {
		resultKeys = append(resultKeys, k)
	}
	sort.Strings(expectedFilesWithTypos)
	sort.Strings(resultKeys)

	if !reflect.DeepEqual(expectedFilesWithTypos, resultKeys) {
		t.Errorf("Expected files with typos to be %v, but got %v", expectedFilesWithTypos, resultKeys)
	}

	// Verify the specific typos found in one of the files
	filePath := filepath.Join(tempDir, "file_with_typo.txt")
	if typos, ok := results[filePath]; ok {
		if len(typos) != 1 {
			t.Fatalf("Expected 1 typo in %s, but got %d", filePath, len(typos))
		}
		if typos[0].Word != "wrld" {
			t.Errorf("Expected typo to be 'wrld', but got '%s'", typos[0].Word)
		}
	} else {
		t.Errorf("Expected to find results for %s, but did not", filePath)
	}
}
