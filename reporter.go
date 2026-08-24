package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ANSI color codes for terminal output.
const (
	ansiReset = "\033[0m"
	ansiRed   = "\033[31m"
	ansiGreen = "\033[32m"
	ansiBold  = "\033[1m"
)

// styleCSS is the stylesheet shared by the single-file and multi-file HTML
// reports. Embedded at compile time so the binary stays self-contained.
//
//go:embed style.css
var styleCSS string

// htmlDocType is the reusable HTML document prefix (DOCTYPE, head, opening
// body/container tags). The CSS is injected from the embedded style.css.
var htmlDocType = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Spell Check Report</title>
<style>
` + styleCSS + `
</style>
</head>
<body>
<div class="container">`

const htmlFooter = "\n</div>\n</body>\n</html>"

// summarizeStats computes total counts from all results.
func summarizeStats(results CheckResults) (totalFiles, totalTypos, totalSuggestions int) {
	totalFiles = len(results)
	for _, words := range results {
		totalTypos += len(words)
		for _, w := range words {
			totalSuggestions += len(w.Suggestions)
		}
	}
	return
}

// fileEntry pairs a source file path with its typos and deduplicated report filename.
type fileEntry struct {
	path     string
	words    []MisspelledWord
	filename string
}

// --- Multi-file HTML report generator ---

// generateMultiFileHTMLReport creates a directory with an index.html and separate reports.
func generateMultiFileHTMLReport(outputDir string, results CheckResults) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("could not create output directory %s: %w", outputDir, err)
	}

	// Build entries with safe relative report paths.
	entries := make([]fileEntry, 0, len(results))
	usedNames := make(map[string]int)
	seen := make(map[string]struct{}) // detect duplicates in input

	for path, words := range results {
		if _, dup := seen[path]; dup {
			continue
		}
		seen[path] = struct{}{}
		entries = append(entries, fileEntry{path: path, words: words})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })

	for i, entry := range entries {
		base, err := safeReportPath(entry.path)
		if err != nil {
			return fmt.Errorf("invalid path %q: %w", entry.path, err)
		}
		if n, ok := usedNames[base]; ok {
			ext := filepath.Ext(base)
			origBase := base
			base = strings.TrimSuffix(base, ext) + fmt.Sprintf("_%d", n) + ext
			usedNames[origBase] = n + 1
		} else {
			usedNames[base] = 1
		}
		entries[i].filename = base
	}

	if err := generateIndexFile(outputDir, entries, results); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := generateSingleReportFile(outputDir, entry.filename, entry.path, entry.words, entries); err != nil {
			return err
		}
	}
	fmt.Printf("Successfully generated %d report files in %s\n", len(entries)+1, outputDir)
	return nil
}

// safeReportPath converts a source file path into a safe relative path
// for the report file, preserving directory structure.
func safeReportPath(path string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(path))
	// Remove leading slash (absolute paths on Unix)
	clean = strings.TrimLeft(clean, "/")
	// On Windows, remove drive letter prefix like "C:/"
	if len(clean) >= 2 && clean[1] == ':' {
		clean = clean[2:]
	}
	clean = strings.TrimLeft(clean, "/")
	if clean == "" || clean == "." {
		clean = "root"
	}
	// Check each path component for traversal (e.g. ".."), not just substring
	// (a file named "foo..txt" is safe).
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", fmt.Errorf("path may not contain '..': %s", path)
		}
	}
	// Replace characters unsafe in filenames (except / which preserves structure)
	replacer := strings.NewReplacer(
		":", "_",
		"<", "(",
		">", ")",
		"|", "_",
		"?", "_",
		"*", "_",
		"\"", "'",
	)
	safe := replacer.Replace(clean)
	return safe + ".html", nil
}

// generateIndexFile creates the main summary/index page with links.
func generateIndexFile(outputDir string, entries []fileEntry, results CheckResults) error {
	indexPath := filepath.Join(outputDir, "index.html")
	file, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("could not create index file: %w", err)
	}
	defer file.Close()
	w := &errWriter{w: file}

	totalFiles, totalTypos, totalSuggestions := summarizeStats(results)

	fmt.Fprint(w, htmlDocType)
	writeMetaHeader(w, "Spell Check Summary")
	writeStatsBar(w, totalFiles, totalTypos, totalSuggestions)

	if len(entries) == 0 {
		fmt.Fprint(w, `<div class="empty-state"><div class="icon">✅</div><p>No typos found.</p></div>`)
	} else {
		fmt.Fprint(w, `<ul class="file-list">`)
		for _, entry := range entries {
			count := len(entry.words)
			var label, clsLabel string
			if count > 1 {
				label = fmt.Sprintf("%d typos", count)
				clsLabel = "error"
			} else if count == 1 {
				label = "1 typo"
				clsLabel = "error"
			} else {
				label = "clean"
				clsLabel = "clean"
			}
			fmt.Fprintf(w, `<li class="file-item"><a href="%s">%s</a> <span class="typo-count %s">%s</span></li>`,
				html.EscapeString(entry.filename), html.EscapeString(entry.path), clsLabel, label)
		}
		fmt.Fprint(w, `</ul>`)
	}

	fmt.Fprint(w, htmlFooter)
	return w.err
}

// generateSingleReportFile creates a detailed HTML report for one source file.
func generateSingleReportFile(outputDir, filename, filePath string, words []MisspelledWord, allEntries []fileEntry) error {
	reportPath := filepath.Join(outputDir, filename)
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		return fmt.Errorf("could not create directories for %s: %w", filePath, err)
	}
	file, err := os.Create(reportPath)
	if err != nil {
		return fmt.Errorf("could not create report file for %s: %w", filePath, err)
	}
	defer file.Close()
	w := &errWriter{w: file}

	fmt.Fprint(w, htmlDocType)

	// Back to index link — compute correct relative path from subdirectories
	backLink := relLink(filename, "index.html")
	fmt.Fprintf(w, `<a class="nav-link" href="%s">← Back to Summary</a>`, html.EscapeString(backLink))

	// Heading
	fmt.Fprintf(w, `<h1>%s</h1>`, html.EscapeString(filePath))
	fmt.Fprintf(w, `<div class="meta">%d typo(s) found</div>`, len(words))

	// Navigation between files
	if len(allEntries) > 1 {
		var prevLink, nextLink string
		for i, e := range allEntries {
			if e.path == filePath {
				if i > 0 {
					rel := relLink(filename, allEntries[i-1].filename)
					prevLink = fmt.Sprintf(`<a class="nav-link" href="%s">← %s</a>`, html.EscapeString(rel), html.EscapeString(filepath.Base(allEntries[i-1].path)))
				}
				if i < len(allEntries)-1 {
					rel := relLink(filename, allEntries[i+1].filename)
					nextLink = fmt.Sprintf(`<a class="nav-link" href="%s" style="margin-left:auto">%s →</a>`, html.EscapeString(rel), html.EscapeString(filepath.Base(allEntries[i+1].path)))
				}
				break
			}
		}
		if prevLink != "" || nextLink != "" {
			fmt.Fprintf(w, `<div style="display:flex;gap:16px;margin-bottom:8px">%s %s</div>`, prevLink, nextLink)
		}
	}

	// Table
	if len(words) > 0 {
		fmt.Fprint(w, `<table><tr><th>Line</th><th>Col</th><th>Word</th><th>Suggestions</th></tr>`)
		for _, m := range words {
			writeTypoRow(w, m)
		}
		fmt.Fprint(w, `</table>`)
	} else {
		fmt.Fprint(w, `<div class="empty-state"><div class="icon">✅</div><p>No typos found in this file.</p></div>`)
	}

	fmt.Fprint(w, htmlFooter)
	return w.err
}

// relLink computes a relative URL from the current report filename to a target
// filename. Both filenames use forward slashes (as produced by safeReportPath).
// The result is the minimal relative path (e.g. "../d.html", not
// "../../a/d.html") so multi-file report navigation stays clean.
func relLink(current, target string) string {
	curDir := filepath.ToSlash(filepath.Dir(current))
	rel, err := filepath.Rel(curDir, filepath.ToSlash(target))
	if err != nil {
		return target
	}
	return filepath.ToSlash(rel)
}

// --- Single-file HTML report ---

func generateHTMLReport(writer io.Writer, results CheckResults) error {
	w := &errWriter{w: writer}
	totalFiles, totalTypos, totalSuggestions := summarizeStats(results)

	fmt.Fprint(w, htmlDocType)
	writeMetaHeader(w, "Spell Check Report")
	writeStatsBar(w, totalFiles, totalTypos, totalSuggestions)

	if len(results) == 0 {
		fmt.Fprint(w, `<div class="empty-state"><div class="icon">✅</div><p>No typos found.</p></div>`)
	} else {
		paths := sortedResultPaths(results)
		for _, file := range paths {
			words := results[file]
			fmt.Fprintf(w, `<h2>%s</h2>`, html.EscapeString(file))
			if len(words) > 0 {
				fmt.Fprint(w, `<table><tr><th>Line</th><th>Col</th><th>Word</th><th>Suggestions</th></tr>`)
				for _, m := range words {
					writeTypoRow(w, m)
				}
				fmt.Fprint(w, `</table>`)
			} else {
				fmt.Fprint(w, `<p>No typos found.</p>`)
			}
		}
	}

	fmt.Fprint(w, htmlFooter)
	return w.err
}

// --- Shared helpers for HTML generation ---

// errWriter records the first write error so report generators can return it
// instead of silently dropping output (disk full, closed pipe, ...).
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) Write(p []byte) (int, error) {
	if ew.err != nil {
		return 0, ew.err
	}
	n, err := ew.w.Write(p)
	if err != nil {
		ew.err = err
	}
	return n, err
}

func writeMetaHeader(w io.Writer, title string) {
	fmt.Fprintf(w, `<header><h1>%s</h1><div class="meta">Generated %s</div></header>`,
		html.EscapeString(title), time.Now().Format("2006-01-02 15:04:05"))
}

func writeStatsBar(w io.Writer, totalFiles, totalTypos, totalSuggestions int) {
	cls := "clean"
	if totalTypos > 0 {
		cls = "error"
	}
	fmt.Fprintf(w, `<div class="stats">
  <div class="stat-card"><div class="value">%d</div><div class="label">Files Checked</div></div>
  <div class="stat-card"><div class="value %s">%d</div><div class="label">Typos Found</div></div>
  <div class="stat-card"><div class="value suggestion">%d</div><div class="label">Suggestions</div></div>
