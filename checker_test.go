package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// runConcurrentChecker is the test convenience wrapper around
// runConcurrentCheckerWithDict; production code calls the WithDict variant
// directly so the BK-tree is built once per run.
func runConcurrentChecker(rootPath string, dictionary map[string]struct{}, excludePatterns []string, verbose bool) (map[string][]MisspelledWord, error) {
	return runConcurrentCheckerWithDict(rootPath, NewConcurrentDictionary(dictionary), excludePatterns, verbose)
}

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
		// Trailing-slash normalization: "build/" should match a directory
		// named "build" just like "build" does. The README advertises both
		// forms; previously only the slash-less form worked.
		{"trailing slash matches dir", "build", []string{"build/"}, true, false},
		{"trailing slash no match", "main.go", []string{"build/"}, false, false},
		{"backslash trailing normalized", "build", []string{"build\\"}, true, false},
		{"empty pattern skipped", "any.txt", []string{""}, false, false},
		// Path-glob patterns (containing /) match against the full relative
		// path, not just the basename. This is the fix for the README's
		// advertised "third_party/**" and "src/generated/*" patterns.
		{"path glob dir contents", "third_party/x.txt", []string{"third_party/*"}, true, false},
		{"path glob no match", "src/main.go", []string{"third_party/*"}, false, false},
		{"path glob subdir", "src/generated/a.go", []string{"src/generated/*"}, true, false},
		{"path glob double star", "third_party/deep/nested/x.txt", []string{"third_party/**"}, true, false},
		{"path glob with dot prefix", "./third_party/x.txt", []string{"third_party/*"}, true, false},
		{"basename pattern still works", "x.log", []string{"*.log"}, true, false},
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

// TestScanForTyposLongLine verifies the scanner buffer was raised so lines
// between the default 64 KiB token limit and the 1 MiB cap are scanned
// successfully instead of returning bufio.ErrTooLong.
func TestScanForTyposLongLine(t *testing.T) {
	dict := NewConcurrentDictionary(map[string]struct{}{"hello": {}, "world": {}})
	// A line just over the default token limit — previously failed.
	long := strings.Repeat("a", bufio.MaxScanTokenSize+10)
	typos, err := scanForTypos(strings.NewReader(long), dict)
	if err != nil {
		t.Fatalf("scanForTypos failed on a long line: %v", err)
	}
	if len(typos) != 1 {
		t.Errorf("expected 1 typo (the long non-dictionary word), got %d", len(typos))
	}
}

// TestScanForTyposHugeLineSkipped verifies that lines exceeding the 1 MiB cap
// are skipped (content not checked) instead of failing the scan, and the
// following line still gets its correct line number.
func TestScanForTyposHugeLineSkipped(t *testing.T) {
	dict := NewConcurrentDictionary(map[string]struct{}{"hello": {}})
	huge := strings.Repeat("a", maxLineLen+10) + "\nwrold\nother\n"
	typos, err := scanForTypos(strings.NewReader(huge), dict)
	if err != nil {
		t.Fatalf("scanForTypos: unexpected error, got %v", err)
	}
	if len(typos) != 2 {
		t.Fatalf("expected 2 typos on the following lines, got %d: %v", len(typos), typos)
	}
	if typos[0].LineNumber != 2 || typos[1].LineNumber != 3 {
		t.Errorf("expected typos on lines 2 and 3, got %d and %d", typos[0].LineNumber, typos[1].LineNumber)
	}
}

