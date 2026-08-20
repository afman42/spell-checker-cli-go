package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// Benchmark data: pre-compressed copy of the word list.
var zstdCompressed []byte

// initBenchData loads the raw word list from dictionary.csv and compresses it
// with zstd so each benchmark iteration decompresses identical data.
func initBenchData() error {
	f, err := os.Open("dictionary.csv")
	if err != nil {
		return fmt.Errorf("opening dictionary.csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	// Skip header.
	if _, err := r.Read(); err != nil {
		return fmt.Errorf("reading header: %w", err)
	}

	// Build the raw newline-delimited word list (lowercased, sorted already).
	var buf bytes.Buffer
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
		buf.WriteString(word + "\n")
	}
	raw := buf.Bytes()

	// --- zstd (BestCompression) ---
	var zstBuf bytes.Buffer
	zstW, err := zstd.NewWriter(&zstBuf, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return fmt.Errorf("zstd writer: %w", err)
	}
	if _, err := zstW.Write(raw); err != nil {
		return fmt.Errorf("zstd write: %w", err)
	}
	if err := zstW.Close(); err != nil {
		return fmt.Errorf("zstd close: %w", err)
	}
	zstdCompressed = zstBuf.Bytes()

	return nil
}

func TestMain(m *testing.M) {
	flag.Parse()
	// Bench data costs a full dictionary.csv parse + compression; only load it
	// when benchmarks are actually requested, not on every `go test` run.
	if flag.Lookup("test.bench").Value.String() != "" {
		if err := initBenchData(); err != nil {
			fmt.Fprintf(os.Stderr, "bench data init: %v\n", err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// --- Benchmark: zstd decompress + scan ---

func BenchmarkZstdDecompress(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(zstdCompressed)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		zr, err := zstd.NewReader(bytes.NewReader(zstdCompressed))
		if err != nil {
			b.Fatal(err)
		}
		dict := make(map[string]struct{}, 120000)
		scanner := bufio.NewScanner(zr)
		for scanner.Scan() {
			word := strings.TrimSpace(scanner.Text())
			if word != "" {
				dict[word] = struct{}{}
			}
		}
		zr.Close()
		if err := scanner.Err(); err != nil {
			b.Fatal(err)
		}
		if len(dict) < 1000 {
			b.Fatal("dictionary too small")
		}
	}
}

// --- Benchmark: zstd decompress only (no map build) ---

func BenchmarkZstdDecompressOnly(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(zstdCompressed)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		zr, err := zstd.NewReader(bytes.NewReader(zstdCompressed))
		if err != nil {
			b.Fatal(err)
		}
		n, err := io.Copy(io.Discard, zr)
		if err != nil {
			b.Fatal(err)
		}
		zr.Close()
		if n < 100000 {
			b.Fatal("too little data")
		}
	}
}
