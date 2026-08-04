package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestTreeCacheKeyStable verifies the cache key is deterministic for the same
// dictionary and changes for a different one (case included).
func TestTreeCacheKeyStable(t *testing.T) {
	a := map[string]struct{}{"hello": {}, "world": {}, "ONE": {}}
	b := map[string]struct{}{"hello": {}, "world": {}, "one": {}}
	if key := treeCacheKey(a); key != treeCacheKey(a) {
		t.Errorf("key not stable: %s vs %s", key, treeCacheKey(a))
	}
	if treeCacheKey(a) == treeCacheKey(b) {
		t.Error("different dictionaries should produce different keys")
	}
}

// TestBKTreeCacheRoundTrip verifies a persisted tree decodes into one that
// produces identical suggestions, and that garbage files are ignored.
func TestBKTreeCacheRoundTrip(t *testing.T) {
	dict := map[string]struct{}{
		"world": {}, "whorl": {}, "wald": {}, "word": {}, "hello": {}, "worn": {},
	}
	orig := NewBKTree(dict)

	path := filepath.Join(t.TempDir(), "bktree.gob")
	if err := writeBKTreeCacheAt(path, orig); err != nil {
		t.Fatalf("write: %v", err)
	}
	back, err := readBKTreeCacheAt(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	for _, w := range []string{"worl", "wrold", "helo", "xyz"} {
		got := rankSuggestions(back.Search(w, levenshteinThreshold), w)
		want := rankSuggestions(orig.Search(w, levenshteinThreshold), w)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("word %q: decoded=%v original=%v", w, got, want)
		}
	}

	// A corrupt cache file must be treated as a miss, not a panic.
	bad := filepath.Join(t.TempDir(), "bad.gob")
	if err := os.WriteFile(bad, []byte("not a gob stream"), 0644); err != nil {
		t.Fatalf("write bad: %v", err)
	}
	if _, err := readBKTreeCacheAt(bad); err == nil {
		t.Error("expected error decoding corrupt cache file")
	}
}
