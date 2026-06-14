package main

import (
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

// --- Modern HTML template with dark mode, responsive design, and polished UI ---

const htmlDocType = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Spell Check Report</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  :root {
    --bg: #ffffff; --surface: #f8f9fa; --border: #e0e0e0;
    --text: #1a1a2e; --text-muted: #6c757d;
    --primary: #4361ee; --primary-hover: #3a56d4;
    --accent: #f72585; --accent-green: #06d6a0;
    --error: #e63946; --suggestion: #4361ee;
    --shadow: 0 2px 8px rgba(0,0,0,0.08);
    --radius: 8px;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #0f0f23; --surface: #1a1a3e; --border: #2a2a5e;
      --text: #e0e0f0; --text-muted: #8888aa;
      --primary: #5e7ce2; --primary-hover: #7b96ff;
      --accent: #f72585; --accent-green: #06d6a0;
      --error: #ff6b6b; --suggestion: #5e7ce2;
      --shadow: 0 2px 8px rgba(0,0,0,0.3);
    }
  }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
    background: var(--bg); color: var(--text);
    line-height: 1.6; padding: 24px; min-height: 100vh;
  }
  .container { max-width: 960px; margin: 0 auto; }

  /* Header */
  header { margin-bottom: 32px; }
  h1 {
    font-size: 1.75rem; font-weight: 700; letter-spacing: -0.02em;
    display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
  }
  h1 .badge {
    font-size: 0.75rem; font-weight: 600; padding: 4px 12px;
    border-radius: 20px; background: var(--primary); color: #fff;
  }
  h1 .badge.clean { background: var(--accent-green); color: #000; }
  h1 .badge.dirty { background: var(--error); }
  .meta {
    margin-top: 8px; font-size: 0.85rem; color: var(--text-muted);
    display: flex; gap: 16px; flex-wrap: wrap;
  }
  .stats {
    display: flex; gap: 16px; flex-wrap: wrap; margin: 24px 0;
  }
  .stat-card {
    flex: 1; min-width: 120px; padding: 16px 20px;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); box-shadow: var(--shadow);
    text-align: center;
  }
  .stat-card .value {
    font-size: 1.5rem; font-weight: 700; line-height: 1.2;
  }
  .stat-card .label {
    font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em;
    color: var(--text-muted); margin-top: 4px;
  }
  .stat-card .value.error { color: var(--error); }
  .stat-card .value.clean { color: var(--accent-green); }
  .stat-card .value.suggestion { color: var(--suggestion); }

  /* Navigation / Back link */
  .nav-link {
    display: inline-flex; align-items: center; gap: 6px;
    color: var(--primary); text-decoration: none; font-size: 0.9rem;
    margin-bottom: 16px; font-weight: 500;
  }
  .nav-link:hover { color: var(--primary-hover); text-decoration: underline; }

  /* File list (index page) */
  .file-list { list-style: none; padding: 0; }
  .file-item {
    display: flex; align-items: center; justify-content: space-between;
    padding: 14px 18px; margin-bottom: 8px;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); box-shadow: var(--shadow);
    transition: transform 0.15s ease, box-shadow 0.15s ease;
    flex-wrap: wrap; gap: 8px;
  }
  .file-item:hover {
    transform: translateY(-1px); box-shadow: 0 4px 16px rgba(0,0,0,0.12);
  }
  .file-item a {
    color: var(--primary); text-decoration: none; font-weight: 500;
    word-break: break-all;
  }
  .file-item a:hover { color: var(--primary-hover); text-decoration: underline; }
  .file-item .typo-count {
    font-size: 0.8rem; font-weight: 600; white-space: nowrap;
  }
  .file-item .typo-count.error { color: var(--error); }
  .file-item .typo-count.clean { color: var(--accent-green); }

  /* Tables */
  table {
    width: 100%; border-collapse: separate; border-spacing: 0;
    margin: 16px 0; border-radius: var(--radius); overflow: hidden;
    box-shadow: var(--shadow);
  }
  th {
    background: var(--primary); color: #fff; font-weight: 600;
    padding: 12px 16px; text-align: left; font-size: 0.85rem;
    text-transform: uppercase; letter-spacing: 0.04em;
  }
  td {
    padding: 12px 16px; border-bottom: 1px solid var(--border);
    background: var(--surface); font-size: 0.9rem;
  }
  tr:last-child td { border-bottom: none; }
  tr:hover td { background: color-mix(in srgb, var(--primary) 8%, var(--surface)); }
  td.word { font-weight: 600; color: var(--error); }
  td.suggestions { color: var(--suggestion); }
  td.line,
  td.col { text-align: center; font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; }

  /* Empty state */
  .empty-state {
    text-align: center; padding: 48px 24px;
    color: var(--text-muted); font-size: 1.1rem;
  }
  .empty-state .icon { font-size: 3rem; margin-bottom: 16px; }

  /* Responsive */
  @media (max-width: 640px) {
    body { padding: 12px; }
    h1 { font-size: 1.35rem; }
    .stats { flex-direction: column; }
    .stat-card { min-width: auto; }
    th, td { padding: 8px 10px; font-size: 0.8rem; }
    .file-item { padding: 10px 14px; }
    table { font-size: 0.8rem; }
  }
