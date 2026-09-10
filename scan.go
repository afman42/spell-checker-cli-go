package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

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

// scanOptions controls per-scan filters applied inside scanForTypos.
type scanOptions struct {
	// MinWordLength skips tokens shorter than this. 0 = check all.
	MinWordLength int
	Verbose       bool
	ChangedLines  map[int]struct{}
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

// scanLineForTypos appends the misspelled words found in a single line to
// misspelled and returns the extended slice. Its sole consumer is the
// streaming scanner scanForTypos, so the tokenize/filter/report loop exists
// in exactly one place.
func scanLineForTypos(line string, lineNumber int, dictionary *ConcurrentDictionary, opts scanOptions, misspelled []MisspelledWord) []MisspelledWord {
	if opts.ChangedLines != nil {
		if _, ok := opts.ChangedLines[lineNumber]; !ok {
			return misspelled
		}
	}
	for _, indices := range wordRegex.FindAllStringIndex(line, -1) {
		word := line[indices[0]:indices[1]]
		// Skip tokens that are fragments of identifiers (adjacent to a digit
		// or underscore, e.g. "Mi03x_er" splitting into "Mi"/"er", or the
		// "px" in "10px"); see isIdentifierFragment for details.
		if isIdentifierFragment(line, indices[0], indices[1]) {
			continue
		}
		if opts.MinWordLength > 0 && utf8.RuneCountInString(word) < opts.MinWordLength {
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

func scanForTypos(r io.Reader, dictionary *ConcurrentDictionary, opts scanOptions) ([]MisspelledWord, error) {
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
		misspelledWords = scanLineForTypos(line, lineNumber, dictionary, opts, misspelledWords)
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

// checkFileFunc is the per-file entry point used by the concurrent scanner's
// workers. It is a variable so tests can inject failures to exercise error
// aggregation without touching the file system; production code only ever sees
// checkFile.
var checkFileFunc = checkFile

func checkFile(filePath string, dictionary *ConcurrentDictionary) ([]MisspelledWord, error) {
	return checkFileWithOptions(filePath, dictionary, scanOptions{})
}

func checkFileWithOptions(filePath string, dictionary *ConcurrentDictionary, opts scanOptions) ([]MisspelledWord, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", filePath, err)
	}
	defer file.Close()
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
				misspelled = scanLineForTypos(l, lineNumber, dictionary, opts, misspelled)
			}
		}
		return misspelled, nil
	}
	misspelledWords, err := scanForTypos(file, dictionary, opts)
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
func checkStdin(r io.Reader, dictionary *ConcurrentDictionary, opts scanOptions) ([]MisspelledWord, error) {
	misspelledWords, err := scanForTypos(r, dictionary, opts)
	if err != nil {
		return nil, fmt.Errorf("error reading stdin: %w", err)
	}
	return misspelledWords, nil
}
