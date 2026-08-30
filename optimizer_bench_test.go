package main

import (
	"strings"
	"testing"
)

// BenchmarkSuggest measures the full suggestion path (BK-tree search + ranking)
// for a realistic typo against the embedded dictionary. The same 5 typos repeat
// every iteration, so this exercises the memo cache — the common scan pattern.
func BenchmarkSuggest(b *testing.B) {
	dict := loadBenchDictionary(b)
	cd := NewConcurrentDictionary(dict)
	// force tree build
	_ = cd.Suggest("worl")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, typo := range []string{"worl", "teh", "recieve", "adress", "langauge"} {
			_ = cd.Suggest(typo)
		}
	}
}

// BenchmarkSuggestDistinct measures the uncached BK-tree search cost: every
// typo is unique, so no memo hits. Guards the raw suggestion hot path.
func BenchmarkSuggestDistinct(b *testing.B) {
	dict := loadBenchDictionary(b)
	cd := NewConcurrentDictionary(dict)
	// Unique near-miss typos: prefix + one letter off each dict word.
	words := make([]string, 0, 100)
	for w := range dict {
		words = append(words, w)
		if len(words) == 100 {
			break
		}
	}
	typos := make([]string, len(words))
	for i, w := range words {
		if len(w) > 3 {
			typos[i] = w[:len(w)-1] + "x"
		} else {
			typos[i] = w + "x"
		}
	}
	_ = cd.Suggest("worl")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, t := range typos {
			_ = cd.Suggest(t)
		}
	}
}

func BenchmarkLevenshtein(b *testing.B) {
	pairs := [][2]string{
		{"world", "wordl"},
		{"receive", "recieve"},
		{"language", "langauge"},
		{"address", "adress"},
		{"café", "cafe"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range pairs {
			_ = levenshteinDistance(p[0], p[1])
		}
	}
}

func BenchmarkOSA(b *testing.B) {
	pairs := [][2]string{
		{"world", "wordl"},
		{"receive", "recieve"},
		{"language", "langauge"},
		{"address", "adress"},
		{"café", "cafe"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range pairs {
			_ = osaDistance(p[0], p[1])
		}
	}
}

func BenchmarkRankSuggestions(b *testing.B) {
	scored := []scoredWord{
		{"world", 1}, {"wordl", 1}, {"whorl", 2}, {"worn", 2},
		{"wald", 2}, {"word", 1}, {"words", 2}, {"would", 2},
		{"work", 2}, {"worm", 2}, {"wore", 2}, {"wars", 2},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rankSuggestions(scored, "worl")
	}
}

func BenchmarkScanForTypos(b *testing.B) {
	dict := loadBenchDictionary(b)
	cd := NewConcurrentDictionary(dict)
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. "+
		"Hello worl, this is a tset of teh speling checker. ", 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scanForTypos(strings.NewReader(text), cd, scanOptions{})
	}
}

func loadBenchDictionary(b *testing.B) map[string]struct{} {
	dict, err := loadDictionary("")
	if err != nil {
		b.Fatal(err)
	}
	return dict
}
