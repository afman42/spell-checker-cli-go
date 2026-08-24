package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	// wordRegex tokenizes words. It matches Unicode letters, contractions
	// (don't, it's, it’s), and hyphenated words (state-of-the-art). Leading or
	// trailing apostrophes/quotes are not captured, and runs of only
	// punctuation produce no match.
	wordRegex = regexp.MustCompile(`\p{L}+(?:['’\-]\p{L}+)*`)
)

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
// reusing one persisted on disk for identical dictionaries. Caller must hold
// cd.mu.
func (cd *ConcurrentDictionary) treeLocked() *BKTree {
	if cd.bkTree == nil && len(cd.dict) >= bkTreeMinDictSize {
		cd.bkTree = loadBKTreeCache(cd.dict)
		if cd.bkTree == nil {
			cd.bkTree = NewBKTree(cd.dict)
			storeBKTreeCache(cd.dict, cd.bkTree)
		}
	}
	return cd.bkTree
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
	// Build tree under lock (build is serialized), then release so concurrent
	// workers can search in parallel — the tree is immutable after construction.
	_ = cd.treeLocked()
	cd.mu.Unlock()

	var sug []string
	// treeLocked sets cd.bkTree under lock; the tree is immutable after that,
	// so Search runs lock-free and concurrent workers share the read-only tree.
	if tree := cd.bkTree; tree != nil {
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

type MisspelledWord struct {
	Word        string
	LineNumber  int
	Column      int
	Suggestions []string
}

type CheckResults = map[string][]MisspelledWord

type CheckResult struct {
	FilePath string
	Typos    []MisspelledWord
	Err      error
}

// runConcurrentCheckerWithDict is the core scanner using an already-built
// ConcurrentDictionary, so the BK-tree is constructed only once per run.
func runConcurrentCheckerWithDict(rootPath string, concurrentDict *ConcurrentDictionary, excludePatterns []string, verbose bool) (CheckResults, error) {
	// Phase 1: Collect all file paths (filters applied)
	allFiles, err := collectFiles(rootPath, excludePatterns, verbose)
	if err != nil {
		return nil, err
	}
	return runCheckerOnFiles(allFiles, concurrentDict, verbose)
}

// runCheckerOnFiles runs the worker pool over an already-collected list of
// files. Shared by the directory walk (runConcurrentCheckerWithDict) and the
// --git-diff path (runGitDiffChecker), which supplies its own file list.
func runCheckerOnFiles(files []string, concurrentDict *ConcurrentDictionary, verbose bool) (CheckResults, error) {
	if len(files) == 0 {
		return make(CheckResults), nil
	}

	totalFiles := len(files)
	numWorkers := runtime.NumCPU()
	jobBuf := numWorkers * 10
	if jobBuf < 100 {
		jobBuf = 100
	}
	jobs := make(chan string, jobBuf)
	results := make(chan CheckResult, jobBuf)

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go worker(&wg, jobs, results, concurrentDict)
	}

	// Sender goroutine: pushes collected file paths to workers
	go func() {
		for _, path := range files {
			jobs <- path
		}
		close(jobs)
	}()

	// Sink goroutine: closes results when all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Progress bar (stderr, terminal only)
	showProgress := totalFiles > 1 && isStderrTerminal()
	var processed atomic.Int64 // total results received (success + error)
	var errored atomic.Int64   // results that had errors
	progressDone := make(chan struct{})
	if showProgress {
		go renderProgressBar(totalFiles, &processed, &errored, progressDone)
	}

	allTypos := make(CheckResults)
	var errs []error
	for result := range results {
		processed.Add(1)
		if result.Err != nil {
			errored.Add(1)
			errs = append(errs, result.Err)
			continue
		}
		if len(result.Typos) > 0 {
			allTypos[result.FilePath] = result.Typos
		}
	}

	close(progressDone)
	if showProgress {
		fmt.Fprint(os.Stderr, "\r"+strings.Repeat(" ", 80)+"\r")
	}

	// Surface every worker failure, not just the first. Successful files are
	// still returned so the user gets a report even when some files error out.
	if len(errs) > 0 {
		return allTypos, errors.Join(errs...)
	}
	return allTypos, nil
}

// runGitDiffChecker restricts the scan to files changed relative to a git ref
// (--git-diff). The ref is resolved by gitDiffFiles; the resulting file list is
// filtered by the same exclude + binary rules as a directory walk so ignored
// or binary files are still skipped. Runs the same worker pool as the walk.
func runGitDiffChecker(ref string, rootPath string, concurrentDict *ConcurrentDictionary, excludePatterns []string, verbose bool) (CheckResults, error) {
	raw, err := gitDiffFiles(ref)
	if err != nil {
		return nil, fmt.Errorf("git-diff: %w", err)
	}
	// Restrict git-diff results to rootPath: normalize both sides to
	// forward slashes and clean them (dropping any trailing slash) for a
	// clean exact-or-prefix match.
	rootPath = filepath.ToSlash(filepath.Clean(rootPath))
	if rootPath != "." {
		filtered := raw[:0]
		for _, p := range raw {
			pNorm := filepath.ToSlash(filepath.Clean(p))
			if pNorm == rootPath || strings.HasPrefix(pNorm, rootPath+"/") {
				filtered = append(filtered, p)
			}
		}
		raw = filtered
	}
	patterns := mergeDefaultExcludes(excludePatterns)
	var files []string
	for _, p := range raw {
		gate, err := classifyFile(p, patterns)
		switch gate {
		case fileExcludeErr:
			if verbose {
				fmt.Fprintf(os.Stderr, "Error checking exclude pattern on %q: %v\n", p, err)
			}
		case fileExcluded:
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping excluded file: %s\n", p)
			}
		case fileBinaryErr:
			if verbose {
				fmt.Fprintf(os.Stderr, "Error checking if file is binary %q: %v\n", p, err)
			}
		case fileBinary:
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping binary file: %s\n", p)
			}
		default:
			files = append(files, p)
		}
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "git-diff: %d file(s) to check\n", len(files))
	}
	return runCheckerOnFiles(files, concurrentDict, verbose)
}

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

