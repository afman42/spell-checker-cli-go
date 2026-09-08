package main

import (
	"reflect"
	"testing"
)

func TestParseChangedLinesEmpty(t *testing.T) {
	if got := parseChangedLines(""); got != nil {
		t.Errorf("empty input: got %v, want nil", got)
	}
	if got := parseChangedLines("   \n\n  "); got != nil {
		t.Errorf("whitespace-only input: got %v, want nil", got)
	}
}

func TestParseChangedLinesSingleHunk(t *testing.T) {
	input := `diff --git a/foo.txt b/foo.txt
index 1234567..89abcde 100644
--- a/foo.txt
+++ b/foo.txt
@@ -1,3 +1,5 @@
+line1
+added1
+line2
+added2
+line3`
	got := parseChangedLines(input)
	want := ChangedLines{"foo.txt": {1: {}, 2: {}, 3: {}, 4: {}, 5: {}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("single hunk: got %v, want %v", got, want)
	}
}

func TestParseChangedLinesMultipleHunks(t *testing.T) {
	input := `--- a/bar.txt
+++ b/bar.txt
@@ -1 +1,2 @@
+first
@@ -10,3 +11,4 @@
+second`
	got := parseChangedLines(input)
	want := ChangedLines{"bar.txt": {1: {}, 2: {}, 11: {}, 12: {}, 13: {}, 14: {}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("multiple hunks: got %v, want %v", got, want)
	}
}

func TestParseChangedLinesMultipleFiles(t *testing.T) {
	input := `--- a/a.txt
+++ b/a.txt
@@ -5 +5,2 @@
+hello
--- a/b.txt
+++ b/b.txt
@@ -10 +10,2 @@
+world`
	got := parseChangedLines(input)
	want := ChangedLines{
		"a.txt": {5: {}, 6: {}},
		"b.txt": {10: {}, 11: {}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("multiple files: got %v, want %v", got, want)
	}
}

func TestParseChangedLinesZeroCount(t *testing.T) {
	input := `--- a/d.txt
+++ b/d.txt
@@ -1,5 +0,0 @@
-old1
-old2`
	got := parseChangedLines(input)
	if len(got) != 0 {
		t.Errorf("zero count: got %v, want empty map", got)
	}
}

func TestParseChangedLinesNoBPrefix(t *testing.T) {
	input := `+++ not_a_header
@@ -1 +1,2 @@
+line`
	got := parseChangedLines(input)
	if len(got) != 0 {
		t.Errorf("no b/ prefix: got %v, want empty", got)
	}
}

func TestParseChangedLinesPlusWithoutHunk(t *testing.T) {
	input := `--- a/empty.txt
+++ b/empty.txt`
	got := parseChangedLines(input)
	if len(got) != 0 {
		t.Errorf("no hunk: got %v, want empty", got)
	}
}

func TestParseChangedLinesDefaultCount(t *testing.T) {
	input := `--- a/d.txt
+++ b/d.txt
@@ -5 +6 @@
+only`
	got := parseChangedLines(input)
	want := ChangedLines{"d.txt": {6: {}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("default count: got %v, want %v", got, want)
	}
}
