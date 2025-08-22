package main

import (
	"math"
	"strings"
)

// levenshteinThreshold is the maximum edit distance to be considered a suggestion.
const levenshteinThreshold = 2

// generateSuggestions finds words in the dictionary that are "close" to a misspelled word.
func generateSuggestions(word string, dictionary map[string]struct{}) []string {
	suggestions := make([]string, 0)
	lowerWord := strings.ToLower(word)

	for dictWord := range dictionary {
		// Optimization: skip comparing words with a length difference greater than the threshold.
		if math.Abs(float64(len(dictWord)-len(lowerWord))) > float64(levenshteinThreshold) {
			continue
		}

		distance := levenshteinDistance(lowerWord, dictWord)

		if distance <= levenshteinThreshold {
			suggestions = append(suggestions, dictWord)
		}
	}
	return suggestions
}

// levenshteinDistance calculates the edit distance between two strings using dynamic programming.
func levenshteinDistance(a, b string) int {
	runesA := []rune(a)
	runesB := []rune(b)
	lenA, lenB := len(runesA), len(runesB)

	dp := make([][]int, lenA+1)
	for i := range dp {
		dp[i] = make([]int, lenB+1)
	}

	for i := 0; i <= lenA; i++ {
		dp[i][0] = i
	}
	for j := 0; j <= lenB; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= lenA; i++ {
		for j := 1; j <= lenB; j++ {
			cost := 0
			if runesA[i-1] != runesB[j-1] {
				cost = 1
			}
			dp[i][j] = min(dp[i-1][j]+1, dp[i][j-1]+1, dp[i-1][j-1]+cost)
		}
	}
	return dp[lenA][lenB]
}

// min is a helper to find the minimum of three integers.
func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
	} else if b < c {
		return b
	}
	return c
}