// collectFiles walks rootPath and returns a list of file paths that should be
// checked (excludes, binary files, and directories are filtered out).
func collectFiles(rootPath string, excludePatterns []string, verbose bool) ([]string, error) {
	var files []string
	patterns := mergeDefaultExcludes(excludePatterns)
	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// A failed root means nothing can be scanned: report it. A failed
			// subdirectory is just one inaccessible subtree — skip it and
			// keep scanning the rest, matching the per-file error aggregation
			// that already keeps partial results.
			if path == rootPath {
				return err
			}
			fmt.Fprintf(os.Stderr, "Error accessing path %q (skipping): %v\n", path, err)
			return nil
		}

		if info.IsDir() {
			exclude, err := shouldExclude(path, patterns)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error checking exclude pattern on directory %q: %v\n", path, err)
				return nil
			}
			if exclude {
				if verbose {
					fmt.Fprintf(os.Stderr, "Skipping excluded directory: %s\n", path)
				}
				return filepath.SkipDir
			}
			return nil
		}

		gate, err := classifyFile(path, patterns)
		switch gate {
		case fileExcludeErr:
			fmt.Fprintf(os.Stderr, "Error checking exclude pattern on %q: %v\n", path, err)
		case fileExcluded:
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping excluded file: %s\n", path)
			}
		case fileBinaryErr:
			fmt.Fprintf(os.Stderr, "Error checking if file is binary %q: %v\n", path, err)
		case fileBinary:
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping binary file: %s\n", path)
			}
		default:
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// renderProgressBar prints a live progress bar to stderr until all files are done.
func renderProgressBar(total int, processed, errored *atomic.Int64, done <-chan struct{}) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p := int(processed.Load())
			if p >= total {
				printProgressBar(total, total, int(errored.Load()))
				return
			}
			printProgressBar(p, total, int(errored.Load()))
		case <-done:
			printProgressBar(total, total, int(errored.Load()))
			return
		}
	}
}

