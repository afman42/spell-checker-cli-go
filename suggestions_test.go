package main

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestLevenshteinDistance(t *testing.T) {
	testCases := []struct {
		a, b     string
		expected int
	}{
		{"", "hello", 5},
		{"hello", "hello", 0},
		{"cat", "car", 1},
		{"apple", "aple", 1},
		{"kitten", "sitting", 3},
		{"", "", 0},
		{"abc", "", 3},
		{"café", "cafe", 1}, // multibyte: é vs e is one substitution
	}

	for _, tc := range testCases {
		t.Run(tc.a+"->"+tc.b, func(t *testing.T) {
			if got := levenshteinDistance(tc.a, tc.b); got != tc.expected {
				t.Errorf("levenshteinDistance(%q, %q) = %d; want %d", tc.a, tc.b, got, tc.expected)
			}
		})
	}
}

// TestLevenshteinDistanceLongGuard verifies the OOM guard returns the longer
// length instead of allocating a huge DP table.
func TestLevenshteinDistanceLongGuard(t *testing.T) {
	long := make([]byte, maxSuggestionWordLength+50)
	for i := range long {
		long[i] = 'a'
	}
	got := levenshteinDistance(string(long), "short")
	if got != len(long) {
		t.Errorf("expected guard to return %d, got %d", len(long), got)
	}
}

func TestGenerateSuggestions(t *testing.T) {
	mockDictionary := map[string]struct{}{
		"hello": {}, "world": {}, "error": {}, "errors": {}, "go": {}, "golang": {},
		"state-of-the-art": {}, // Added for hyphenation test
	}
	cd := NewConcurrentDictionary(mockDictionary)

	testCases := []struct {
		word     string
		expected []string
	}{
		{"wrold", []string{"world"}},
		{"eror", []string{"error", "errors"}},
		{"errror", []string{"error", "errors"}},
		{"golan", []string{"golang"}},
		{"xyz", []string{}},
		// NEW: Test case for a misspelled hyphenated word.
		{"state-of-the-artt", []string{"state-of-the-art"}},
	}

	for _, tc := range testCases {
		t.Run(tc.word, func(t *testing.T) {
			got := cd.Suggest(tc.word)
			sort.Strings(got)
			sort.Strings(tc.expected)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("Suggest(%q) = %v; want %v", tc.word, got, tc.expected)
			}
		})
	}
}

// TestGenerateSuggestionsLongWord verifies that absurdly long words return no
// suggestions rather than doing expensive work.
func TestGenerateSuggestionsLongWord(t *testing.T) {
	dict := map[string]struct{}{"hello": {}, "world": {}}
	cd := NewConcurrentDictionary(dict)
	long := make([]byte, maxSuggestionWordLength+1)
	for i := range long {
		long[i] = 'a'
	}
	if got := cd.Suggest(string(long)); got != nil {
		t.Errorf("expected nil for over-long word, got %v", got)
	}
}

// TestRankSuggestionsOrderingAndCap verifies suggestions are ordered by edit
// distance (then alphabetically) and capped at maxSuggestions.
func TestRankSuggestionsOrderingAndCap(t *testing.T) {
	scored := []scoredWord{
		{"delta", 2},
		{"bravo", 1},
		{"alpha", 1},
		{"echo", 2},
		{"charlie", 0},
		{"foxtrot", 2},
		{"golf", 2},
	}
	got := rankSuggestions(scored, "")

	// distance 0: charlie; distance 1: alpha, bravo; distance 2: delta, echo, foxtrot, golf
	// capped at 5 -> charlie, alpha, bravo, delta, echo
	want := []string{"charlie", "alpha", "bravo", "delta", "echo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rankSuggestions() = %v, want %v", got, want)
	}
	if len(got) > maxSuggestions {
		t.Errorf("expected at most %d suggestions, got %d", maxSuggestions, len(got))
	}
}

