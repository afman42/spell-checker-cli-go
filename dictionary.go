package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// dictionaryData is a zstd-compressed, newline-delimited, lowercased word list.
// Regenerate it with `go run gen_dict.go` whenever dictionary.csv changes.
//
//go:embed dictionary.txt.zst
var dictionaryData []byte

func loadDictionary(customPath string) (map[string]struct{}, error) {
	if customPath != "" {
		file, err := os.Open(customPath)
		if err != nil {
			return nil, fmt.Errorf("could not open custom dictionary: %w", err)
		}
		defer file.Close()
		return parseDictionary(file)
	}

	return parseEmbeddedDictionary(dictionaryData)
}

// parseEmbeddedDictionary decompresses and reads the embedded zstd-compressed
// word list (one lowercase word per line).
func parseEmbeddedDictionary(data []byte) (map[string]struct{}, error) {
	zr, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("could not open embedded dictionary: %w", err)
	}
	defer zr.Close()

	dictionary := make(map[string]struct{})
	scanner := bufio.NewScanner(zr)
	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		if word != "" {
			dictionary[word] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading embedded dictionary: %w", err)
	}
	return dictionary, nil
}

func parseDictionary(reader io.Reader) (map[string]struct{}, error) {
	dictionary := make(map[string]struct{})
	csvReader := csv.NewReader(reader)
	_, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("could not read dictionary header: %w", err)
	}
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading dictionary record: %w", err)
		}
		if len(record) > 0 {
			dictionary[strings.ToLower(record[0])] = struct{}{}
		}
	}
	return dictionary, nil
}

func loadPersonalDictionary(path string, dictionary map[string]struct{}) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("could not open personal dictionary: %w", err)
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		// Ignore empty lines or comments
		if word != "" && !strings.HasPrefix(word, "#") {
			dictionary[strings.ToLower(word)] = struct{}{}
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error reading personal dictionary: %w", err)
	}

	return count, nil
}

// ConcurrentDictionary provides thread-safe read access to the dictionary and
// lazily builds a BK-tree for efficient fuzzy suggestions on first use.
type ConcurrentDictionary struct {
	dict   map[string]struct{}
	mu     sync.Mutex
	bkTree *BKTree
	// suggestCache memoizes Suggest results per lowercase word. Suggestions
	// are deterministic for a fixed dictionary, and the same typo typically
	// recurs many times in a scan (or file). Bounded below; callers only read
	// the returned slice, never mutate it.
	suggestCache map[string][]string
}

// bkTreeMinDictSize is the dictionary size at or above which the BK-tree is
// used; smaller dictionaries fall back to brute-force generation.
const bkTreeMinDictSize = 100

// maxSuggestCacheEntries caps the suggestion memo cache. When full, new
// misses stop being cached — memory stays bounded and behavior is unchanged,
// only repeated-typo speedups are lost.
const maxSuggestCacheEntries = 1024

// NewConcurrentDictionary creates a new dictionary wrapper. The BK-tree is not
// built here: it is deferred until the first Suggest call, so a run that finds
// no typos never pays the build cost.
func NewConcurrentDictionary(dict map[string]struct{}) *ConcurrentDictionary {
	return &ConcurrentDictionary{dict: dict}
}

// treeLocked returns the cached BK-tree, building it once on first access and
// reusing one persisted on disk for identical dictionaries. Caller must NOT
// hold cd.mu; the method handles locking internally and never holds the mutex
// during disk IO or tree construction.
func (cd *ConcurrentDictionary) treeLocked() *BKTree {
	cd.mu.Lock()
	if cd.bkTree != nil {
		t := cd.bkTree
		cd.mu.Unlock()
		return t
	}
	if len(cd.dict) < bkTreeMinDictSize {
		cd.mu.Unlock()
		return nil
	}
	cd.mu.Unlock()
	if cached := loadBKTreeCache(cd.dict); cached != nil {
		cd.mu.Lock()
		if cd.bkTree == nil {
			cd.bkTree = cached
		}
		t := cd.bkTree
		cd.mu.Unlock()
		return t
	}
	built := NewBKTree(cd.dict)
	storeBKTreeCache(cd.dict, built)
	cd.mu.Lock()
	if cd.bkTree == nil {
		cd.bkTree = built
	}
	t := cd.bkTree
	cd.mu.Unlock()
	return t
}

// Contains checks if a word exists in the dictionary
func (cd *ConcurrentDictionary) Contains(word string) bool {
	_, exists := cd.dict[strings.ToLower(word)]
	return exists
}

// Suggest returns ranked spelling suggestions using the cached BK-tree (fast
// path) or falls back to brute-force for small dictionaries. Results are
// memoized per word: the BK search is deterministic, and repeated occurrences
// of the same typo (common in real scans) skip the expensive traversal.
func (cd *ConcurrentDictionary) Suggest(word string) []string {
	if len(word) > maxSuggestionWordLength {
		return nil
	}
	lower := strings.ToLower(word)
	cd.mu.Lock()
	if cd.suggestCache == nil {
		cd.suggestCache = make(map[string][]string, 16)
	}
	if cached, ok := cd.suggestCache[lower]; ok {
		cd.mu.Unlock()
		return append([]string{}, cached...)
	}
	cd.mu.Unlock()
	tree := cd.treeLocked()

	var sug []string
	if tree != nil {
		sug = rankSuggestions(tree.Search(lower, levenshteinThreshold), word)
	} else {
		sug = simpleGenerateSuggestions(word, cd.dict)
	}
	cd.mu.Lock()
	if len(cd.suggestCache) < maxSuggestCacheEntries {
		cd.suggestCache[lower] = sug
	}
	cd.mu.Unlock()
	return append([]string{}, sug...)
}
