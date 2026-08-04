package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// OutputFormat represents valid output formats
type OutputFormat string

const (
	FormatText OutputFormat = "txt"
	FormatHTML OutputFormat = "html"
	FormatJSON OutputFormat = "json"
	FormatAuto OutputFormat = "" // Auto-detect from file extension
)

type Config struct {
	// Exclude is a list of file patterns to exclude.
	Exclude []string `yaml:"exclude"`
	// Dictionary is the path to a custom dictionary file.
	Dictionary string `yaml:"dictionary"`
	// PersonalDictionary is the path to a personal word list.
	PersonalDictionary string `yaml:"personal-dictionary"`
	// Format is the output format (txt, html, json).
	Format OutputFormat `yaml:"format"`
	// Verbose enables verbose logging.
	Verbose bool `yaml:"verbose"`
	// Output is the path for the report file or directory.
	Output string `yaml:"output"`
	// Watch enables file watching mode.
	Watch bool `yaml:"watch"`
	// Fix enables auto-correcting typos to their top suggestion.
	Fix bool `yaml:"fix"`
	// DryRun, with Fix, reports changes without writing them.
	DryRun bool `yaml:"dry-run"`
}

// Validate ensures the configuration is valid
func (c *Config) Validate() error {
	switch c.Format {
	case "", FormatText, FormatHTML, FormatJSON:
	default:
		return fmt.Errorf("invalid format: %s, must be 'txt', 'html', or 'json'", c.Format)
	}
	if c.DryRun && !c.Fix {
		return fmt.Errorf("--dry-run requires --fix")
	}
	return nil
}

// configSearchDirs returns the directories searched for spellchecker.yaml,
// in precedence order (cwd first, then the user's config directory).
func configSearchDirs() []string {
	dirs := []string{"."}
	if homeDir, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(homeDir, ".config", "spellchecker"))
	}
	return dirs
}

