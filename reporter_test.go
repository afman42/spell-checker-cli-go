package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// IMPROVED: Assertions are more specific.
func TestGenerateTextReport(t *testing.T) {
	results := map[string][]MisspelledWord{
		"test.txt": {
			{Word: "errror", LineNumber: 1, Column: 5, Suggestions: []string{"error"}},
		},
	}

	var buf bytes.Buffer
	generateTextReport(&buf, results)
	output := buf.String()

	// Check for the exact, complete output line.
	expectedLine := `- Line 1, Col 5: "errror" appears to be a typo. Did you mean: error?`
	if !strings.Contains(output, expectedLine) {
		t.Errorf("Text report missing expected line.\nGOT:\n%s\nWANT (to contain):\n%s", output, expectedLine)
	}
}

// IMPROVED: Check for the new "Suggestions" table header.
func TestGenerateHTMLReport(t *testing.T) {
	results := map[string][]MisspelledWord{
		"test.txt": {
			{Word: "wrod", LineNumber: 2, Column: 10, Suggestions: []string{"world"}},
		},
	}

	var buf bytes.Buffer
	generateHTMLReport(&buf, results)
	output := buf.String()

	if !strings.Contains(output, "<th>Suggestions</th>") {
		t.Error("HTML report missing 'Suggestions' table header")
	}
	if !strings.Contains(output, ">world</td>") {
		t.Error("HTML report missing suggestion in a <td> tag")
	}
	if !strings.Contains(output, "Typos Found") {
		t.Error("HTML report missing stats bar")
	}
}

func TestGenerateReportNoTypos(t *testing.T) {
	results := make(map[string][]MisspelledWord)
	var textBuf bytes.Buffer
	generateTextReport(&textBuf, results)

	if !strings.Contains(textBuf.String(), "No typos found.") {
		t.Error("Text report for no typos is incorrect")
	}

	var htmlBuf bytes.Buffer
	generateHTMLReport(&htmlBuf, results)
	if !strings.Contains(htmlBuf.String(), "No typos found.") {
		t.Error("HTML report for no typos is incorrect")
	}
}

// TestGenerateHTMLReportEscaping ensures user-controlled content is HTML-escaped
// to prevent injection / XSS in generated reports.
func TestGenerateHTMLReportEscaping(t *testing.T) {
	results := map[string][]MisspelledWord{
		"test.txt": {
			{
				Word:        "<script>",
				LineNumber:  1,
				Column:      1,
				Suggestions: []string{"<b>bold</b>"},
			},
		},
	}

	var buf bytes.Buffer
	generateHTMLReport(&buf, results)
	output := buf.String()

	if strings.Contains(output, "<script>") {
		t.Error("raw <script> leaked into HTML output (XSS risk)")
	}
	if !strings.Contains(output, "&lt;script&gt;") {
		t.Error("expected escaped word in HTML output")
	}
	if strings.Contains(output, "<b>bold</b>") {
		t.Error("raw suggestion HTML leaked into output")
	}
	if !strings.Contains(output, "&lt;b&gt;bold&lt;/b&gt;") {
		t.Error("expected escaped suggestion in HTML output")
	}
}

// TestGenerateTextReportNoSuggestions ensures the line omits "Did you mean"
// when there are no suggestions.
func TestGenerateTextReportNoSuggestions(t *testing.T) {
	results := map[string][]MisspelledWord{
		"a.txt": {
			{Word: "zzzz", LineNumber: 3, Column: 2, Suggestions: nil},
		},
	}
	var buf bytes.Buffer
	generateTextReport(&buf, results)
	out := buf.String()

	if !strings.Contains(out, `- Line 3, Col 2: "zzzz" appears to be a typo.`) {
		t.Errorf("missing base typo line.\nGOT:\n%s", out)
	}
	if strings.Contains(out, "Did you mean") {
		t.Errorf("did not expect a suggestion prompt.\nGOT:\n%s", out)
	}
}