</div>`, totalFiles, cls, totalTypos, totalSuggestions)
}

// writeTypoRow emits one typo table row, shared by the single-file and
// multi-file HTML reports so the markup cannot drift between them.
func writeTypoRow(w io.Writer, m MisspelledWord) {
	fmt.Fprintf(w, `<tr><td class="line">%d</td><td class="col">%d</td><td class="word">%s</td><td class="suggestions">%s</td></tr>`,
		m.LineNumber, m.Column, html.EscapeString(m.Word), html.EscapeString(strings.Join(m.Suggestions, ", ")))
}

// --- Text report with optional terminal colors ---

// typoMessage renders the shared "X appears to be a typo" phrase with an
// optional "Did you mean ...?" suffix, keeping the text reporter and SARIF
// messages in sync. suggestions must be pre-joined (or colored); empty means
// no suffix.
func typoMessage(word, suggestions string) string {
	msg := fmt.Sprintf("\"%s\" appears to be a typo.", word)
	if suggestions != "" {
		msg += fmt.Sprintf(" Did you mean: %s?", suggestions)
	}
	return msg
}

// sortedResultPaths returns the result map's keys sorted, so every report
// format writes deterministic output.
func sortedResultPaths(results CheckResults) []string {
	paths := make([]string, 0, len(results))
	for p := range results {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// formatTypoLine renders a single misspelled word as a report line (without
// the leading bullet/prefix). Callers may pass pre-colored word and
// suggestion strings for terminal output; pass empty strings to use the raw
// values. The "Did you mean: ...?" suffix is only appended when suggestions
// exist, keeping the reporter and watcher in sync.
func formatTypoLine(m MisspelledWord, word, suggestions string) string {
	if word == "" {
		word = m.Word
	}
	if suggestions == "" && len(m.Suggestions) > 0 {
		suggestions = strings.Join(m.Suggestions, ", ")
	}
	return fmt.Sprintf("Line %d, Col %d: %s", m.LineNumber, m.Column, typoMessage(word, suggestions))
}

func generateTextReport(writer io.Writer, results CheckResults) error {
	w := &errWriter{w: writer}
	useColors := false
	if f, ok := writer.(*os.File); ok {
		fi, err := f.Stat()
		if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			useColors = true
		}
	}

	_, totalTypos, _ := summarizeStats(results)

	if len(results) == 0 {
		fmt.Fprintln(w, "No typos found.")
		return w.err
	}
	fmt.Fprintf(w, "Typos found (%d total):\n", totalTypos)

	paths := sortedResultPaths(results)

	for _, file := range paths {
		words := results[file]
		fmt.Fprintf(w, "\n--- In file %s ---\n", file)
		for _, m := range words {
			word := m.Word
			suggestionsStr := strings.Join(m.Suggestions, ", ")
			if useColors {
				word = ansiBold + ansiRed + word + ansiReset
				if len(m.Suggestions) > 0 {
					suggestionsStr = ansiGreen + suggestionsStr + ansiReset
				}
			}
			fmt.Fprintf(w, "- %s\n", formatTypoLine(m, word, suggestionsStr))
		}
	}
	return w.err
}

// --- JSON report ---

// jsonTypo is the JSON shape of a single misspelled word.
type jsonTypo struct {
	Word        string   `json:"word"`
	Line        int      `json:"line"`
	Column      int      `json:"column"`
	Suggestions []string `json:"suggestions"`
}

// jsonFile groups typos by source file.
type jsonFile struct {
	File  string     `json:"file"`
	Typos []jsonTypo `json:"typos"`
}

// jsonReport is the top-level JSON document.
type jsonReport struct {
	Summary struct {
		Files       int `json:"files"`
		Typos       int `json:"typos"`
		Suggestions int `json:"suggestions"`
	} `json:"summary"`
	Files []jsonFile `json:"files"`
}

// generateJSONReport writes a machine-readable report. Files are sorted by path
// for deterministic output, and empty suggestion lists serialize as [] (not null).
func generateJSONReport(writer io.Writer, results CheckResults) error {
	var report jsonReport
	totalFiles, totalTypos, totalSuggestions := summarizeStats(results)
	report.Summary.Files = totalFiles
	report.Summary.Typos = totalTypos
	report.Summary.Suggestions = totalSuggestions

	paths := sortedResultPaths(results)

	report.Files = make([]jsonFile, 0, len(paths))
	for _, p := range paths {
		words := results[p]
		jf := jsonFile{File: p, Typos: make([]jsonTypo, 0, len(words))}
		for _, m := range words {
			suggestions := m.Suggestions
			if suggestions == nil {
				suggestions = []string{}
			}
			jf.Typos = append(jf.Typos, jsonTypo{
				Word:        m.Word,
				Line:        m.LineNumber,
				Column:      m.Column,
				Suggestions: suggestions,
			})
		}
		report.Files = append(report.Files, jf)
	}

	enc := json.NewEncoder(writer)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(report)
}
