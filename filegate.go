package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// wordRegex tokenizes words. It matches Unicode letters, contractions
	// (don't, it's, it’s), and hyphenated words (state-of-the-art). Leading or
	// trailing apostrophes/quotes are not captured, and runs of only
	// punctuation produce no match.
	wordRegex = regexp.MustCompile(`\p{L}+(?:['’\-]\p{L}+)*`)
)

// defaultExcludes are directories and files skipped even when no --exclude is
// given. They cover VCS metadata, dependency stores, and tool caches that are
// never meant to be spell-checked. User patterns are merged on top.
var defaultExcludes = []string{
	".git", ".hg", ".svn", ".bzr",
	// Tool-local config consumed by the checker itself; never spell-check.
	".spellignore", ".spellcheckerrc.yaml", ".spellcheckerrc.yml",
	"node_modules", "bower_components",
	".venv", "venv", "virtualenv",
	"_vendor", "vendor",
	"__pycache__", ".tox", ".nox", ".ipynb_checkpoints",
	".pytest_cache", ".mypy_cache", ".ruff_cache",
	".gradle", ".cargo", ".terraform", ".serverless", ".turbo",
	".cache", ".idea", ".vscode", ".next", ".nuxt", ".svelte-kit",
}

// mergeDefaultExcludes appends the built-in excludes to any user-provided ones.
func mergeDefaultExcludes(patterns []string) []string {
	merged := make([]string, 0, len(defaultExcludes)+len(patterns))
	merged = append(merged, defaultExcludes...)
	merged = append(merged, patterns...)
	return merged
}
func shouldExclude(filePath string, patterns []string) (bool, error) {
	fileName := filepath.Base(filePath)
	// Normalize the file path to forward slashes for consistent prefix
	// matching against path-glob patterns (e.g. "third_party/**").
	relPath := filepath.ToSlash(filePath)
	for _, pattern := range patterns {
		// Normalize trailing slashes so "build/" works like "build" — the
		// README advertises both forms, and filepath.Match would otherwise
		// fail to match a directory whose name has no trailing slash.
		pattern = strings.TrimRight(pattern, `/\`)
		if pattern == "" {
			continue
		}
		// Patterns containing "/" match against the full relative path so
		// "third_party/**" or "src/generated/*" work as documented. Patterns
		// without "/" keep the basename match (backward compatible).
		if strings.Contains(pattern, "/") {
			// Handle "**" as a recursive prefix match, since filepath.Match
			// treats * as single-component only. "third_party/**" should
			// match any file under third_party/ at any depth.
			if strings.HasSuffix(pattern, "/**") {
				prefix := strings.TrimSuffix(pattern, "/**")
				if relPath == prefix || strings.HasPrefix(relPath, prefix+"/") {
					return true, nil
				}
				cleanRel := strings.TrimPrefix(relPath, "./")
				if cleanRel == prefix || strings.HasPrefix(cleanRel, prefix+"/") {
					return true, nil
				}
				continue
			}
			matched, err := filepath.Match(pattern, relPath)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
			// Also try matching the pattern against the path after
			// stripping any leading "./" from the file path.
			cleanRel := strings.TrimPrefix(relPath, "./")
			if cleanRel != relPath {
				matched, err = filepath.Match(pattern, cleanRel)
				if err != nil {
					return false, err
				}
				if matched {
					return true, nil
				}
			}
			continue
		}
		matched, err := filepath.Match(pattern, fileName)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

// binaryExtensions are file types that are almost never written as prose and
// would otherwise produce a stream of nonsense typos when scanned as text.
var binaryExtensions = map[string]struct{}{
	".pdf": {}, ".epub": {}, ".doc": {}, ".docx": {}, ".xls": {}, ".xlsx": {}, ".ppt": {}, ".pptx": {},
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".bmp": {}, ".webp": {}, ".tiff": {}, ".tif": {},
	".ico": {}, ".avif": {}, ".heic": {},
	".zip": {}, ".gz": {}, ".bz2": {}, ".xz": {}, ".zst": {}, ".7z": {}, ".rar": {}, ".tar": {},
	".exe": {}, ".msi": {}, ".dll": {}, ".so": {}, ".dylib": {}, ".a": {}, ".o": {}, ".class": {}, ".jar": {},
	".pyc": {}, ".pyo": {}, ".wasm": {}, ".woff": {}, ".woff2": {}, ".ttf": {}, ".otf": {}, ".eot": {},
	".mp3": {}, ".wav": {}, ".flac": {}, ".ogg": {}, ".mp4": {}, ".mov": {}, ".avi": {}, ".mkv": {},
	".sqlite": {}, ".db": {}, ".parquet": {}, ".bin": {},
}

func isLikelyBinary(filePath string) (bool, error) {
	if ext := strings.ToLower(filepath.Ext(filePath)); ext != "" {
		if _, ok := binaryExtensions[ext]; ok {
			return true, nil
		}
	}

	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false, err
	}
	buffer = buffer[:n]
	// NUL is the classic text/binary discriminator.
	if bytes.Contains(buffer, []byte{0}) {
		return true, nil
	}
	// Beyond common whitespace, a run of control bytes usually means the file
	// is binary (this catches UTF-16 and other encodings that skip the NUL
	// check or embed formatting escapes).
	controls := 0
	for _, b := range buffer {
		switch b {
		case 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x1b:
			continue
		}
		if b < 0x20 || b == 0x7f {
			controls++
		}
	}
	return controls > 3, nil
}

// fileGate classifies the outcome of the shared exclude + binary checks so
// every scanner entry point filters files through one implementation.
type fileGate int

const (
	fileOK         fileGate = iota // file passed both checks and should be scanned
	fileExcludeErr                 // exclude-pattern match failed
	fileExcluded                   // file matched an exclude pattern
	fileBinaryErr                  // binary sniff failed
	fileBinary                     // file looks binary
)

// classifyFile applies the exclude patterns and the binary sniff to path.
// It performs no output; each caller keeps its own stderr policy per gate,
// so wording and verbose gating stay exactly where they belong.
func classifyFile(path string, patterns []string) (fileGate, error) {
	excluded, err := shouldExclude(path, patterns)
	if err != nil {
		return fileExcludeErr, err
	}
	if excluded {
		return fileExcluded, nil
	}
	isBinary, err := isLikelyBinary(path)
	if err != nil {
		return fileBinaryErr, err
	}
	if isBinary {
		return fileBinary, nil
	}
	return fileOK, nil
}