</style>
</head>
<body>
<div class="container">`

const htmlFooter = "\n</div>\n</body>\n</html>"

// summarizeStats computes total counts from all results.
func summarizeStats(results map[string][]MisspelledWord) (totalFiles, totalTypos, totalSuggestions int) {
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
func generateMultiFileHTMLReport(outputDir string, results map[string][]MisspelledWord) error {
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
func generateIndexFile(outputDir string, entries []fileEntry, results map[string][]MisspelledWord) error {
	indexPath := filepath.Join(outputDir, "index.html")
	file, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("could not create index file: %w", err)
	}
	defer file.Close()

	totalFiles, totalTypos, totalSuggestions := summarizeStats(results)

	fmt.Fprint(file, htmlDocType)
	writeMetaHeader(file, "Spell Check Summary")
	writeStatsBar(file, totalFiles, totalTypos, totalSuggestions)

	if len(entries) == 0 {
		fmt.Fprint(file, `<div class="empty-state"><div class="icon">✅</div><p>No typos found.</p></div>`)
	} else {
		fmt.Fprint(file, `<ul class="file-list">`)
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
			fmt.Fprintf(file, `<li class="file-item"><a href="%s">%s</a> <span class="typo-count %s">%s</span></li>`,
				html.EscapeString(entry.filename), html.EscapeString(entry.path), clsLabel, label)
		}
		fmt.Fprint(file, `</ul>`)
	}

	fmt.Fprint(file, htmlFooter)
	return nil
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

	fmt.Fprint(file, htmlDocType)

	// Back to index link — compute correct relative path from subdirectories
	backLink := relLink(filename, "index.html")
	fmt.Fprintf(file, `<a class="nav-link" href="%s">← Back to Summary</a>`, html.EscapeString(backLink))

	// Heading
	fmt.Fprintf(file, `<h1>%s</h1>`, html.EscapeString(filePath))
	fmt.Fprintf(file, `<div class="meta">%d typo(s) found</div>`, len(words))

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
			fmt.Fprintf(file, `<div style="display:flex;gap:16px;margin-bottom:8px">%s %s</div>`, prevLink, nextLink)
		}
	}

	// Table
	if len(words) > 0 {
		fmt.Fprint(file, `<table><tr><th>Line</th><th>Col</th><th>Word</th><th>Suggestions</th></tr>`)
		for _, m := range words {
			suggestionsStr := strings.Join(m.Suggestions, ", ")
			fmt.Fprintf(file, `<tr><td class="line">%d</td><td class="col">%d</td><td class="word">%s</td><td class="suggestions">%s</td></tr>`,
				m.LineNumber, m.Column, html.EscapeString(m.Word), html.EscapeString(suggestionsStr))
		}
		fmt.Fprint(file, `</table>`)
	} else {
		fmt.Fprint(file, `<div class="empty-state"><div class="icon">✅</div><p>No typos found in this file.</p></div>`)
	}

	fmt.Fprint(file, htmlFooter)
	return nil
}

// relLink computes a relative URL from the current report filename to a target filename.
// Both filenames use forward slashes (as produced by safeReportPath).
func relLink(current, target string) string {
	curDir := filepath.Dir(current)
	targetDir := filepath.Dir(target)
	// Same directory — just use the target's base name.
	if curDir == targetDir {
		return filepath.Base(target)
	}
	if curDir == "." {
		return target
	}
	// Walk up from current's directory to the common ancestor, then back down.
	parts := strings.Split(curDir, "/")
	up := strings.Repeat("../", len(parts))
	return up + target
}

// --- Single-file HTML report ---

func generateHTMLReport(writer io.Writer, results map[string][]MisspelledWord) {
	totalFiles, totalTypos, totalSuggestions := summarizeStats(results)

	fmt.Fprint(writer, htmlDocType)
	writeMetaHeader(writer, "Spell Check Report")
	writeStatsBar(writer, totalFiles, totalTypos, totalSuggestions)

	if len(results) == 0 {
		fmt.Fprint(writer, `<div class="empty-state"><div class="icon">✅</div><p>No typos found.</p></div>`)
	} else {
		// Sort file paths for consistent output
		paths := make([]string, 0, len(results))
		for p := range results {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		for _, file := range paths {
			words := results[file]
			fmt.Fprintf(writer, `<h2>%s</h2>`, html.EscapeString(file))
			if len(words) > 0 {
				fmt.Fprint(writer, `<table><tr><th>Line</th><th>Col</th><th>Word</th><th>Suggestions</th></tr>`)
				for _, m := range words {
					suggestionsStr := strings.Join(m.Suggestions, ", ")
					fmt.Fprintf(writer, `<tr><td class="line">%d</td><td class="col">%d</td><td class="word">%s</td><td class="suggestions">%s</td></tr>`,
						m.LineNumber, m.Column, html.EscapeString(m.Word), html.EscapeString(suggestionsStr))
				}
				fmt.Fprint(writer, `</table>`)
			} else {
				fmt.Fprint(writer, `<p>No typos found.</p>`)
			}
		}
	}

	fmt.Fprint(writer, htmlFooter)
}

// --- Shared helpers for HTML generation ---

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

// --- Text report with optional terminal colors ---

func generateTextReport(writer io.Writer, results map[string][]MisspelledWord) {
	useColors := false
	if f, ok := writer.(*os.File); ok {
		fi, err := f.Stat()
		if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			useColors = true
		}
	}

	_, totalTypos, _ := summarizeStats(results)

	if len(results) == 0 {
		fmt.Fprintln(writer, "No typos found.")
		return
	}
	fmt.Fprintf(writer, "Typos found (%d total):\n", totalTypos)
	for file, words := range results {
		fmt.Fprintf(writer, "\n--- In file %s ---\n", file)
		for _, m := range words {
			word := m.Word
			suggestionsStr := strings.Join(m.Suggestions, ", ")
			if useColors {
				word = ansiBold + ansiRed + word + ansiReset
				if len(m.Suggestions) > 0 {
					suggestionsStr = ansiGreen + suggestionsStr + ansiReset
				}
			}
			baseMessage := fmt.Sprintf("- Line %d, Col %d: \"%s\" appears to be a typo.", m.LineNumber, m.Column, word)
			if len(m.Suggestions) > 0 {
				fmt.Fprintf(writer, "%s Did you mean: %s?\n", baseMessage, suggestionsStr)
			} else {
				fmt.Fprintln(writer, baseMessage)
			}
		}
	}
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
func generateJSONReport(writer io.Writer, results map[string][]MisspelledWord) error {
	var report jsonReport
	totalFiles, totalTypos, totalSuggestions := summarizeStats(results)
	report.Summary.Files = totalFiles
	report.Summary.Typos = totalTypos
	report.Summary.Suggestions = totalSuggestions

	paths := make([]string, 0, len(results))
	for p := range results {
		paths = append(paths, p)
	}
	sort.Strings(paths)

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
