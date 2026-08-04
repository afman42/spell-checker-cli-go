package main

import (
	"sort"
	"strings"
)

// BKNode is a node in the BK-tree. The fields are exported so a built tree can
// be persisted with encoding/gob for cross-run caching.
type BKNode struct {
	Word     string
	Children map[int]*BKNode
}

// BKTree implements a Burkhard-Keller tree for efficient fuzzy string matching.
type BKTree struct {
	Root *BKNode
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
	if tree.Root == nil {
		tree.Root = &BKNode{Word: word, Children: make(map[int]*BKNode)}
		return
	}

	node := tree.Root
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
	if tree.Root == nil {
		return nil
	}
	var results []scoredWord
	tree.searchRecursive(tree.Root, word, threshold, &results)
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

// rankedWord is a suggestion with its transposition-aware score and a flag for
// whether it is an exact adjacent-transposition match (teh→the).
type rankedWord struct {
	word  string
	eff   int
	trans bool
}

// rankSuggestions sorts candidates by edit distance (closest first), then by
// how closely they match the typo's structure — transposition matches first,
// then whether the typo's letters still appear in order in the suggestion,
// then their shared prefix length — so a naive alphabetical tie-break can't
// shove "whorl" ahead of "world" for the typo "worl". Distances account for
// transpositions (teh→the), which plain Levenshtein counts as 2 edits.
// Returns at most maxSuggestions words.
func rankSuggestions(scored []scoredWord, typo string) []string {
	typoLower := strings.ToLower(typo)

	byEff := make([]rankedWord, len(scored))
	for i, s := range scored {
		osa := osaDistance(typoLower, s.word)
		eff := s.distance
		if osa < eff {
			eff = osa
		}
		byEff[i] = rankedWord{word: s.word, eff: eff, trans: osa < s.distance}
	}

	sort.Slice(byEff, func(i, j int) bool {
		a, b := byEff[i], byEff[j]
		if a.eff != b.eff {
			return a.eff < b.eff
		}
		if a.trans != b.trans {
			return a.trans
		}
		aSub, bSub := isSubsequence(typoLower, a.word), isSubsequence(typoLower, b.word)
		if aSub != bSub {
			return aSub
		}
		aPre, bPre := commonPrefixLen(typoLower, a.word), commonPrefixLen(typoLower, b.word)
		if aPre != bPre {
			return aPre > bPre
		}
		return a.word < b.word
	})
	limit := len(byEff)
	if limit > maxSuggestions {
		limit = maxSuggestions
	}
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, byEff[i].word)
	}
	return out
}

// osaDistance is the optimal string alignment distance (a.k.a. Damerau-
// Levenshtein), which treats an adjacent transposition as one edit. It is only
// used to score and rank candidates already found by the BK-tree search, so the
// tree's metric properties are unaffected.
func osaDistance(aStr, bStr string) int {
	if len(aStr) > maxSuggestionWordLength || len(bStr) > maxSuggestionWordLength {
		if len(aStr) > len(bStr) {
			return len(aStr)
		}
		return len(bStr)
	}
	a := []rune(aStr)
	b := []rune(bStr)
	lenA, lenB := len(a), len(b)

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
			if a[i-1] != b[j-1] {
				cost = 1
			}
			dp[i][j] = min(dp[i-1][j]+1, dp[i][j-1]+1, dp[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				dp[i][j] = min(dp[i][j], dp[i-2][j-2]+1)
			}
		}
	}
	return dp[lenA][lenB]
}

// isSubsequence reports whether the runes of a appear, in order, within b.
func isSubsequence(a, b string) bool {
	if len(a) == 0 {
		return true
	}
	i := 0
	for _, r := range b {
		if i < len(a) && byte(a[i]) == byte(r) {
			i++
			if i == len(a) {
				return true
			}
		}
	}
	return false
}

// commonPrefixLen returns the length (in bytes) of the shared prefix of a and b.
func commonPrefixLen(a, b string) int {
	n := 0
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	for n < max && a[n] == b[n] {
		n++
	}
	return n
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
	return rankSuggestions(scored, word)
}

// levenshteinDistance calculates the edit distance between two strings using
// dynamic programming with a rolling two-row table, so memory stays O(lenB)
// instead of O(lenA*lenB) and the tight loop touches cache-hot rows.
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
	if lenA == 0 {
		return lenB
	}
	if lenB == 0 {
		return lenA
	}

	prev := make([]int, lenB+1)
	cur := make([]int, lenB+1)
	for j := 0; j <= lenB; j++ {
		prev[j] = j
	}

	for i := 1; i <= lenA; i++ {
		cur[0] = i
		ai := runesA[i-1]
		for j := 1; j <= lenB; j++ {
			cost := 0
			if ai != runesB[j-1] {
				cost = 1
			}
			// min is a Go 1.21+ builtin.
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[lenB]
}
