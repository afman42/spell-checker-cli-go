package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateTextReport verifies the text report content and ordering.
func TestGenerateTextReport(t *testing.T) {
	results := map[string][]MisspelledWord{
		"test.txt": {
			{Word: "errror", LineNumber: 1, Column: 5, Suggestions: []string{"error"}},
		},
	}

	var buf bytes.Buffer
	_ = generateTextReport(&buf, results)
	output := buf.String()

	// Check for the exact, complete output line.
	expectedLine := `- Line 1, Col 5: "errror" appears to be a typo. Did you mean: error?`
	if !strings.Contains(output, expectedLine) {
		t.Errorf("Text report missing expected line.\nGOT:\n%s\nWANT (to contain):\n%s", output, expectedLine)
	}
}

// TestGenerateTextReportDeterminism verifies the text report emits files in
// sorted path order, not map-iteration order. Previously only HTML and JSON
// sorted; text output varied run-to-run.
func TestGenerateTextReportDeterminism(t *testing.T) {
	results := map[string][]MisspelledWord{
		"zebra.txt": {{Word: "wrld", LineNumber: 1, Column: 1, Suggestions: []string{"world"}}},
		"alpha.txt": {{Word: "eror", LineNumber: 1, Column: 1, Suggestions: []string{"error"}}},
		"mid.txt":   {{Word: "helo", LineNumber: 1, Column: 1, Suggestions: []string{"hello"}}},
	}
	var buf bytes.Buffer
	_ = generateTextReport(&buf, results)
	out := buf.String()
	// alpha.txt must appear before mid.txt, which must appear before zebra.txt.
	iAlpha := strings.Index(out, "alpha.txt")
	iMid := strings.Index(out, "mid.txt")
	iZebra := strings.Index(out, "zebra.txt")
	if iAlpha < 0 || iMid < 0 || iZebra < 0 {
		t.Fatalf("missing file headers in output:\n%s", out)
	}
	if iAlpha >= iMid || iMid >= iZebra {
		t.Errorf("text report not sorted: alpha=%d mid=%d zebra=%d\n%s", iAlpha, iMid, iZebra, out)
	}
}

// TestFormatTypoLine verifies the shared formatting helper used by both the
// text reporter and the watcher. It must append "Did you mean: ...?" only when
// suggestions exist, and must use the provided (possibly colored) strings.
func TestFormatTypoLine(t *testing.T) {
	withSuggest := MisspelledWord{Word: "wrld", LineNumber: 2, Column: 5, Suggestions: []string{"world", "wild"}}
	got := formatTypoLine(withSuggest, "", "")
	want := `Line 2, Col 5: "wrld" appears to be a typo. Did you mean: world, wild?`
	if got != want {
		t.Errorf("with suggestions: got %q, want %q", got, want)
	}

	noSuggest := MisspelledWord{Word: "zzzz", LineNumber: 3, Column: 2, Suggestions: nil}
	got = formatTypoLine(noSuggest, "", "")
	want = `Line 3, Col 2: "zzzz" appears to be a typo.`
	if got != want {
		t.Errorf("no suggestions: got %q, want %q", got, want)
	}

	// Explicit overrides (for ANSI color pass-through). Use the same ANSI
	// constants as the reporter so both sides agree on the exact bytes.
	coloredWord := ansiBold + ansiRed + "wrld" + ansiReset
	coloredSugg := ansiGreen + "world" + ansiReset
	got = formatTypoLine(withSuggest, coloredWord, coloredSugg)
	want = `Line 2, Col 5: "` + coloredWord + `" appears to be a typo. Did you mean: ` + coloredSugg + `?`
	if got != want {
		t.Errorf("colored override: got %q, want %q", got, want)
	}
}

func TestGenerateHTMLReport(t *testing.T) {
	results := map[string][]MisspelledWord{
		"test.txt": {
			{Word: "wrod", LineNumber: 2, Column: 10, Suggestions: []string{"world"}},
		},
	}

	var buf bytes.Buffer
	_ = generateHTMLReport(&buf, results)
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
	_ = generateTextReport(&textBuf, results)

	if !strings.Contains(textBuf.String(), "No typos found.") {
		t.Error("Text report for no typos is incorrect")
	}

	var htmlBuf bytes.Buffer
	_ = generateHTMLReport(&htmlBuf, results)
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
	_ = generateHTMLReport(&buf, results)
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
	_ = generateTextReport(&buf, results)
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

// TestRelLink verifies relative URL computation between report files in the
// same directory, across subdirectories, and from the root. The result must
// be the minimal relative path (e.g. "../d.html" not "../../a/d.html").
func TestRelLink(t *testing.T) {
	cases := []struct {
		name    string
		current string
		target  string
		want    string
	}{
		{"same root directory", "a.html", "index.html", "index.html"},
		{"current in subdir, target at root", "src/main.go.html", "index.html", "../index.html"},
		{"current nested two levels", "a/b/c.html", "index.html", "../../index.html"},
		{"both in same subdir", "src/a.html", "src/b.html", "b.html"},
		{"current at root, target in subdir", "index.html", "src/a.html", "src/a.html"},
		// Minimal path: the old implementation always climbed to root, producing
		// "../../a/d.html" instead of the correct "../d.html".
		{"sibling subdir minimal path", "a/b/c.html", "a/d.html", "../d.html"},
		{"cousin subdir minimal path", "a/b/x.html", "c/d/y.html", "../../c/d/y.html"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := relLink(tc.current, tc.target); got != tc.want {
				t.Errorf("relLink(%q, %q) = %q, want %q", tc.current, tc.target, got, tc.want)
			}
		})
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

// failingWriter always fails, exercising the report generators' error paths.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("injected write failure") }

// TestGenerateTextReportWriteError verifies a mid-write failure is returned,
// not silently dropped (previously text reports exited 0 on a truncated
// write, e.g. disk full).
func TestGenerateTextReportWriteError(t *testing.T) {
	results := map[string][]MisspelledWord{
		"test.txt": {{Word: "errror", LineNumber: 1, Column: 5}},
	}
	if err := generateTextReport(failingWriter{}, results); err == nil {
		t.Fatal("expected write error, got nil")
	}
}

// TestGenerateHTMLReportWriteError verifies the same contract for HTML.
func TestGenerateHTMLReportWriteError(t *testing.T) {
	results := map[string][]MisspelledWord{
		"test.txt": {{Word: "errror", LineNumber: 1, Column: 5}},
	}
	if err := generateHTMLReport(failingWriter{}, results); err == nil {
		t.Fatal("expected write error, got nil")
	}
}
