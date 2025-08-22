package main

import (
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
	}

	for _, tc := range testCases {
		t.Run(tc.a+"->"+tc.b, func(t *testing.T) {
			if got := levenshteinDistance(tc.a, tc.b); got != tc.expected {
				t.Errorf("levenshteinDistance(%q, %q) = %d; want %d", tc.a, tc.b, got, tc.expected)
			}
		})
	}
}

func TestGenerateSuggestions(t *testing.T) {
	mockDictionary := map[string]struct{}{
		"hello": {}, "world": {}, "error": {}, "errors": {}, "go": {}, "golang": {},
	}

	testCases := []struct {
		word     string
		expected []string
	}{
		{"wrold", []string{"world"}},
		// FIX: Correctly expect both "error" and "errors".
		{"eror", []string{"error", "errors"}},
		{"errror", []string{"error", "errors"}},
		// FIX: Correctly expect only "golang" (distance to "go" is 3).
		{"golan", []string{"golang"}},
		// This now passes because the function returns a non-nil empty slice.
		{"xyz", []string{}},
	}

	for _, tc := range testCases {
		t.Run(tc.word, func(t *testing.T) {
			got := generateSuggestions(tc.word, mockDictionary)
			sort.Strings(got)
			sort.Strings(tc.expected)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("generateSuggestions(%q) = %v; want %v", tc.word, got, tc.expected)
			}
		})
	}
}