// findConfigFile returns the path of the first spellchecker.yaml/yml found in
// any of the given search directories, or "" if none exists.
func findConfigFile(dirs []string) string {
	for _, dir := range dirs {
		for _, name := range []string{"spellchecker.yaml", "spellchecker.yml"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// Exit codes: 0 = clean, 1 = typos found (or fixincomplete), 2 = usage/config/
// internal error. 1 vs 2 lets CI distinguish "spelling problems" from "couldn't
// run".
const (
	exitOK    = 0
	exitTypos = 1
	exitError = 2
)

// loadConfig parses flags from args, merges them over any spellchecker.yaml,
// validates the result, and returns the config plus leftover positional args.
// Precedence: Flags > Config File > Defaults.
func loadConfig(args []string) (*Config, []string, error) {
	// --- Define Flags using pflag ---
	fs := pflag.NewFlagSet("spellchecker", pflag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are reported by the caller
	excludeFlag := fs.StringSlice("exclude", nil, "Optional: comma-separated list of file patterns to exclude.")
	dictFlag := fs.String("dict", "", "Optional: path to a custom CSV dictionary file.")
	personalDictFlag := fs.String("personal-dict", "", "Optional: path to a personal dictionary file (one word per line).")
	outputFlag := fs.String("output", "", "Optional: path to an output file or directory (for HTML reports).")
	formatFlag := fs.String("format", "", "Optional: output format (txt, html, json). Overrides filename extension.")
	verboseFlag := fs.Bool("verbose", false, "Enable verbose logging to show skipped files and directories.")
	watchFlag := fs.Bool("watch", false, "Watch directory for changes and re-check files on save.")
	fixFlag := fs.Bool("fix", false, "Rewrite each typo to its top suggestion, in place.")
	dryRunFlag := fs.Bool("dry-run", false, "With --fix: report what would change without writing files.")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}

	// --- Load Config File (YAML) ---
	cfg := &Config{}
	if path := findConfigFile(configSearchDirs()); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("error reading config file: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, nil, fmt.Errorf("error parsing config file: %w", err)
		}
	}

	// --- Apply flags (flags > config > defaults) ---
	// Only override when the flag was explicitly set on the command line.
	if fs.Lookup("exclude").Changed {
		cfg.Exclude = *excludeFlag
	}
	if fs.Lookup("dict").Changed {
		cfg.Dictionary = *dictFlag
	}
	if fs.Lookup("personal-dict").Changed {
		cfg.PersonalDictionary = *personalDictFlag
	}
	if fs.Lookup("output").Changed {
		cfg.Output = *outputFlag
	}
	if fs.Lookup("format").Changed {
		cfg.Format = OutputFormat(*formatFlag)
	}
	if fs.Lookup("verbose").Changed {
		cfg.Verbose = *verboseFlag
	}
	if fs.Lookup("watch").Changed {
		cfg.Watch = *watchFlag
	}
	if fs.Lookup("fix").Changed {
		cfg.Fix = *fixFlag
	}
	if fs.Lookup("dry-run").Changed {
		cfg.DryRun = *dryRunFlag
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("configuration validation error: %w", err)
	}

	positionals := fs.Args()
	if len(positionals) > 1 {
		return nil, nil, fmt.Errorf("expected a single file or directory, got %d paths: %v", len(positionals), positionals)
	}
	return cfg, positionals, nil
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// run executes the CLI with the given arguments and returns the exit code.
func run(args []string) int {
	// --- Load Configuration ---
	cfg, positionals, err := loadConfig(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error loading configuration: %v\n", err)
		return exitError
	}

	dictionary, err := loadDictionary(cfg.Dictionary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error loading dictionary: %v\n", err)
		return exitError
	}
	fmt.Fprintf(os.Stderr, "Successfully loaded %d words.\n", len(dictionary))

	if cfg.PersonalDictionary != "" {
		count, err := loadPersonalDictionary(cfg.PersonalDictionary, dictionary)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading personal dictionary: %v\n", err)
			return exitError
		}
		fmt.Fprintf(os.Stderr, "Successfully loaded and merged %d words from personal dictionary.\n", count)
	}

	if len(positionals) < 1 {
		fmt.Println("Usage: spellchecker [flags] <file_or_directory>")
		return exitError
	}

	path := positionals[0]

	// Watch mode: stay running and re-check files on change
	if cfg.Watch {
		if path == "-" {
			fmt.Fprintln(os.Stderr, "Watch mode does not support stdin.")
			return exitError
		}
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot access path: %v\n", err)
			return exitError
		}
		if !info.IsDir() {
			fmt.Fprintln(os.Stderr, "Watch mode requires a directory path.")
			return exitError
		}
		if err := runWatcher(path, dictionary, cfg.Exclude); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return exitError
		}
		return exitOK
	}

	var allTypos map[string][]MisspelledWord
	var checkErr error
	if path == "-" {
		// Process stdin
		concurrentDict := NewConcurrentDictionary(dictionary)
		typos, err := checkStdin(concurrentDict)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing stdin: %v\n", err)
			return exitError
		}
		allTypos = map[string][]MisspelledWord{"<stdin>": typos}
	} else {
		// Process file or directory. Individual file failures are aggregated
		// into checkErr but successful files still come back in allTypos, so a
		// report is produced for what could be scanned.
		allTypos, checkErr = runConcurrentChecker(path, dictionary, cfg.Exclude, cfg.Verbose)
		if checkErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: some files could not be checked:\n%v\n", checkErr)
		}
	}

	// --- Fix mode: rewrite typos in place instead of producing a report ---
	if cfg.Fix {
		if path == "-" {
			fmt.Fprintln(os.Stderr, "Fix mode does not support stdin.")
			return exitError
		}
		_, skipped, err := runFixer(allTypos, cfg.DryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fixing files: %v\n", err)
			return exitError
		}
		// Dry-run: signal typos via exit code 1 so CI still fails.
		// Real fix: exit 0 only if every typo was correctable. Skipped typos
		// (no suggestion) remain in the files, so CI should fail to surface
		// them for manual review. Unscanned files also fail CI.
		if cfg.DryRun && (len(allTypos) > 0 || checkErr != nil) {
			return exitTypos
		}
		if !cfg.DryRun && (skipped > 0 || checkErr != nil) {
			return exitTypos
		}
		return exitOK
	}

	// --- REVISED OUTPUT LOGIC ---
	// If scanning failed before collecting anything (e.g. a missing root path),
	// the warning above is the report; don't emit a misleading "No typos found".
	if len(allTypos) > 0 || checkErr == nil {
		if cfg.Output == "" {
			// Default case: no output path. Print to stdout in the chosen format
			// (text unless --format json was requested).
			switch strings.ToLower(string(cfg.Format)) {
			case string(FormatJSON):
				if err := generateJSONReport(os.Stdout, allTypos); err != nil {
					fmt.Fprintf(os.Stderr, "Error generating JSON report: %v\n", err)
					return exitError
				}
			default:
				generateTextReport(os.Stdout, allTypos)
			}
		} else {
			// An output path was provided. Determine the format and mode.
			format := strings.ToLower(string(cfg.Format))
			ext := strings.ToLower(filepath.Ext(cfg.Output))

			// Determine if the desired format is HTML.
			isHTML := format == string(FormatHTML) || (format == string(FormatAuto) && ext == ".html")
			isJSON := format == string(FormatJSON) || (format == string(FormatAuto) && ext == ".json")

			// Determine if we should use the multi-file directory mode for HTML.
			// This is triggered if the format is HTML AND the path does not end in ".html".
			isMultiFileDir := isHTML && ext != ".html"

			if isMultiFileDir {
				fmt.Printf("Generating multi-file HTML report in directory: %s\n", cfg.Output)
				if err := generateMultiFileHTMLReport(cfg.Output, allTypos); err != nil {
					fmt.Fprintf(os.Stderr, "Error generating multi-file report: %v\n", err)
					return exitError
				}
			} else {
				// Single-file output for text, JSON, or a specific HTML file.
				file, err := os.Create(cfg.Output)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
					return exitError
				}

				fmt.Printf("Report will be saved to: %s\n", cfg.Output)
				switch {
				case isHTML:
					generateHTMLReport(file, allTypos)
				case isJSON:
					if err := generateJSONReport(file, allTypos); err != nil {
						fmt.Fprintf(os.Stderr, "Error generating JSON report: %v\n", err)
						file.Close()
						return exitError
					}
				default:
					generateTextReport(file, allTypos)
				}
				file.Close()
			}
		}
	}

	if len(allTypos) > 0 || checkErr != nil {
		return exitTypos
	}
	return exitOK
}
