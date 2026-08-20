package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestConfigValidate covers the edge cases of Config.Validate: every valid
// format value (including auto-detect ""), invalid formats, and the
// --dry-run/--fix dependency.
func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"empty format (auto-detect)", Config{Format: FormatAuto}, false},
		{"txt format", Config{Format: FormatText}, false},
		{"html format", Config{Format: FormatHTML}, false},
		{"json format", Config{Format: FormatJSON}, false},
		{"invalid format", Config{Format: OutputFormat("xml")}, true},
		{"uppercase format is invalid", Config{Format: OutputFormat("JSON")}, true},
		{"fix without dry-run", Config{Fix: true}, false},
		{"fix with dry-run", Config{Fix: true, DryRun: true}, false},
		{"dry-run without fix", Config{DryRun: true}, true},
		{"dry-run with invalid format reports format first", Config{Format: OutputFormat("nope"), DryRun: true}, true},
		{"fully zero config is valid", Config{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestConfigValidateDryRunMessage verifies the specific guidance returned when
// --dry-run is used without --fix.
func TestConfigValidateDryRunMessage(t *testing.T) {
	cfg := Config{DryRun: true}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for --dry-run without --fix, got nil")
	}
	if got := err.Error(); got != "--dry-run requires --fix" {
		t.Errorf("unexpected error message: %q", got)
	}
}

// TestFindConfigFile verifies the YAML config-file search: it finds
// spellchecker.yaml and .yml, and returns "" when no config is present.
func TestFindConfigFile(t *testing.T) {
	dir := t.TempDir()

	// No config file present.
	if got := findConfigFile([]string{dir}); got != "" {
		t.Errorf("expected empty path when no config exists, got %s", got)
	}

	// .yaml extension.
	yamlPath := filepath.Join(dir, "spellchecker.yaml")
	if err := os.WriteFile(yamlPath, []byte("format: json\n"), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	if got := findConfigFile([]string{dir}); got != yamlPath {
		t.Errorf("expected %s, got %s", yamlPath, got)
	}

	// .yml takes a back seat to .yaml in the same dir (already found above).
	// In a fresh dir, .yml should still be discovered.
	dir2 := t.TempDir()
	ymlPath := filepath.Join(dir2, "spellchecker.yml")
	if err := os.WriteFile(ymlPath, []byte("format: json\n"), 0644); err != nil {
		t.Fatalf("write yml: %v", err)
	}
	if got := findConfigFile([]string{dir2}); got != ymlPath {
		t.Errorf("expected %s, got %s", ymlPath, got)
	}

	// First search dir wins over later ones.
	dir3 := t.TempDir()
	dir4 := t.TempDir()
	first := filepath.Join(dir3, "spellchecker.yaml")
	os.WriteFile(first, []byte("format: json\n"), 0644)
	os.WriteFile(filepath.Join(dir4, "spellchecker.yaml"), []byte("format: html\n"), 0644)
	if got := findConfigFile([]string{dir3, dir4}); got != first {
		t.Errorf("expected first dir to win, got %s", got)
	}
}

// TestConfigYAMLUnmarshal verifies the Config struct parses the YAML shape
// documented in the README (including the hyphenated personal-dictionary key).
func TestConfigYAMLUnmarshal(t *testing.T) {
	src := []byte(`exclude:
  - "*.log"
  - "build/"
personal-dictionary: ".project-words.txt"
format: "html"
output: "./reports/"
verbose: true
fix: false
`)
	var cfg Config
	if err := yaml.Unmarshal(src, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.Exclude) != 2 || cfg.Exclude[0] != "*.log" || cfg.Exclude[1] != "build/" {
		t.Errorf("Exclude mismatch: %#v", cfg.Exclude)
	}
	if cfg.PersonalDictionary != ".project-words.txt" {
		t.Errorf("PersonalDictionary = %q", cfg.PersonalDictionary)
	}
	if cfg.Format != FormatHTML {
		t.Errorf("Format = %q, want html", cfg.Format)
	}
	if cfg.Output != "./reports/" {
		t.Errorf("Output = %q", cfg.Output)
	}
	if !cfg.Verbose {
		t.Error("Verbose should be true")
	}
	if cfg.Fix {
		t.Error("Fix should be false")
	}
}

// quietRun runs the CLI with output redirected to temp files so tests don't
// pollute the test log, and returns the exit code.
func quietRun(t *testing.T, args ...string) int {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	outF, err := os.CreateTemp(t.TempDir(), "stdout-")
	if err != nil {
		t.Fatalf("temp stdout: %v", err)
	}
	errF, err := os.CreateTemp(t.TempDir(), "stderr-")
	if err != nil {
		t.Fatalf("temp stderr: %v", err)
	}
	os.Stdout, os.Stderr = outF, errF
	defer func() {
		os.Stdout, os.Stderr = origOut, origErr
		outF.Close()
		errF.Close()
	}()
	return run(args)
}

// writeDict writes a tiny CSV dictionary (header + words) for fast run() tests.
func writeDict(t *testing.T, words ...string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "dict.csv")
	var sb strings.Builder
	sb.WriteString("word\n")
	for _, w := range words {
		sb.WriteString(w + "\n")
	}
	if err := os.WriteFile(f, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("write dict: %v", err)
	}
	return f
}

// TestRunExitCodes verifies the CLI exit contract end to end: clean = 0,
// typos found = 1.
func TestRunExitCodes(t *testing.T) {
	dict := writeDict(t, "hello", "world")
	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.txt")
	if err := os.WriteFile(clean, []byte("hello world\n"), 0644); err != nil {
		t.Fatalf("write clean: %v", err)
	}
	typo := filepath.Join(dir, "typo.txt")
	if err := os.WriteFile(typo, []byte("hello wrld\n"), 0644); err != nil {
		t.Fatalf("write typo: %v", err)
	}

	if code := quietRun(t, "--dict", dict, clean); code != exitOK {
		t.Errorf("clean file: got exit %d, want %d", code, exitOK)
	}
	if code := quietRun(t, "--dict", dict, typo); code != exitTypos {
		t.Errorf("typo file: got exit %d, want %d", code, exitTypos)
	}
}

// TestRunUsageErrors verifies config/usage mistakes exit with 2 (error), not 1.
func TestRunUsageErrors(t *testing.T) {
	dict := writeDict(t, "hello")

	if code := quietRun(t, "--dict", dict); code != exitError {
		t.Errorf("no path: got exit %d, want %d", code, exitError)
	}
	if code := quietRun(t, "--dict", dict, "a.txt", "b.txt"); code != exitError {
		t.Errorf("two paths: got exit %d, want %d", code, exitError)
	}
	if code := quietRun(t, "--bogus-flag"); code != exitError {
		t.Errorf("unknown flag: got exit %d, want %d", code, exitError)
	}
	if code := quietRun(t, "--dry-run", "a.txt"); code != exitError {
		t.Errorf("dry-run without fix: got exit %d, want %d", code, exitError)
	}
	if code := quietRun(t, "--dict", dict, filepath.Join(t.TempDir(), "missing")); code != exitError {
		t.Errorf("missing path: got exit %d, want %d", code, exitError)
	}
}

// TestRunFixMode verifies --fix rewrites the top suggestion and exits 0 when
// nothing is skipped.
func TestRunFixMode(t *testing.T) {
	dict := writeDict(t, "hello", "world")
	path := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(path, []byte("hello wrld\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if code := quietRun(t, "--dict", dict, "--fix", path); code != exitOK {
		t.Fatalf("fix: got exit %d, want %d", code, exitOK)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello world\n" {
		t.Errorf("unexpected fixed content: %q", string(got))
	}
}

// TestRunVersionFlag verifies --version prints version and exits 0 without
// requiring a path argument or building the dictionary.
func TestRunVersionFlag(t *testing.T) {
	if code := run([]string{"--version"}); code != exitOK {
		t.Errorf("run([--version]) = %d, want %d", code, exitOK)
	}
}

// TestRunCleanStdinExitCode verifies that clean stdin (no typos) exits 0,
// not 1. Before the fix, stdin always built a 1-key map even with zero typos,
// causing a false "typos found" and wrong exit code.
func TestRunCleanStdinExitCode(t *testing.T) {
	dict := writeDict(t, "hello", "world", "this", "is", "fine")

	// Temporarily replace stdin with clean text.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin; r.Close(); w.Close() }()

	go func() {
		w.Write([]byte("hello world this is fine\n"))
		w.Close()
	}()

	if code := quietRun(t, "--dict", dict, "-"); code != exitOK {
		t.Errorf("clean stdin: got exit %d, want %d", code, exitOK)
	}
}

// TestRunGitDiffNotInRepoExitCode verifies that --git-diff outside a git repo
// exits with exitError (2), not exitTypos (1). A git failure is a tooling
// error, not a spelling problem.
func TestRunGitDiffNotInRepoExitCode(t *testing.T) {
	dict := writeDict(t, "hello")

	// Run in a temp dir that is NOT a git repo.
	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.txt")
	if err := os.WriteFile(clean, []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// We need to chdir to the temp dir so git-diff fails there.
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origWd)

	if code := quietRun(t, "--dict", dict, "--git-diff", "HEAD", "."); code != exitError {
		t.Errorf("git-diff not in repo: got exit %d, want %d (exitError)", code, exitError)
	}
}
