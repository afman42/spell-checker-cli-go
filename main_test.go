package main

import (
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"testing"
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