func printProgressBar(current, total, errored int) {
	const barWidth = 30
	percent := float64(current) / float64(total) * 100
	filled := int(float64(barWidth) * float64(current) / float64(total))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	errSuffix := ""
	if errored > 0 {
		errSuffix = fmt.Sprintf(" (%d errored)", errored)
	}
	fmt.Fprintf(os.Stderr, "\r  %3.0f%% |%s| %d/%d files%s", percent, bar, current, total, errSuffix)
}

func isStderrTerminal() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// checkFileFunc is the per-file entry point used by the concurrent scanner's
// workers. It is a variable so tests can inject failures to exercise error
// aggregation without touching the file system; production code only ever sees
// checkFile.
var checkFileFunc = checkFile

func worker(wg *sync.WaitGroup, jobs <-chan string, results chan<- CheckResult, dictionary *ConcurrentDictionary) {
	defer wg.Done()
	for path := range jobs {
		typos, err := checkFileFunc(path, dictionary)
		results <- CheckResult{FilePath: path, Typos: typos, Err: err}
	}
}

func checkFile(filePath string, dictionary *ConcurrentDictionary) ([]MisspelledWord, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", filePath, err)
	}
	defer file.Close()

	// Markdown-aware scanning: strip fenced code, inline code, link URLs and
	// YAML frontmatter before tokenizing so prose is checked, code isn't.
	// Streamed through the same bounded lineReader as plain text (1 MiB line
	// cap), so a huge .md file cannot OOM the process.
	if isMarkdownExt(filePath) {
		lr := newLineReader(file, maxLineLen)
		var st mdState
		var misspelled []MisspelledWord
		for {
			line, lineNumber, err := lr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("failed scanning %s: %w", filePath, err)
			}
			if l := scanMarkdownLine(strings.TrimRight(line, "\r\n"), lineNumber, &st); l != "" {
				misspelled = scanLineForTypos(l, lineNumber, dictionary, misspelled)
			}
		}
		return misspelled, nil
	}

	misspelledWords, err := scanForTypos(file, dictionary)
	if err != nil {
		return nil, fmt.Errorf("failed scanning %s: %w", filePath, err)
	}
	return misspelledWords, nil
}

