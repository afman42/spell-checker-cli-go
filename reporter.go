package main

import (
	"fmt"
	"io"
	"path/filepath"
)

func generateTextReport(writer io.Writer, results map[string][]MisspelledWord) {
	if len(results) == 0 {
		fmt.Fprintln(writer, "No typos found.")
		return
	}
	fmt.Fprintln(writer, "Typos found:")
	for file, words := range results {
		fmt.Fprintf(writer, "\n--- In file %s ---\n", file)
		for _, m := range words {
			fmt.Fprintf(writer, "- Line %d, Col %d: \"%s\" appears to be a typo\n", m.LineNumber, m.Column, m.Word)
		}
	}
}

func generateHTMLReport(writer io.Writer, results map[string][]MisspelledWord) {
	htmlHeader := `<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><title>Spell Check Report</title>
<style>body{font-family:sans-serif;max-width:960px;margin:20px auto}h1,h2{border-bottom:2px solid #eee}table{width:100%;border-collapse:collapse}th,td{padding:12px;border:1px solid #ddd}th{background-color:#3498db;color:white}</style>
</head><body><h1>Spell Check Report</h1>`
	fmt.Fprint(writer, htmlHeader)
	if len(results) == 0 {
		fmt.Fprint(writer, `<p>✅ No typos found.</p>`)
	} else {
		for file, words := range results {
			fmt.Fprintf(writer, `<h2>Typos in: %s</h2>`, filepath.Base(file))
			fmt.Fprint(writer, `<table><tr><th>Line</th><th>Column</th><th>Word</th></tr>`)
			for _, m := range words {
				fmt.Fprintf(writer, "<tr><td>%d</td><td>%d</td><td>%s</td></tr>", m.LineNumber, m.Column, m.Word)
			}
			fmt.Fprint(writer, `</table>`)
		}
	}
	fmt.Fprint(writer, "</body></html>")
}
