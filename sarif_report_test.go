package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestGenerateSARIFReport verifies the SARIF v2.1.0 shape: top-level version,
// one run with a named driver and a "spellcheck/typo" rule, one result per
// typo with location (startLine/startColumn), message, and a stable primary
// fingerprint. Files sorted for deterministic output.
func TestGenerateSARIFReport(t *testing.T) {
	results := map[string][]MisspelledWord{
		"b.txt": {{Word: "wrld", LineNumber: 1, Column: 7, Suggestions: []string{"world"}}},
		"a.txt": {{Word: "zzzz", LineNumber: 2, Column: 1, Suggestions: nil}},
	}

	var buf bytes.Buffer
	if err := generateSARIFReport(&buf, results); err != nil {
		t.Fatalf("generateSARIFReport: %v", err)
	}

	var report sarifReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal SARIF: %v", err)
	}

	if report.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", report.Version)
	}
	if report.Schema == "" {
		t.Error("schema is empty")
	}
	if len(report.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(report.Runs))
	}
	run := report.Runs[0]
	if run.Tool.Driver.Name == "" || run.Tool.Driver.InformationURI == "" {
		t.Errorf("driver name/URI empty: %+v", run.Tool.Driver)
	}
	// One reporting rule, "spellcheck/typo".
	if len(run.Tool.Driver.Rules) != 1 || run.Tool.Driver.Rules[0].ID != "spellcheck/typo" {
		t.Errorf("rules = %+v, want single spellcheck/typo rule", run.Tool.Driver.Rules)
	}

	// Results sorted by path: a.txt then b.txt.
	if len(run.Results) != len(results) {
		t.Fatalf("results = %d, want %d", len(run.Results), len(results))
	}
	a, b := run.Results[0], run.Results[1]
	if got := a.Locations[0].PhysicalLocation.ArtifactLocation.URI; got != "a.txt" {
		t.Errorf("first result URI = %q, want a.txt (sorted)", got)
	}
	if a.Level != "warning" || a.RuleID != "spellcheck/typo" {
		t.Errorf("first result level/rule = %q/%q", a.Level, a.RuleID)
	}
	if a.Locations[0].PhysicalLocation.Region.StartLine != 2 || a.Locations[0].PhysicalLocation.Region.StartColumn != 1 {
		t.Errorf("a.txt location = line %d col %d, want 2/1",
			a.Locations[0].PhysicalLocation.Region.StartLine, a.Locations[0].PhysicalLocation.Region.StartColumn)
	}
	if a.PartialFingerprints["primary"] != "a.txt:2:zzzz" {
		t.Errorf("fingerprint = %q, want a.txt:2:zzzz", a.PartialFingerprints["primary"])
	}
	if got := b.Locations[0].PhysicalLocation.ArtifactLocation.URI; got != "b.txt" {
		t.Errorf("second result URI = %q, want b.txt", got)
	}
	// Suggestions are joined into the message.
	if b.Message.Text == "" || b.Message.Text == `"wrld" appears to be a typo.` {
		t.Errorf("message = %q, want typo message with suggestions", b.Message.Text)
	}
}

// TestGenerateSARIFReportEmpty ensures an empty scan emits a valid SARIF log
// with no results (not an error and not a null runs/results array).
func TestGenerateSARIFReportEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := generateSARIFReport(&buf, map[string][]MisspelledWord{}); err != nil {
		t.Fatalf("generateSARIFReport: %v", err)
	}
	var report sarifReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(report.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(report.Runs))
	}
	if len(report.Runs[0].Results) != 0 {
		t.Errorf("results = %d, want 0", len(report.Runs[0].Results))
	}
	// GitHub Code Scanning rejects "results": null — it must be an array.
	// Verify the raw JSON contains [] not null for the results field.
	raw := buf.String()
	if !strings.Contains(raw, `"results": []`) {
		t.Errorf("SARIF results not emitted as []: %s", raw)
	}
}
