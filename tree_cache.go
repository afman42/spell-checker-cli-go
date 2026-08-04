package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// treeCacheVersion is bumped whenever the serialized format or BK-tree
// structure changes, so a stale on-disk tree is never loaded.
const treeCacheVersion = 1

// treeCacheKey derives a stable identity from the dictionary's exact contents,
// so a persisted tree is reused only for identical dictionaries. Words are
// sorted and length-framed so different sets can't collide.
func treeCacheKey(dict map[string]struct{}) string {
	words := make([]string, 0, len(dict))
	for w := range dict {
		words = append(words, w)
	}
	sort.Strings(words)
	h := sha256.New()
	var lenBuf [4]byte
	for _, w := range words {
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(w)))
		h.Write(lenBuf[:])
		h.Write([]byte(w))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// treeCachePath returns the cache file path for a dictionary, or "" when no
// user cache directory can be determined or created.
func treeCachePath(dict map[string]struct{}) string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(base, "spellchecker")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ""
	}
	return filepath.Join(dir, fmt.Sprintf("bktree-v%d-%s.gob", treeCacheVersion, treeCacheKey(dict)))
}

// readBKTreeCacheAt decodes a persisted tree from path.
func readBKTreeCacheAt(path string) (*BKTree, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var tree BKTree
	if err := gob.NewDecoder(f).Decode(&tree); err != nil {
		return nil, err
	}
	return &tree, nil
}

// writeBKTreeCacheAt encodes tree to path via a temp file + rename so a crash
// never leaves a partial cache file.
func writeBKTreeCacheAt(path string, tree *BKTree) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bktree-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := gob.NewEncoder(tmp).Encode(tree); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// loadBKTreeCache returns the persisted tree for dict, or nil if none exists
// (or is invalid).
func loadBKTreeCache(dict map[string]struct{}) *BKTree {
	path := treeCachePath(dict)
	if path == "" {
		return nil
	}
	tree, err := readBKTreeCacheAt(path)
	if err != nil {
		return nil
	}
	return tree
}

// storeBKTreeCache writes the tree for dict, best-effort: a failure here (no
// cache dir, no permission, disk full) just means the next run rebuilds.
func storeBKTreeCache(dict map[string]struct{}, tree *BKTree) {
	path := treeCachePath(dict)
	if path == "" {
		return
	}
	_ = writeBKTreeCacheAt(path, tree)
}
