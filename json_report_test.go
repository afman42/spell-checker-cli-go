package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestGenerateJSONReport verifies the structured JSON shape, summary counts,
// deterministic file ordering, and that empty suggestions serialize as [].
func TestGenerateJSONReport(t *testing.T) {
	results := map[string][]MisspelledWord{
		"b.txt": {
			{Word: "wrld", LineNumber: 1, Column: 7, Suggestions: []string{"world"}},
		},
		"a.txt": {
			{Word: "zzzz", LineNumber: 2, Column: 1, Suggestions: nil},
		},
	}

	var buf bytes.Buffer
	if err := generateJSONReport(&buf, results); err != nil {
		t.Fatalf("generateJSONReport error: %v", err)
	}

	var report jsonReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	if report.Summary.Files != 2 {
		t.Errorf("summary.files = %d, want 2", report.Summary.Files)
	}
	if report.Summary.Typos != 2 {
		t.Errorf("summary.typos = %d, want 2", report.Summary.Typos)
	}
	if report.Summary.Suggestions != 1 {
		t.Errorf("summary.suggestions = %d, want 1", report.Summary.Suggestions)
	}

	// Files must be sorted by path: a.txt before b.txt.
	if len(report.Files) != 2 || report.Files[0].File != "a.txt" || report.Files[1].File != "b.txt" {
		t.Fatalf("files not sorted as expected: %+v", report.Files)
	}

	// Empty suggestions must be [] not null.
	if report.Files[0].Typos[0].Suggestions == nil {
		t.Error("expected empty suggestions to serialize as [], got null")
	}
	if !strings.Contains(buf.String(), `"suggestions": []`) {
		t.Errorf("expected '\"suggestions\": []' in output:\n%s", buf.String())
	}
}

// TestGenerateJSONReportEmpty verifies a clean run yields zeroed summary and an
// empty (non-null) files array.
func TestGenerateJSONReportEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := generateJSONReport(&buf, map[string][]MisspelledWord{}); err != nil {
		t.Fatalf("error: %v", err)
	}
	var report jsonReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if report.Summary.Typos != 0 || report.Summary.Files != 0 {
		t.Errorf("expected zero summary, got %+v", report.Summary)
	}
	if report.Files == nil {
		t.Error("expected files to be [] not null")
	}
}

// TestGenerateJSONReportNoHTMLEscape ensures characters like < > & are not
// escaped into \u003c sequences (SetEscapeHTML(false)).
func TestGenerateJSONReportNoHTMLEscape(t *testing.T) {
	results := map[string][]MisspelledWord{
		"x.txt": {{Word: "a<b>&c", LineNumber: 1, Column: 1, Suggestions: []string{"a&b"}}},
	}
	var buf bytes.Buffer
	if err := generateJSONReport(&buf, results); err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.Contains(buf.String(), `\u003c`) {
		t.Errorf("expected raw '<' not unicode-escaped:\n%s", buf.String())
	}
}
