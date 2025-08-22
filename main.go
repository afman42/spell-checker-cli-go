package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	dictPath := flag.String("dict", "", "Optional: path to a custom CSV dictionary file.")
	excludeStr := flag.String("exclude", "", "Optional: comma-separated list of file patterns to exclude.")
	outputPath := flag.String("output", "", "Optional: path to an output file.")
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
	// --- IMPROVEMENT: Pass verbose flag to the checker ---
	allTypos, err := runConcurrentChecker(path, dictionary, excludePatterns, *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing path: %v\n", err)
		os.Exit(1)
	}

	var outputWriter io.Writer = os.Stdout
	if *outputPath != "" {
		file, err := os.Create(*outputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()
		outputWriter = file
		fmt.Printf("Report will be saved to: %s\n", *outputPath)
	}

	isHTML := false
	if strings.ToLower(*outputFormat) == "html" {
		isHTML = true
	} else if *outputFormat == "" && *outputPath != "" {
		if strings.ToLower(filepath.Ext(*outputPath)) == ".html" {
			isHTML = true
		}
	}

	if isHTML {
		generateHTMLReport(outputWriter, allTypos)
	} else {
		generateTextReport(outputWriter, allTypos)
	}

	if len(allTypos) > 0 {
		os.Exit(1)
	}
}
