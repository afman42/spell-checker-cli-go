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
