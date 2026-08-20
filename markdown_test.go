package main

import (
	"strings"
	"testing"
)

// TestIsMarkdownExt verifies extension detection is case-insensitive and
// matches .md and .markdown only.
func TestIsMarkdownExt(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"lower md", "notes.md", true},
		{"upper MD", "README.MD", true},
		{"markdown", "doc.markdown", true},
		{"txt", "notes.txt", false},
		{"no ext", "Makefile", false},
		{"dotfile", ".gitignore", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isMarkdownExt(c.path); got != c.want {
				t.Errorf("isMarkdownExt(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

// TestScanMarkdownLines verifies prose-only extraction: YAML frontmatter,
// fenced code blocks, inline code spans, link destinations, and bare URLs are
// stripped while real prose lines survive with their line positions.
func TestScanMarkdownLines(t *testing.T) {
	input := []byte("---\ntitle: My Post\n---\n\nHello wrld here.\n\n```go\nfunc wrld() {}\n```\n\nInline `code wrld` and a link [text](http://example.com/api/v2/users).\n\nhttps://example.com/bare-url\n\nFinal line frm.\n")
	got := scanMarkdownLines(input)

	join := strings.Join(got, "\n")
	// Prose survives.
	if !strings.Contains(join, "Hello wrld here.") {
		t.Errorf("prose line missing:\n%s", join)
	}
	if !strings.Contains(join, "Final line frm.") {
		t.Errorf("last prose line missing:\n%s", join)
	}
	// Frontmatter, code fences, inline code, and URLs are stripped.
	for _, banned := range []string{"title:", "func wrld", "code wrld", "http://example.com", "bare-url"} {
		if strings.Contains(join, banned) {
			t.Errorf("noise not stripped, contains %q:\n%s", banned, join)
		}
	}
	// Link text survives, its destination is dropped.
	if !strings.Contains(join, "link [text]") {
		t.Errorf("link text should survive (dest stripped):\n%s", join)
	}
}
