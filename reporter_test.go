package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerateTextReport(t *testing.T) {
	// Create sample results
	results := map[string][]MisspelledWord{
		"test.txt": {
			{Word: "errror", LineNumber: 1, Column: 5},
		},
	}

	var buf bytes.Buffer
	generateTextReport(&buf, results)

	output := buf.String()
	if !strings.Contains(output, "Typos found:") {
		t.Error("Text report missing 'Typos found:' header")
	}
	if !strings.Contains(output, "errror") {
		t.Error("Text report missing the misspelled word")
	}
	if !strings.Contains(output, "Line 1, Col 5") {
		t.Error("Text report missing line and column numbers")
	}
}

func TestGenerateHTMLReport(t *testing.T) {
	results := map[string][]MisspelledWord{
		"test.txt": {
			{Word: "wrod", LineNumber: 2, Column: 10},
		},
	}

	var buf bytes.Buffer
	generateHTMLReport(&buf, results)

	output := buf.String()
	if !strings.Contains(output, "<html") {
		t.Error("HTML report missing <html> tag")
	}
	if !strings.Contains(output, "<table>") {
		t.Error("HTML report missing <table> tag")
	}
	if !strings.Contains(output, "<td>wrod</td>") {
		t.Error("HTML report missing the misspelled word in a <td> tag")
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