// TestSafeReportPath verifies the new path-safe function preserves directory
// structure and handles unsafe characters.
func TestSafeReportPath(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"src/main.go", "src/main.go.html", false},
		{"a/b/c.txt", "a/b/c.txt.html", false},
		{"single.txt", "single.txt.html", false},
		{"weird:name*.txt", "weird_name_.txt.html", false},
		{"path/with spaces.txt", "path/with spaces.txt.html", false},
	}
	for _, tc := range cases {
		got, err := safeReportPath(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("safeReportPath(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("safeReportPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestGenerateMultiFileHTMLReport verifies an index plus one page per file are written.
func TestGenerateMultiFileHTMLReport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports")
	results := map[string][]MisspelledWord{
		"a.txt": {{Word: "wrld", LineNumber: 1, Column: 1, Suggestions: []string{"world"}}},
		"b.txt": {{Word: "eror", LineNumber: 2, Column: 1, Suggestions: []string{"error"}}},
	}

	if err := generateMultiFileHTMLReport(dir, results); err != nil {
		t.Fatalf("generateMultiFileHTMLReport error: %v", err)
	}

	// index + one file per source = 3 files
	aPath, err := safeReportPath("a.txt")
	if err != nil {
		t.Fatalf("safeReportPath error: %v", err)
	}
	bPath, err := safeReportPath("b.txt")
	if err != nil {
		t.Fatalf("safeReportPath error: %v", err)
	}
	mustExist := []string{
		filepath.Join(dir, "index.html"),
		filepath.Join(dir, aPath),
		filepath.Join(dir, bPath),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected report file %q to exist: %v", p, err)
		}
	}

	index, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(string(index), "Spell Check Summary") {
		t.Error("index.html missing summary heading")
	}
}

// TestGenerateMultiFileHTMLReportCollision verifies dedup when multiple source paths
// sanitize to the same filename.
func TestGenerateMultiFileHTMLReportCollision(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports")
	//
	// With subdirectory-based safeReportPath:
	//   "src/main.go"  → "src/main.go.html"    (unique, in src/ subdir)
	//   "src:main.go"  → "src_main.go.html"    (: replaced with _)
	//   "src_main.go"  → "src_main.go.html"    (collision with above!)
	//
	// After dedup (sorted by path: src/main.go < src:main.go < src_main.go):
	//   src/main.go  → src/main.go.html      (unique path, no collision)
	//   src:main.go  → src_main.go.html       (first, no suffix)
	//   src_main.go  → src_main.go_1.html     (collision, _1 suffix)
	results := map[string][]MisspelledWord{
		"src/main.go": {{Word: "wrld", LineNumber: 1, Column: 1, Suggestions: []string{"world"}}},
		"src:main.go": {{Word: "eror", LineNumber: 2, Column: 1, Suggestions: []string{"error"}}},
		"src_main.go": {{Word: "helo", LineNumber: 3, Column: 1, Suggestions: []string{"hello"}}},
	}

	if err := generateMultiFileHTMLReport(dir, results); err != nil {
		t.Fatalf("generateMultiFileHTMLReport error: %v", err)
	}

	mustExist := []string{
		filepath.Join(dir, "index.html"),
		filepath.Join(dir, "src/main.go.html"),
		filepath.Join(dir, "src_main.go.html"),
		filepath.Join(dir, "src_main.go_1.html"),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected report file %q to exist: %v", p, err)
		}
	}

	// Verify no extra files beyond index + 3 deduped reports.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 4 {
		t.Errorf("expected 4 files in report dir, got %d", len(entries))
	}

	// Verify the index hrefs point to the deduped filenames.
	index, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	for _, expected := range []string{"src/main.go.html", "src_main.go.html", "src_main.go_1.html"} {
		if !strings.Contains(string(index), expected) {
			t.Errorf("index.html missing link to %s", expected)
		}
	}
}

// TestGenerateMultiFileHTMLReportEmpty verifies the index reports no typos cleanly.
func TestGenerateMultiFileHTMLReportEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports")
	if err := generateMultiFileHTMLReport(dir, map[string][]MisspelledWord{}); err != nil {
		t.Fatalf("error: %v", err)
	}
	index, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(string(index), "No typos found") {
		t.Error("expected 'No typos found' in empty index")
	}
}
