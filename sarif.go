package main

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

// SARIF (Static Analysis Results Interchange Format) is the standard format
// ingested by GitHub Code Scanning.  The schema below follows SARIF v2.1.0
// (the version GitHub expects).  Only the fields we populate are kept; the
// rest are omitted so the output stays compact and parseable.

// sarifReport is the top-level SARIF log.
type sarifReport struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string               `json:"name"`
	Version        string               `json:"version"`
	InformationURI string               `json:"informationUri"`
	Rules          []sarifReportingRule `json:"rules,omitempty"`
}

type sarifReportingRule struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	ShortDescription sarifMessage `json:"shortDescription"`
	HelpURI          string       `json:"helpUri,omitempty"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level"`
	Message             sarifMessage      `json:"message"`
	Locations           []sarifLocation   `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
}

// generateSARIFReport writes a SARIF v2.1.0 report. Files are sorted by path
// for deterministic output (same contract as generateJSONReport).  Each typo
// becomes one result, level "warning", rule "spellcheck/typo".
func generateSARIFReport(writer io.Writer, results map[string][]MisspelledWord) error {
	paths := sortedResultPaths(results)

	sarifResults := []sarifResult{}
	for _, p := range paths {
		for _, m := range results[p] {
			msg := typoMessage(m.Word, strings.Join(m.Suggestions, ", "))
			sarifResults = append(sarifResults, sarifResult{
				RuleID:  "spellcheck/typo",
				Level:   "warning",
				Message: sarifMessage{Text: msg},
				Locations: []sarifLocation{
					{
						PhysicalLocation: sarifPhysicalLocation{
							ArtifactLocation: sarifArtifactLocation{URI: p},
							Region: sarifRegion{
								StartLine:   m.LineNumber,
								StartColumn: m.Column,
							},
						},
					},
				},
				// Low-cost, stable fingerprint: file + line + word. Lets GitHub
				// de-duplicate the same finding across runs and annotate the
				// right line in a PR review.
				PartialFingerprints: map[string]string{
					"primary": p + ":" + strconv.Itoa(m.LineNumber) + ":" + m.Word,
				},
			})
		}
	}

	report := sarifReport{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{
				Driver: sarifDriver{
					Name:           "spell-checker-cli",
					Version:        versionString,
					InformationURI: "https://github.com/afman42/spell-checker-cli-go",
					Rules: []sarifReportingRule{{
						ID:               "spellcheck/typo",
						Name:             "Typo",
						ShortDescription: sarifMessage{Text: "A word not found in the dictionary."},
					}},
				},
			},
			Results: sarifResults,
		}},
	}

	enc := json.NewEncoder(writer)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(report)
}