// TestLineReaderBasics verifies line iteration and line numbers on a simple input.
func TestLineReaderBasics(t *testing.T) {
	lr := newLineReader(strings.NewReader("one\ntwo\n"), maxLineLen)
	line, num, err := lr.Next()
	if err != nil || line != "one\n" || num != 1 {
		t.Fatalf("got (%q,%d,%v), want (\"one\\n\",1,nil)", line, num, err)
	}
	line, num, err = lr.Next()
	if err != nil || line != "two\n" || num != 2 {
		t.Fatalf("got (%q,%d,%v), want (\"two\\n\",2,nil)", line, num, err)
	}
	if _, _, err := lr.Next(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

// TestLineReaderNoTrailingNewline verifies the final unterminated line is returned.
func TestLineReaderNoTrailingNewline(t *testing.T) {
	lr := newLineReader(strings.NewReader("abc"), maxLineLen)
	line, num, err := lr.Next()
	if err != nil || line != "abc" || num != 1 {
		t.Fatalf("got (%q,%d,%v), want (\"abc\",1,nil)", line, num, err)
	}
	if _, _, err := lr.Next(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

// TestLineReaderOverlongSkippedButPreserved verifies over-long lines are not
// returned as checkable lines, the following line keeps its true number, and an
// installed overlong sink still receives the full content verbatim.
func TestLineReaderOverlongSkippedButPreserved(t *testing.T) {
	huge := strings.Repeat("q", maxLineLen+10)
	lr := newLineReader(strings.NewReader("ok\n"+huge+"\nend\n"), maxLineLen)
	var preserved strings.Builder
	lr.setOverlong(&preserved)

	line, num, err := lr.Next()
	if err != nil || line != "ok\n" || num != 1 {
		t.Fatalf("got (%q,%d,%v), want (\"ok\\n\",1,nil)", line, num, err)
	}
	line, num, err = lr.Next()
	if err != nil || line != "end\n" || num != 3 {
		t.Fatalf("got (%q,%d,%v), want (\"end\\n\",3,nil)", line, num, err)
	}
	if preserved.String() != huge+"\n" {
		t.Error("overlong content not preserved verbatim")
	}
}

func TestRunConcurrentCheckerWorkerError(t *testing.T) {
	mockDictionary := map[string]struct{}{"word": {}}
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "a.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	orig := checkFileFunc
	checkFileFunc = func(_ string, _ *ConcurrentDictionary) ([]MisspelledWord, error) {
		return nil, errors.New("injected worker failure")
	}
	defer func() { checkFileFunc = orig }()

	result, err := runConcurrentChecker(tempDir, mockDictionary, nil, false)
	if err == nil {
		t.Fatal("expected error from runConcurrentChecker, got nil")
	}
	if !strings.Contains(err.Error(), "injected worker failure") {
		t.Fatalf("expected injected failure in error, got %v", err)
	}
	// After the aggregation fix, successful results are still returned (empty
	// here because the only file failed) rather than nil.
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d: %v", len(result), result)
	}
}

// TestRunConcurrentCheckerAggregatesErrors verifies that when multiple files
// fail, the combined error references every failure (not just the first) and
// successful files still appear in the results map.
func TestRunConcurrentCheckerAggregatesErrors(t *testing.T) {
	mockDictionary := map[string]struct{}{"hello": {}, "world": {}}
	tempDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tempDir, "bad1.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatalf("write bad1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "bad2.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatalf("write bad2: %v", err)
	}
	goodPath := filepath.Join(tempDir, "good.txt")
	if err := os.WriteFile(goodPath, []byte("hello wrld\n"), 0644); err != nil {
		t.Fatalf("write good: %v", err)
	}

	orig := checkFileFunc
	checkFileFunc = func(path string, dict *ConcurrentDictionary) ([]MisspelledWord, error) {
		switch filepath.Base(path) {
		case "bad1.txt", "bad2.txt":
			return nil, fmt.Errorf("injected failure in %s", filepath.Base(path))
		default:
			return checkFile(path, dict)
		}
	}
	defer func() { checkFileFunc = orig }()

	result, err := runConcurrentChecker(tempDir, mockDictionary, nil, false)
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}
	// Both bad files should be referenced in the joined error message.
	msg := err.Error()
	if !strings.Contains(msg, "bad1.txt") || !strings.Contains(msg, "bad2.txt") {
		t.Errorf("expected error to mention both bad files, got: %s", msg)
	}
	// The good file's typo should still be in the results.
	if typos, ok := result[goodPath]; !ok || len(typos) != 1 {
		t.Errorf("expected 1 typo in good.txt, got %v", result[goodPath])
	}
}

func TestRunConcurrentCheckerWalkError(t *testing.T) {
	mockDictionary := map[string]struct{}{"word": {}}
	missingPath := filepath.Join(t.TempDir(), "does-not-exist")

	if _, err := runConcurrentChecker(missingPath, mockDictionary, nil, false); err == nil {
		t.Fatal("expected error from runConcurrentChecker when root missing")
	}
}

// TestCheckFileSkippedHugeLine verifies checkFile tolerates a file whose only
// line exceeds the 1 MiB cap: the line is skipped rather than failing the file.
func TestCheckFileSkippedHugeLine(t *testing.T) {
	mockDictionary := map[string]struct{}{"word": {}}
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "long.txt")

	hugeToken := strings.Repeat("b", maxLineLen+10)
	if err := os.WriteFile(filePath, []byte(hugeToken), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	concurrentDict := NewConcurrentDictionary(mockDictionary)
	typos, err := checkFile(filePath, concurrentDict)
	if err != nil {
		t.Fatalf("expected no error for over-long line, got %v", err)
	}
	if len(typos) != 0 {
		t.Fatalf("expected 0 typos, got %v", typos)
	}
}

func TestIsLikelyBinaryReadError(t *testing.T) {
	dir := t.TempDir()
	if _, err := isLikelyBinary(dir); err == nil {
		t.Fatal("expected error when checking directory")
	}
}

// TestScanForTyposSkipsIdentifierFragments verifies tokens split out of
// identifiers (adjacent to a digit or underscore) are not reported.
func TestScanForTyposSkipsIdentifierFragments(t *testing.T) {
	dict := NewConcurrentDictionary(map[string]struct{}{})
	// Mi03x_er -> Mi/x/er, 10px -> px, 3D -> D are identifiers/units; the two
	// spaced words are real prose (and misspelled relative to the empty dict).
	typos, err := scanForTypos(strings.NewReader("Mi03x_er 10px 3D hello wolrd\n"), dict)
	if err != nil {
		t.Fatalf("scanForTypos: %v", err)
	}
	if len(typos) != 2 {
		t.Fatalf("expected 2 typos (identifier fragments skipped), got %d: %v", len(typos), typos)
	}
	if typos[0].Word != "hello" || typos[1].Word != "wolrd" {
		t.Errorf("unexpected typos: %v", typos)
	}
}

// TestIsLikelyBinaryByExtension verifies known-binary extensions are skipped
// even when their content looks like text.
func TestIsLikelyBinaryByExtension(t *testing.T) {
	p := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(p, []byte("%PDF-1.4\nplain-looking text, no null bytes\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := isLikelyBinary(p)
	if err != nil {
		t.Fatalf("isLikelyBinary: %v", err)
	}
	if !got {
		t.Error("expected .pdf to be treated as binary")
	}
}

// TestCollectFilesDefaultExcludes verifies built-in excludes (.git, dependency
// stores, caches) are skipped even when the user passes no --exclude.
func TestCollectFilesDefaultExcludes(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("node_modules/pkg/index.js", "var x = 1;\n")
	write(".git/config", "[core]\n")
	write("docs/readme.txt", "hello world\n")
	write("note.txt", "hi there\n")

	files, err := collectFiles(dir, nil, false)
	if err != nil {
		t.Fatalf("collectFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files (node_modules and .git skipped), got %v", files)
	}
	for _, f := range files {
		if strings.Contains(f, "node_modules") || strings.Contains(filepath.Base(f), ".git") {
			t.Errorf("default exclude leaked: %s", f)
		}
	}
}

// TestScanLinesForTypos verifies the streaming scanner scanForTypos: blank
// lines skipped, in-dictionary words ignored, correct 1-based line and
// column numbers, and suggestions populated for a near-miss word.
func TestScanLinesForTypos(t *testing.T) {
	dict := NewConcurrentDictionary(map[string]struct{}{"hello": {}, "world": {}})
	input := "\nhello world\nhelllo\n\nhello qwerx\n"
	got, err := scanForTypos(strings.NewReader(input), dict)
	if err != nil {
		t.Fatalf("scanForTypos error: %v", err)
	}
	var gotHelllo, gotQwerx *MisspelledWord
	for i := range got {
		switch got[i].Word {
		case "helllo":
			gotHelllo = &got[i]
		case "qwerx":
			gotQwerx = &got[i]
		}
	}
	if gotHelllo == nil {
		t.Fatalf("expected misspelling %q, got %#v", "helllo", got)
	}
	if gotHelllo.LineNumber != 3 || gotHelllo.Column != 1 {
		t.Errorf("helllo at line/col %d/%d, want 3/1", gotHelllo.LineNumber, gotHelllo.Column)
	}
	found := false
	for _, s := range gotHelllo.Suggestions {
		if s == "hello" {
			found = true
		}
	}
	if !found {
		t.Errorf("helllo suggestions %q, want to include %q", gotHelllo.Suggestions, "hello")
	}
	if gotQwerx == nil {
		t.Fatalf("expected misspelling %q, got %#v", "qwerx", got)
	}
	if gotQwerx.LineNumber != 5 || gotQwerx.Column != 7 {
		t.Errorf("qwerx at line/col %d/%d, want 5/7", gotQwerx.LineNumber, gotQwerx.Column)
	}
	if len(got) != 2 {
		t.Errorf("scanForTypos returned %d typos, want 2", len(got))
	}
}
