package main

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// markdownCodeFenceRE matches ``` or ~~~ fenced code blocks (with optional
// language tag and optional indent up to 3 spaces, per CommonMark).
var markdownCodeFenceRE = regexp.MustCompile(`(?m)^ {0,3}(` + "```" + `|~~~)`)

// markdownInlineCodeRE matches `code` inline code spans.
var markdownInlineCodeRE = regexp.MustCompile("`[^`]+`")

// markdownLinkDestRE matches the destination part of a markdown link: [text](dest)
// so we can strip the URL (which may contain non-words like /api/v2/users).
var markdownLinkDestRE = regexp.MustCompile(`\]\(([^)]*)\)`)

// markdownURLRE matches bare http/https/ftp URLs in prose.
var markdownURLRE = regexp.MustCompile(`\b(https?|ftp)://[^\s)]+`)

// stripMarkdownNoise removes tokens from a markdown line that should not be
// spell-checked: fenced code block delimiters (the whole block is skipped by
// scanMarkdownLines), inline code spans, link destinations, and bare URLs.
// The replacement is a space so rune offsets in the original line are
// preserved, keeping column numbers accurate.
func stripMarkdownNoise(line string) string {
	// Inline code spans → spaces (preserve length where possible).
	line = markdownInlineCodeRE.ReplaceAllStringFunc(line, func(m string) string {
		return spacesOfLen(m)
	})
	// Link destinations → spaces, keep the link text (including its
	// closing bracket, which the match starts at).
	line = markdownLinkDestRE.ReplaceAllStringFunc(line, func(m string) string {
		return "]" + spacesOfLen(m[1:])
	})
	// Bare URLs → spaces (preserve rune offsets like the other passes).
	line = markdownURLRE.ReplaceAllStringFunc(line, func(m string) string {
		return spacesOfLen(m)
	})
	return line
}

// spacesOfLen returns a string of n spaces, capped at the rune length of s.
func spacesOfLen(s string) string {
	n := utf8.RuneCountInString(s)
	return strings.Repeat(" ", n)
}

// mdFrontmatterDelimRE detects the opening --- of YAML frontmatter at the very
// start of a file.  The fence-strip logic handles the closing ---.
var mdFrontmatterDelimRE = regexp.MustCompile(`^---\s*$`)

// scanMarkdownLines filters a raw markdown file's content: it drops code
// fence blocks and returns the remaining lines, preserving line numbers so
// reported positions match the source.
//
// Implementation: iterate lines, track inCodeFence. When inside a fence,
// skip the line (return "" so scanLinesForTypos sees nothing). YAML
// frontmatter (opening --- at line 1) is also skipped until the closing ---.
// The returned slice has empty strings for skipped lines to preserve count.
func scanMarkdownLines(raw []byte) []string {
	lines := strings.Split(string(raw), "\n")
	out := make([]string, len(lines))
	inCodeFence := false
	inFrontmatter := false
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if i == 0 && mdFrontmatterDelimRE.MatchString(trimmed) {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if mdFrontmatterDelimRE.MatchString(trimmed) {
				inFrontmatter = false
			}
			continue
		}
		if markdownCodeFenceRE.MatchString(trimmed) {
			inCodeFence = !inCodeFence
			continue
		}
		if inCodeFence {
			continue
		}
		out[i] = stripMarkdownNoise(trimmed)
	}
	return out
}
