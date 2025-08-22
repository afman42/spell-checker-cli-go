package main

import (
	"bytes"
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
	if !strings.Contains(output, "<td>world</td>") {
		t.Error("HTML report missing suggestion in a <td> tag")
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
