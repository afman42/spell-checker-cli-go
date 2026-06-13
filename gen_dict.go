//go:build ignore

// Command gen_dict regenerates the embedded dictionary word list from
// dictionary.csv. It extracts the first column (the word), lowercases and
// de-duplicates it, sorts it, and writes a zstd-compressed, newline-delimited
// word list to dictionary.txt.zst.
//
// Run it whenever dictionary.csv changes:
//
//	go run gen_dict.go
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen_dict:", err)
		os.Exit(1)
	}
}

func run() error {
	in, err := os.Open("dictionary.csv")
	if err != nil {
		return err
	}
	defer in.Close()

	r := csv.NewReader(in)
	r.FieldsPerRecord = -1 // tolerate ragged rows

	// Skip header.
	if _, err := r.Read(); err != nil {
		return fmt.Errorf("reading header: %w", err)
	}

	seen := make(map[string]struct{})
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading record: %w", err)
		}
		if len(record) == 0 {
			continue
		}
		word := strings.ToLower(record[0])
		if word == "" {
			continue
		}
		seen[word] = struct{}{}
	}

	words := make([]string, 0, len(seen))
	for w := range seen {
		words = append(words, w)
	}
	sort.Strings(words)

	out, err := os.Create("dictionary.txt.zst")
	if err != nil {
		return err
	}
	defer out.Close()

	zw, err := zstd.NewWriter(out, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return fmt.Errorf("creating zstd writer: %w", err)
	}
	defer zw.Close()

	for _, w := range words {
		if _, err := io.WriteString(zw, w+"\n"); err != nil {
			return err
		}
	}

	if err := zw.Close(); err != nil {
		return err
	}

	fmt.Printf("wrote dictionary.txt.zst: %d unique words\n", len(words))
	return nil
}
