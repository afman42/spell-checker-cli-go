package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	dictPath := flag.String("dict", "", "Optional: path to a custom CSV dictionary file.")
	excludeStr := flag.String("exclude", "", "Optional: comma-separated list of file patterns to exclude.")
	outputPath := flag.String("output", "", "Optional: path to an output file or directory (for HTML reports).")
	outputFormat := flag.String("format", "", "Optional: output format (txt, html). Overrides filename extension.")
	verbose := flag.Bool("verbose", false, "Enable verbose logging to show skipped files and directories.")
	flag.Parse()

	var excludePatterns []string
	if *excludeStr != "" {
		excludePatterns = strings.Split(*excludeStr, ",")
	}

	dictionary, err := loadDictionary(*dictPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error loading dictionary: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Successfully loaded %d words.\n", len(dictionary))

	if flag.NArg() < 1 {
		fmt.Println("Usage: spellchecker [flags] <file_or_directory>")
		os.Exit(1)
	}

	path := flag.Arg(0)
	allTypos, err := runConcurrentChecker(path, dictionary, excludePatterns, *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing path: %v\n", err)
		os.Exit(1)
	}

	// --- REVISED OUTPUT LOGIC ---
	if *outputPath == "" {
		// Default case: No output path provided, so print a text report to standard output.
		generateTextReport(os.Stdout, allTypos)
	} else {
		// An output path was provided. Determine the format and mode.
		format := strings.ToLower(*outputFormat)
		ext := strings.ToLower(filepath.Ext(*outputPath))

		// Determine if the desired format is HTML.
		isHTML := format == "html" || (format == "" && ext == ".html")

		// NEW: Determine if we should use the multi-file directory mode for HTML.
		// This is triggered if the format is HTML AND the path does not end in ".html".
		isMultiFileDir := isHTML && ext != ".html"

		if isMultiFileDir {
			fmt.Printf("Generating multi-file HTML report in directory: %s\n", *outputPath)
			if err := generateMultiFileHTMLReport(*outputPath, allTypos); err != nil {
				fmt.Fprintf(os.Stderr, "Error generating multi-file report: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Fallback to single-file output for text reports or specific HTML files.
			file, err := os.Create(*outputPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
				os.Exit(1)
			}
			defer file.Close()

			fmt.Printf("Report will be saved to: %s\n", *outputPath)
			if isHTML {
				generateHTMLReport(file, allTypos)
			} else {
				generateTextReport(file, allTypos)
			}
		}
	}

	if len(allTypos) > 0 {
		os.Exit(1)
	}
}