// TestOsaDistance verifies adjacent transpositions cost one edit, unlike plain
// Levenshtein.
func TestOsaDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"teh", "the", 1},         // adjacent transposition
		{"recieve", "receive", 1}, // transposed 'ie'/'ei'
		{"helo", "hello", 1},      // insertion
		{"wrld", "world", 1},
		{"hello", "hello", 0},
		{"cat", "dog", 3},
	}
	for _, tc := range cases {
		if got := osaDistance(tc.a, tc.b); got != tc.want {
			t.Errorf("osaDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestRankSuggestionsTransposition verifies a transposition-1 suggestion beats
// equally-distant (in Levenshtein terms) alternatives.
func TestRankSuggestionsTransposition(t *testing.T) {
	scored := []scoredWord{
		{"tea", 1},  // substitution, lev distance 1
		{"tee", 1},  // substitution, lev distance 1
		{"the", 2},  // transposition: lev 2, but effective distance 1
		{"then", 2}, // transposition + insertion
	}
	got := rankSuggestions(scored, "teh")
	if got[0] != "the" {
		t.Errorf("expected 'the' first for typo 'teh', got %v", got)
	}
}

// TestRankSuggestionsEmpty verifies empty input yields an empty (non-panicking) result.
func TestRankSuggestionsEmpty(t *testing.T) {
	if got := rankSuggestions(nil, ""); len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

// TestRankSuggestionsTypoAware verifies that at equal edit distance, suggestions
// whose letters follow the typo's order, then share its prefix, are ranked above
// alphabetically-earlier-but-structurally-worse words.
func TestRankSuggestionsTypoAware(t *testing.T) {
	scored := []scoredWord{
		{"wald", 1},  // no 'l' after 'o' — not a subsequence
		{"whorl", 1}, // subsequence, shares only "w"
		{"world", 1}, // subsequence + 4-char shared prefix
		{"worn", 1},  // shares "wor" prefix but not a subsequence
	}
	got := rankSuggestions(scored, "worl")
	// distance ties → subsequence matches first (world, whorl), then the rest.
	want := []string{"world", "whorl", "worn", "wald"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rankSuggestions() = %v, want %v", got, want)
	}
}

// TestSuggestUsesBKTreeForLargeDict verifies the BK-tree path produces the same
// ranked results as the brute-force path for a dictionary over the threshold.
func TestSuggestUsesBKTreeForLargeDict(t *testing.T) {
	dict := make(map[string]struct{})
	// Fill past the 100-word threshold so the BK-tree path is used.
	for i := 0; i < 150; i++ {
		dict[fmt.Sprintf("filler%d", i)] = struct{}{}
	}
	dict["world"] = struct{}{}
	dict["word"] = struct{}{}

	cd := NewConcurrentDictionary(dict)
	if cd.bkTree != nil {
		t.Fatal("expected BK-tree to be built lazily, was pre-built")
	}

	got := cd.Suggest("wrold")
	if cd.bkTree == nil {
		t.Fatal("expected BK-tree to be built after first Suggest call")
	}
	found := false
	for _, s := range got {
		if s == "world" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'world' among suggestions for 'wrold', got %v", got)
	}
	if len(got) > maxSuggestions {
		t.Errorf("expected at most %d suggestions, got %d", maxSuggestions, len(got))
	}
}

// TestBKTreeSearchEmptyTree ensures searching an empty tree is safe.
func TestBKTreeSearchEmptyTree(t *testing.T) {
	tree := &BKTree{}
	if got := tree.Search("anything", 2); got != nil {
		t.Errorf("expected nil from empty tree, got %v", got)
	}
}

// TestBKTreeAddDuplicate ensures adding the same word twice doesn't duplicate it.
func TestBKTreeAddDuplicate(t *testing.T) {
	dict := map[string]struct{}{"apple": {}, "apply": {}, "ample": {}}
	tree := NewBKTree(dict)
	tree.Add("apple") // duplicate

	results := tree.Search("apple", 0)
	count := 0
	for _, r := range results {
		if r.word == "apple" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 'apple' exactly once, got %d", count)
	}
}
