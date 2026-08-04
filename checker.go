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
}

// bkTreeMinDictSize is the dictionary size at or above which the BK-tree is
// used; smaller dictionaries fall back to brute-force generation.
const bkTreeMinDictSize = 100

// NewConcurrentDictionary creates a new dictionary wrapper. The BK-tree is not
// built here: it is deferred until the first Suggest call, so a run that finds
// no typos never pays the build cost.
func NewConcurrentDictionary(dict map[string]struct{}) *ConcurrentDictionary {
	return &ConcurrentDictionary{dict: dict}
}

// tree returns the cached BK-tree, building it once on first access and
// reusing one persisted on disk for identical dictionaries. The build is
// guarded by the mutex so concurrent Suggest calls from workers can't build it
// twice.
func (cd *ConcurrentDictionary) tree() *BKTree {
	cd.mu.Lock()
	defer cd.mu.Unlock()
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
// path) or falls back to brute-force for small dictionaries.
func (cd *ConcurrentDictionary) Suggest(word string) []string {
	if len(word) > maxSuggestionWordLength {
		return nil
	}
	if tree := cd.tree(); tree != nil {
		return rankSuggestions(tree.Search(strings.ToLower(word), levenshteinThreshold), word)
	}
	return simpleGenerateSuggestions(word, cd.dict)
}

type MisspelledWord struct {
	Word        string
	LineNumber  int
	Column      int
	Suggestions []string
}

type CheckResult struct {
	FilePath string
	Typos    []MisspelledWord
	Err      error
}

func runConcurrentChecker(rootPath string, dictionary map[string]struct{}, excludePatterns []string, verbose bool) (map[string][]MisspelledWord, error) {
	return runConcurrentCheckerWithDict(rootPath, NewConcurrentDictionary(dictionary), excludePatterns, verbose)
}

// runConcurrentCheckerWithDict is the core scanner using an already-built
// ConcurrentDictionary, so the BK-tree is constructed only once per run.
func runConcurrentCheckerWithDict(rootPath string, concurrentDict *ConcurrentDictionary, excludePatterns []string, verbose bool) (map[string][]MisspelledWord, error) {
	// Phase 1: Collect all file paths (filters applied)
	allFiles, err := collectFiles(rootPath, excludePatterns, verbose)
	if err != nil {
		return nil, err
	}
	if len(allFiles) == 0 {
		return make(map[string][]MisspelledWord), nil
	}

	totalFiles := len(allFiles)
	numWorkers := runtime.NumCPU()
	jobBuf := numWorkers * 10
	if jobBuf < 100 {
		jobBuf = 100
	}
	jobs := make(chan string, jobBuf)
	results := make(chan CheckResult, jobBuf)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(&wg, jobs, results, concurrentDict)
	}

	// Sender goroutine: pushes collected file paths to workers
	go func() {
		for _, path := range allFiles {
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

	allTypos := make(map[string][]MisspelledWord)
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

// defaultExcludes are directories and files skipped even when no --exclude is
// given. They cover VCS metadata, dependency stores, and tool caches that are
// never meant to be spell-checked. User patterns are merged on top.
var defaultExcludes = []string{
	".git", ".hg", ".svn", ".bzr",
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
			fmt.Fprintf(os.Stderr, "Error accessing path %q: %v\n", path, err)
			return err
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

		exclude, err := shouldExclude(path, patterns)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking exclude pattern on %q: %v\n", path, err)
			return nil
		}
		if exclude {
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping excluded file: %s\n", path)
			}
			return nil
		}

		isBinary, err := isLikelyBinary(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking if file is binary %q: %v\n", path, err)
			return nil
		}
		if isBinary {
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping binary file: %s\n", path)
			}
			return nil
		}

		files = append(files, path)
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

	misspelledWords, err := scanForTypos(file, dictionary)
	if err != nil {
		return nil, fmt.Errorf("failed scanning %s: %w", filePath, err)
	}
	return misspelledWords, nil
}

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
		for _, indices := range wordRegex.FindAllStringIndex(line, -1) {
			word := line[indices[0]:indices[1]]
			// Skip tokens that are fragments of identifiers (adjacent to a digit
			// or underscore, e.g. "Mi03x_er" splitting into "Mi"/"er" or "10px"
			// into "px"). Spelling checkers flag prose, not code.
			if isIdentifierFragment(line, indices[0], indices[1]) {
				continue
			}
			if !dictionary.Contains(word) {
				misspelledWords = append(misspelledWords, MisspelledWord{
					Word:        word,
					LineNumber:  lineNumber,
					Column:      utf8.RuneCountInString(line[:indices[0]]) + 1,
					Suggestions: dictionary.Suggest(word),
				})
			}
		}
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
	for _, pattern := range patterns {
		// Normalize trailing slashes so "build/" works like "build" — the
		// README advertises both forms, and filepath.Match would otherwise
		// fail to match a directory whose name has no trailing slash.
		pattern = strings.TrimRight(pattern, `/\`)
		if pattern == "" {
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

// checkStdin processes text input from stdin
func checkStdin(dictionary *ConcurrentDictionary) ([]MisspelledWord, error) {
	misspelledWords, err := scanForTypos(os.Stdin, dictionary)
	if err != nil {
		return nil, fmt.Errorf("error reading stdin: %w", err)
	}
	return misspelledWords, nil
}