// isMarkdownExt reports whether filePath has a recognised markdown extension.
func isMarkdownExt(filePath string) bool {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// scanOptions controls per-scan filters applied inside scanForTypos. Set once
// per run from main; avoids threading extra params through every call site.
type scanOptions struct {
	// MinWordLength skips tokens shorter than this. 0 = check all.
	MinWordLength int
}

// scanOpts is the active scan configuration, set by run() before scanning.
var scanOpts = scanOptions{}

// scanForTypos reads a stream line by line and returns misspelled words with
// 1-based line and (rune) column positions.
// maxLineLen caps the per-line length processed by scanForTypos. Lines longer
// than this are skipped (content not checked) rather than failing the whole
// file. Matches fixFile's limit so the checker and fixer never disagree on what
// constitutes a "line".
const maxLineLen = 1024 * 1024 // 1 MiB

// lineReader reads a stream line by line, counting newlines so reported line
// numbers always match the source. Lines longer than maxLine cannot be scanned
// as a whole token; their content is streamed to overlong (when set) so a
// caller like fixFile can reproduce the file byte-identically, and never
// returned, so scanForTypos simply skips them instead of failing the scan.
// Memory use is bounded by maxLine regardless of input size.
type lineReader struct {
	br       *bufio.Reader
	maxLine  int
	lineNum  int
	overlong io.Writer // optional sink for content of lines exceeding maxLine
}

func newLineReader(r io.Reader, maxLine int) *lineReader {
	return &lineReader{br: bufio.NewReaderSize(r, 64*1024), maxLine: maxLine}
}

// setOverlong installs an optional sink that receives, verbatim, the full
// content of any line exceeding maxLine (including its terminator).
func (lr *lineReader) setOverlong(w io.Writer) { lr.overlong = w }

// writeSink streams an over-long line's bytes to the installed sink; a no-op
// when no sink is set (scanForTypos simply discards over-long content).
func (lr *lineReader) writeSink(frag []byte) {
	if lr.overlong != nil {
		lr.overlong.Write(frag)
	}
}

// Next returns the next checkable line and its 1-based line number. It returns
// io.EOF when the stream is exhausted. Physical lines longer than maxLine are
// consumed but not returned; their content is streamed to the overlong sink (if
// set) so callers can reproduce the file byte-identically, and the checker
// simply skips them. Memory use is bounded by maxLine regardless of input size.
func (lr *lineReader) Next() (string, int, error) {
nextLine:
	for {
		var line []byte
		over := false

		for {
			frag, err := lr.br.ReadSlice('\n')
			switch {
			case err == bufio.ErrBufferFull:
				if over {
					lr.writeSink(frag)
					continue
				}
				line = append(line, frag...)
				if len(line) > lr.maxLine {
					over = true
					lr.writeSink(line)
					line = nil
				}
			case err == io.EOF:
				if over {
					lr.writeSink(frag)
					lr.lineNum++
					continue nextLine
				}
				line = append(line, frag...)
				if len(line) == 0 {
					return "", 0, io.EOF
				}
				lr.lineNum++
				if len(line) > lr.maxLine {
					lr.writeSink(line)
					continue nextLine
				}
				return string(line), lr.lineNum, nil
			case err != nil:
				return "", 0, err
			default:
				if over {
					lr.writeSink(frag)
					lr.lineNum++
					continue nextLine
				}
				line = append(line, frag...)
				if len(line) > lr.maxLine {
					// Overlong line that terminates with this fragment.
					lr.writeSink(line)
					line = nil
					lr.lineNum++
					continue nextLine
				}
				lr.lineNum++
				return string(line), lr.lineNum, nil
			}
		}
	}
}

// scanLineForTypos appends the misspelled words found in a single line to
// misspelled and returns the extended slice. Its sole consumer is the
// streaming scanner scanForTypos, so the tokenize/filter/report loop exists
// in exactly one place.
func scanLineForTypos(line string, lineNumber int, dictionary *ConcurrentDictionary, misspelled []MisspelledWord) []MisspelledWord {
	for _, indices := range wordRegex.FindAllStringIndex(line, -1) {
		word := line[indices[0]:indices[1]]
		// Skip tokens that are fragments of identifiers (adjacent to a digit
		// or underscore, e.g. "Mi03x_er" splitting into "Mi"/"er", or the
		// "px" in "10px"); see isIdentifierFragment for details.
		if isIdentifierFragment(line, indices[0], indices[1]) {
			continue
		}
		if scanOpts.MinWordLength > 0 && utf8.RuneCountInString(word) < scanOpts.MinWordLength {
			continue
		}
		if !dictionary.Contains(word) {
			misspelled = append(misspelled, MisspelledWord{
				Word:        word,
				LineNumber:  lineNumber,
				Column:      utf8.RuneCountInString(line[:indices[0]]) + 1,
				Suggestions: dictionary.Suggest(word),
			})
		}
	}
	return misspelled
}

func scanForTypos(r io.Reader, dictionary *ConcurrentDictionary) ([]MisspelledWord, error) {
	var misspelledWords []MisspelledWord
	lr := newLineReader(r, maxLineLen)
	for {
		line, lineNumber, err := lr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		misspelledWords = scanLineForTypos(line, lineNumber, dictionary, misspelledWords)
	}
	return misspelledWords, nil
}

// isIdentifierFragment reports whether the word matched at [start,end) in line
// is adjacent to a digit or underscore — a sign it is part of an identifier
// (e.g. the "er" in "Mi03x_er" or the "px" in "10px") rather than prose.
func isIdentifierFragment(line string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(line[:start])
		if r == '_' || unicode.IsDigit(r) {
			return true
		}
	}
	if end < len(line) {
		r, _ := utf8.DecodeRuneInString(line[end:])
		if r == '_' || unicode.IsDigit(r) {
			return true
		}
	}
	return false
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

func checkStdin(r io.Reader, dictionary *ConcurrentDictionary) ([]MisspelledWord, error) {
	misspelledWords, err := scanForTypos(r, dictionary)
	if err != nil {
		return nil, fmt.Errorf("error reading stdin: %w", err)
	}
	return misspelledWords, nil
}
