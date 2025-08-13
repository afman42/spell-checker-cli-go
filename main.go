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
	// 1. Define all flags, including the new -format flag
	dictPath := flag.String("dict", "", "Optional: path to a custom CSV dictionary file.")
	excludeStr := flag.String("exclude", "", "Optional: comma-separated list of file patterns to exclude.")
	outputPath := flag.String("output", "", "Optional: path to an output file.")
	outputFormat := flag.String("format", "", "Optional: output format (txt, html). Overrides filename extension.")
	flag.Parse()

	// 2. --- Setup ---
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

	// 3. --- Gather all typos ---
	path := flag.Arg(0)
	allTypos, err := findTypos(path, dictionary, excludePatterns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing path: %v\n", err)
		os.Exit(1)
	}

	// 4. --- Reporting ---
	var outputWriter io.Writer = os.Stdout // Default to console
	if *outputPath != "" {
		file, err := os.Create(*outputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()
		outputWriter = file // Direct output to the file
		fmt.Printf("Report will be saved to: %s\n", *outputPath)
	}

	// Determine the output format with improved logic
	isHTML := false
	// Priority 1: Check the explicit -format flag
	if strings.ToLower(*outputFormat) == "html" {
		isHTML = true
	} else if strings.ToLower(*outputFormat) == "txt" {
		isHTML = false
	} else if *outputFormat == "" && *outputPath != "" {
		// Priority 2: If format is not set, check the output file's extension
		ext := strings.ToLower(filepath.Ext(*outputPath))
		if ext == ".html" {
			isHTML = true
		}
	}

	// Generate the report based on the determined format
	if isHTML {
		generateHTMLReport(outputWriter, allTypos)
	} else {
		generateTextReport(outputWriter, allTypos)
	}

	if len(allTypos) > 0 {
		os.Exit(1) // Exit with non-zero status if typos were found
	}
}
