package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// --- Shared constants for reusable HTML parts ---
const htmlHeader = `<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><title>Spell Check Report</title>
<style>body{font-family:sans-serif;max-width:960px;margin:20px auto}h1,h2{border-bottom:2px solid #eee}table{width:100%;border-collapse:collapse}th,td{padding:12px;border:1px solid #ddd}th{background-color:#3498db;color:white}td:nth-child(3){font-weight:bold;color:#c0392b}td:nth-child(4){color:#27ae60}ul{list-style-type:none;padding:0}li{padding:8px;border-bottom:1px solid #eee}a{text-decoration:none;color:#3498db}</style>
</head><body>`

const htmlFooter = "</body></html>"

// --- NEW: Multi-file HTML report generator ---

// generateMultiFileHTMLReport creates a directory with an index.html and separate reports.
func generateMultiFileHTMLReport(outputDir string, results map[string][]MisspelledWord) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("could not create output directory %s: %w", outputDir, err)
	}

	if err := generateIndexFile(outputDir, results); err != nil {
		return err
	}

	for file, words := range results {
		if err := generateSingleReportFile(outputDir, file, words); err != nil {
			return err
		}
	}
	fmt.Printf("Successfully generated %d report files in %s\n", len(results)+1, outputDir)
	return nil
}

// sanitizePath converts a file path into a safe filename for a report.
func sanitizePath(path string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_")
	return replacer.Replace(path) + ".html"
}

// generateIndexFile creates the main summary/index page with links.
func generateIndexFile(outputDir string, results map[string][]MisspelledWord) error {
	indexPath := filepath.Join(outputDir, "index.html")
	file, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("could not create index file: %w", err)
	}
	defer file.Close()

	fmt.Fprint(file, htmlHeader)
	fmt.Fprint(file, "<h1>Spell Check Summary</h1>")

	if len(results) == 0 {
		fmt.Fprint(file, `<p>✅ No typos found.</p>`)
	} else {
		fmt.Fprint(file, "<ul>")
		for path, words := range results {
			reportFileName := sanitizePath(path)
			fmt.Fprintf(file, `<li><a href="%s">%s</a> (%d typos)</li>`, reportFileName, path, len(words))
		}
		fmt.Fprint(file, "</ul>")
	}

	fmt.Fprint(file, htmlFooter)
	return nil
}

// generateSingleReportFile creates a detailed HTML report for one source file.
func generateSingleReportFile(outputDir, filePath string, words []MisspelledWord) error {
	reportFileName := sanitizePath(filePath)
	reportPath := filepath.Join(outputDir, reportFileName)
	file, err := os.Create(reportPath)
	if err != nil {
		return fmt.Errorf("could not create report file for %s: %w", filePath, err)
	}
	defer file.Close()

	fmt.Fprint(file, htmlHeader)
	writeFileReportTable(file, filePath, words)
	fmt.Fprint(file, htmlFooter)
	return nil
}

// --- REFACTORED: Original functions now use helpers ---

// generateHTMLReport generates a single, self-contained HTML report.
func generateHTMLReport(writer io.Writer, results map[string][]MisspelledWord) {
	fmt.Fprint(writer, htmlHeader)
	fmt.Fprint(writer, "<h1>Spell Check Report</h1>")

	if len(results) == 0 {
		fmt.Fprint(writer, `<p>✅ No typos found.</p>`)
	} else {
		for file, words := range results {
			writeFileReportTable(writer, file, words)
		}
	}
	fmt.Fprint(writer, htmlFooter)
}

// writeFileReportTable is a shared helper that writes the H2 and table for a file's results.
func writeFileReportTable(writer io.Writer, file string, words []MisspelledWord) {
	fmt.Fprintf(writer, `<h2>Typos in: %s</h2>`, filepath.Base(file))
	fmt.Fprint(writer, `<table><tr><th>Line</th><th>Column</th><th>Word</th><th>Suggestions</th></tr>`)
	for _, m := range words {
		suggestionsStr := strings.Join(m.Suggestions, ", ")
		fmt.Fprintf(writer, "<tr><td>%d</td><td>%d</td><td>%s</td><td>%s</td></tr>", m.LineNumber, m.Column, m.Word, suggestionsStr)
	}
	fmt.Fprint(writer, `</table>`)
}

// --- Text report generator remains unchanged ---
func generateTextReport(writer io.Writer, results map[string][]MisspelledWord) {
	if len(results) == 0 {
		fmt.Fprintln(writer, "No typos found.")
		return
	}
	fmt.Fprintln(writer, "Typos found:")
	for file, words := range results {
		fmt.Fprintf(writer, "\n--- In file %s ---\n", file)
		for _, m := range words {
			baseMessage := fmt.Sprintf("- Line %d, Col %d: \"%s\" appears to be a typo.", m.LineNumber, m.Column, m.Word)
			if len(m.Suggestions) > 0 {
				suggestionsStr := strings.Join(m.Suggestions, ", ")
				fmt.Fprintf(writer, "%s Did you mean: %s?\n", baseMessage, suggestionsStr)
			} else {
				fmt.Fprintln(writer, baseMessage)
			}
		}
	}
}
