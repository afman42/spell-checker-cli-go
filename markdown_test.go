package main

import (
	"os"
	"path/filepath"
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

// TestScanMarkdownLines verifies prose-only extraction through checkFile on a
// real temp .md file: YAML frontmatter, fenced code blocks, inline code
// spans, link destinations, and bare URLs are stripped while real prose
// survives with its line positions intact.
func TestScanMarkdownLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.md")
	input := "---\ntitle: My Post\n---\n\nHello wrld here.\n\n```go\nfunc wrld() {}\n```\n\nInline `code wrld` and a link [text](http://example.com/api/v2/users).\n\nhttps://example.com/bare-url\n\nFinal line frm.\n"
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := checkFile(path, NewConcurrentDictionary(map[string]struct{}{}))
	if err != nil {
		t.Fatalf("checkFile(%s) error: %v", path, err)
	}

	words := make([]string, len(got))
	var wrld *MisspelledWord
	for i, m := range got {
		words[i] = m.Word
		if m.Word == "wrld" && wrld == nil {
			wrld = &got[i]
		}
	}
	join := strings.Join(words, "\n")

	// Prose survives.
	for _, want := range []string{"Hello", "here", "frm", "text"} {
		if !strings.Contains(join, want) {
			t.Errorf("prose word %q missing from result:\n%s", want, join)
		}
	}
	// Frontmatter, code fences, inline code, and URLs are stripped.
	for _, banned := range []string{"title", "My", "Post", "func", "code", "users", "https", "example", "bare-url"} {
		if strings.Contains(join, banned) {
			t.Errorf("noise not stripped, found %q:\n%s", banned, join)
		}
	}
	// Line numbers stay accurate against the source file: wrld is on line 5,
	// column 7 of the original input.
	if wrld == nil {
		t.Fatalf("expected misspelling %q, got:\n%s", "wrld", join)
	}
	if wrld.LineNumber != 5 || wrld.Column != 7 {
		t.Errorf("wrld at line/col %d/%d, want 5/7", wrld.LineNumber, wrld.Column)
	}
}
