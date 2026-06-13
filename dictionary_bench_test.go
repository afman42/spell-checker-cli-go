package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// Benchmark data: pre-compressed copies of the word list.
var (
	gzipCompressed []byte
	zstdCompressed []byte
)

// BenchmarkResult captures a single benchmark outcome.
type BenchmarkResult struct {
	alg          string
	compressedKB float64
	nsPerOp      float64
	mbPerSec     float64
}

func (r BenchmarkResult) String() string {
	return fmt.Sprintf("%s: %5.0f KB compressed | %8.0f ns/op | %5.1f MB/s",
		r.alg, r.compressedKB, r.nsPerOp, r.mbPerSec)
}

// initBenchData loads the raw word list from dictionary.csv and compresses it
// with both gzip and zstd so each benchmark iteration decompresses identical data.
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

	// --- gzip (BestCompression) ---
	var gzBuf bytes.Buffer
	gzW, _ := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression)
	if _, err := gzW.Write(raw); err != nil {
		return fmt.Errorf("gzip write: %w", err)
	}
	if err := gzW.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}
	gzipCompressed = gzBuf.Bytes()

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
	if err := initBenchData(); err != nil {
		fmt.Fprintf(os.Stderr, "bench data init: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// --- Benchmark: gzip decompress + scan ---

func BenchmarkGzipDecompress(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(gzipCompressed)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		gz, err := gzip.NewReader(bytes.NewReader(gzipCompressed))
		if err != nil {
			b.Fatal(err)
		}
		dict := make(map[string]struct{}, 120000)
		scanner := bufio.NewScanner(gz)
		for scanner.Scan() {
			word := strings.TrimSpace(scanner.Text())
			if word != "" {
				dict[word] = struct{}{}
			}
		}
		gz.Close()
		if err := scanner.Err(); err != nil {
			b.Fatal(err)
		}
		if len(dict) < 1000 {
			b.Fatal("dictionary too small")
		}
	}
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

// --- Benchmark: gzip decompress only (no map build) ---

func BenchmarkGzipDecompressOnly(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(gzipCompressed)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		gz, err := gzip.NewReader(bytes.NewReader(gzipCompressed))
		if err != nil {
			b.Fatal(err)
		}
		n, err := io.Copy(io.Discard, gz)
		if err != nil {
			b.Fatal(err)
		}
		gz.Close()
		if n < 100000 {
			b.Fatal("too little data")
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
