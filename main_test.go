package main

import "testing"

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
