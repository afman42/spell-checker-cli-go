package main

import (
	"math"
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

// Search finds words within a given edit distance threshold
func (tree *BKTree) Search(word string, threshold int) []string {
	if tree.root == nil {
		return []string{}
	}

	var results []string
	tree.searchRecursive(tree.root, word, threshold, &results)
	return results
}

// searchRecursive is the helper for searching the BK-tree
func (tree *BKTree) searchRecursive(node *BKNode, word string, threshold int, results *[]string) {
	distance := levenshteinDistance(node.Word, word)

	// If the distance is within our threshold, add to results
	if distance <= threshold {
		*results = append(*results, node.Word)
	}

	// Check subtrees that could contain matches
	// We need to check distances [distance - threshold, distance + threshold]
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

// generateSuggestions finds words in the dictionary that are "close" to a misspelled word.
// Uses a BK-tree for efficient fuzzy string matching.
func generateSuggestions(word string, dictionary map[string]struct{}) []string {
	// For small dictionaries, use the simple approach
	if len(dictionary) < 100 {
		return simpleGenerateSuggestions(word, dictionary)
	}

	// For larger dictionaries, build a BK-tree
	tree := NewBKTree(dictionary)
	return tree.Search(strings.ToLower(word), levenshteinThreshold)
}

// simpleGenerateSuggestions is the original implementation for small dictionaries
func simpleGenerateSuggestions(word string, dictionary map[string]struct{}) []string {
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
