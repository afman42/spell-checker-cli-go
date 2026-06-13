package main

import (
	"sort"
	"strings"
)

// BK-tree node for efficient fuzzy string matching
type BKNode struct {
	Word     string
	Children map[int]*BKNode
}

// BKTree implements a Burkhard-Keller tree for efficient fuzzy string matching
type BKTree struct {
	root *BKNode
}

// scoredWord pairs a candidate suggestion with its edit distance.
type scoredWord struct {
	word     string
	distance int
}

// NewBKTree creates a new BK-tree from a dictionary
func NewBKTree(dictionary map[string]struct{}) *BKTree {
	tree := &BKTree{}
	for word := range dictionary {
		tree.Add(word)
	}
	return tree
}

// Add inserts a word into the BK-tree
func (tree *BKTree) Add(word string) {
	if tree.root == nil {
		tree.root = &BKNode{Word: word, Children: make(map[int]*BKNode)}
		return
	}

	node := tree.root
	for {
		distance := levenshteinDistance(node.Word, word)
		if distance == 0 {
			// Word already exists
			return
		}

		if child, exists := node.Children[distance]; exists {
			node = child
		} else {
			node.Children[distance] = &BKNode{Word: word, Children: make(map[int]*BKNode)}
			return
		}
	}
}

// Search finds words within a given edit distance threshold, paired with distance.
func (tree *BKTree) Search(word string, threshold int) []scoredWord {
	if tree.root == nil {
		return nil
	}
	var results []scoredWord
	tree.searchRecursive(tree.root, word, threshold, &results)
	return results
}

// searchRecursive is the helper for searching the BK-tree
func (tree *BKTree) searchRecursive(node *BKNode, word string, threshold int, results *[]scoredWord) {
	distance := levenshteinDistance(node.Word, word)

	if distance <= threshold {
		*results = append(*results, scoredWord{word: node.Word, distance: distance})
	}

	// Check subtrees that could contain matches: distances [distance-threshold, distance+threshold]
	for i := distance - threshold; i <= distance+threshold; i++ {
		if i >= 0 {
			if child, exists := node.Children[i]; exists {
				tree.searchRecursive(child, word, threshold, results)
			}
		}
	}
}

// levenshteinThreshold is the maximum edit distance to be considered a suggestion.
const levenshteinThreshold = 2

// maxSuggestions caps how many suggestions are returned for a single typo.
const maxSuggestions = 5

// maxSuggestionWordLength caps words fed into Levenshtein to prevent OOM
// from pathological input (e.g. binary data misidentified as text).
const maxSuggestionWordLength = 200

// rankSuggestions sorts candidates by edit distance (closest first), then
// alphabetically, and returns at most maxSuggestions words.
func rankSuggestions(scored []scoredWord) []string {
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].distance != scored[j].distance {
			return scored[i].distance < scored[j].distance
		}
		return scored[i].word < scored[j].word
	})
	limit := len(scored)
	if limit > maxSuggestions {
		limit = maxSuggestions
	}
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, scored[i].word)
	}
	return out
}

// generateSuggestions finds words in the dictionary that are "close" to a misspelled word.
// Results are ranked by edit distance and capped. Uses a BK-tree for larger dictionaries.
func generateSuggestions(word string, dictionary map[string]struct{}) []string {
	if len(word) > maxSuggestionWordLength {
		return nil
	}
	// For small dictionaries, use the simple approach
	if len(dictionary) < 100 {
		return simpleGenerateSuggestions(word, dictionary)
	}

	// For larger dictionaries, build a BK-tree
	tree := NewBKTree(dictionary)
	return rankSuggestions(tree.Search(strings.ToLower(word), levenshteinThreshold))
}

// simpleGenerateSuggestions is the brute-force implementation for small dictionaries.
func simpleGenerateSuggestions(word string, dictionary map[string]struct{}) []string {
	if len(word) > maxSuggestionWordLength {
		return nil
	}
	lowerWord := strings.ToLower(word)

	var scored []scoredWord
	for dictWord := range dictionary {
		// Optimization: skip comparing words with a length difference greater than the threshold.
		diff := len(dictWord) - len(lowerWord)
		if diff < 0 {
			diff = -diff
		}
		if diff > levenshteinThreshold {
			continue
		}

		distance := levenshteinDistance(lowerWord, dictWord)
		if distance <= levenshteinThreshold {
			scored = append(scored, scoredWord{word: dictWord, distance: distance})
		}
	}
	return rankSuggestions(scored)
}

// levenshteinDistance calculates the edit distance between two strings using dynamic programming.
func levenshteinDistance(a, b string) int {
	// Guard against pathological input that would allocate huge DP tables.
	if len(a) > maxSuggestionWordLength || len(b) > maxSuggestionWordLength {
		if len(a) > len(b) {
			return len(a)
		}
		return len(b)
	}

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
			// min is a Go 1.21+ builtin.
			dp[i][j] = min(dp[i-1][j]+1, dp[i][j-1]+1, dp[i-1][j-1]+cost)
		}
	}
	return dp[lenA][lenB]
}
