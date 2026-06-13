package main

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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

			concurrentDict := NewConcurrentDictionary(mockDictionary)
			gotTypos, err := checkFile(filePath, concurrentDict)
			if err != nil {
				t.Fatalf("checkFile returned error: %v", err)
			}

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

// TestCheckFileEdgeCases covers tokenizer and column edge cases.
func TestCheckFileEdgeCases(t *testing.T) {
	mockDictionary := map[string]struct{}{
		"café": {}, "hello": {}, "world": {}, "it's": {}, "a": {}, "test": {},
	}

	testCases := []struct {
		name          string
		fileContent   string
		expectedTypos []MisspelledWord
	}{
		{
			// "café " is 5 runes but 6 bytes (é is 2 bytes). The column for the
			// following word must be counted in runes, not bytes.
			name:        "rune column after multibyte char",
			fileContent: "café xyzzy",
			expectedTypos: []MisspelledWord{
				{Word: "xyzzy", LineNumber: 1, Column: 6, Suggestions: []string{}},
			},
		},
		{
			name:          "surrounding quotes are stripped",
			fileContent:   "'hello' \"world\"",
			expectedTypos: nil,
		},
		{
			name:          "pure punctuation produces no words",
			fileContent:   "!!! ??? --- '''",
			expectedTypos: nil,
		},
		{
			name:          "valid contraction is accepted",
			fileContent:   "it's a test",
			expectedTypos: nil,
		},
		{
			name:        "typo on second line reports correct line number",
			fileContent: "hello world\nhello zzzz",
			expectedTypos: []MisspelledWord{
				{Word: "zzzz", LineNumber: 2, Column: 7, Suggestions: []string{}},
			},
		},
		{
			name:        "numbers are not treated as words",
			fileContent: "test 12345 test",
			// digits don't match \p{L}, so no typo is produced
			expectedTypos: nil,
		},
		{
			name:          "empty file yields no typos",
			fileContent:   "",
			expectedTypos: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			filePath := filepath.Join(tempDir, "testfile.txt")
			if err := os.WriteFile(filePath, []byte(tc.fileContent), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			concurrentDict := NewConcurrentDictionary(mockDictionary)
			gotTypos, err := checkFile(filePath, concurrentDict)
			if err != nil {
				t.Fatalf("checkFile returned error: %v", err)
			}

			if len(gotTypos) == 0 && len(tc.expectedTypos) == 0 {
				return
			}
			if !reflect.DeepEqual(gotTypos, tc.expectedTypos) {
				t.Errorf("checkFile() = %#v, want %#v", gotTypos, tc.expectedTypos)
			}
		})
	}
}

// TestCheckFileMissing ensures opening a nonexistent file returns an error.
func TestCheckFileMissing(t *testing.T) {
	dict := NewConcurrentDictionary(map[string]struct{}{"word": {}})
	if _, err := checkFile(filepath.Join(t.TempDir(), "nope.txt"), dict); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestCollectFiles verifies filtering of excluded patterns, binary files, and dirs.
func TestCollectFiles(t *testing.T) {
	tempDir := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(tempDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("keep.txt", "hello")
	write("skip.log", "hello")
	write("bin.dat", "a\x00b")
	write("nested/keep2.md", "hello")
	write("vendor/dep.txt", "hello")

	got, err := collectFiles(tempDir, []string{"*.log", "vendor"}, false)
	if err != nil {
		t.Fatalf("collectFiles error: %v", err)
	}

	want := []string{
		filepath.Join(tempDir, "keep.txt"),
		filepath.Join(tempDir, "nested/keep2.md"),
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collectFiles() = %v, want %v", got, want)
	}
}

// TestCollectFilesEmptyDir verifies an empty directory yields no files and no error.
func TestCollectFilesEmptyDir(t *testing.T) {
	got, err := collectFiles(t.TempDir(), nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no files, got %v", got)
	}
}

// TestIsLikelyBinary checks detection of binary vs text content.
func TestIsLikelyBinary(t *testing.T) {
	tempDir := t.TempDir()
	cases := []struct {
		name    string
		content []byte
		want    bool
	}{
		{"plain text", []byte("just some text"), false},
		{"empty file", []byte{}, false},
		{"null byte", []byte("abc\x00def"), true},
		{"utf8 text", []byte("café déjà vu"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(tempDir, tc.name+".dat")
			if err := os.WriteFile(p, tc.content, 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := isLikelyBinary(p)
			if err != nil {
				t.Fatalf("isLikelyBinary error: %v", err)
			}
			if got != tc.want {
				t.Errorf("isLikelyBinary(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestShouldExcludeInvalidPattern verifies a malformed glob returns an error.
func TestShouldExcludeInvalidPattern(t *testing.T) {
	if _, err := shouldExclude("file.txt", []string{"[invalid"}); err == nil {
		t.Fatal("expected error for malformed pattern, got nil")
	}
}

// TestScanForTyposCRLF ensures carriage returns don't corrupt tokenizing.
func TestScanForTyposCRLF(t *testing.T) {
	dict := NewConcurrentDictionary(map[string]struct{}{"hello": {}, "world": {}})
	typos, err := scanForTypos(strings.NewReader("hello world\r\nhello world\r\n"), dict)
	if err != nil {
		t.Fatalf("scanForTypos error: %v", err)
	}
	if len(typos) != 0 {
		t.Errorf("expected no typos, got %#v", typos)
	}
}

func TestRunConcurrentCheckerWorkerError(t *testing.T) {
	mockDictionary := map[string]struct{}{"word": {}}
	tempDir := t.TempDir()

	longToken := strings.Repeat("a", bufio.MaxScanTokenSize+10)
	if err := os.WriteFile(filepath.Join(tempDir, "long.txt"), []byte(longToken), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err := runConcurrentChecker(tempDir, mockDictionary, nil, false)
	if err == nil {
		t.Fatal("expected error from runConcurrentChecker, got nil")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("expected ErrTooLong, got %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result, got %v", result)
	}
}

func TestRunConcurrentCheckerWalkError(t *testing.T) {
	mockDictionary := map[string]struct{}{"word": {}}
	missingPath := filepath.Join(t.TempDir(), "does-not-exist")

	if _, err := runConcurrentChecker(missingPath, mockDictionary, nil, false); err == nil {
		t.Fatal("expected error from runConcurrentChecker when root missing")
	}
}

func TestCheckFileScannerError(t *testing.T) {
	mockDictionary := map[string]struct{}{"word": {}}
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "long.txt")

	longToken := strings.Repeat("b", bufio.MaxScanTokenSize+10)
	if err := os.WriteFile(filePath, []byte(longToken), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	concurrentDict := NewConcurrentDictionary(mockDictionary)
	_, err := checkFile(filePath, concurrentDict)
	if err == nil {
		t.Fatal("expected scanner error, got nil")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("expected ErrTooLong, got %v", err)
	}
}

func TestIsLikelyBinaryReadError(t *testing.T) {
	dir := t.TempDir()
	if _, err := isLikelyBinary(dir); err == nil {
		t.Fatal("expected error when checking directory")
	}
}
