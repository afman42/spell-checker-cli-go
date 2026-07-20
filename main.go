package main

import (
	"fmt"
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

// loadConfig initializes flags and loads configuration from a file and flags.
// Precedence: Flags > Config File > Defaults.
func loadConfig() (*Config, error) {
	// --- Define Flags using pflag ---
	excludeFlag := pflag.StringSlice("exclude", nil, "Optional: comma-separated list of file patterns to exclude.")
	dictFlag := pflag.String("dict", "", "Optional: path to a custom CSV dictionary file.")
	personalDictFlag := pflag.String("personal-dict", "", "Optional: path to a personal dictionary file (one word per line).")
	outputFlag := pflag.String("output", "", "Optional: path to an output file or directory (for HTML reports).")
	formatFlag := pflag.String("format", "", "Optional: output format (txt, html, json). Overrides filename extension.")
	verboseFlag := pflag.Bool("verbose", false, "Enable verbose logging to show skipped files and directories.")
	watchFlag := pflag.Bool("watch", false, "Watch directory for changes and re-check files on save.")
	fixFlag := pflag.Bool("fix", false, "Rewrite each typo to its top suggestion, in place.")
	dryRunFlag := pflag.Bool("dry-run", false, "With --fix: report what would change without writing files.")
	pflag.Parse()

	// --- Load Config File (YAML) ---
	cfg := &Config{}
	if path := findConfigFile(configSearchDirs()); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("error parsing config file: %w", err)
		}
	}

	// --- Apply flags (flags > config > defaults) ---
	// Only override when the flag was explicitly set on the command line.
	if pflag.Lookup("exclude").Changed {
		cfg.Exclude = *excludeFlag
	}
	if pflag.Lookup("dict").Changed {
		cfg.Dictionary = *dictFlag
	}
	if pflag.Lookup("personal-dict").Changed {
		cfg.PersonalDictionary = *personalDictFlag
	}
	if pflag.Lookup("output").Changed {
		cfg.Output = *outputFlag
	}
	if pflag.Lookup("format").Changed {
		cfg.Format = OutputFormat(*formatFlag)
	}
	if pflag.Lookup("verbose").Changed {
		cfg.Verbose = *verboseFlag
	}
	if pflag.Lookup("watch").Changed {
		cfg.Watch = *watchFlag
	}
	if pflag.Lookup("fix").Changed {
		cfg.Fix = *fixFlag
	}
	if pflag.Lookup("dry-run").Changed {
		cfg.DryRun = *dryRunFlag
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation error: %w", err)
	}

	return cfg, nil
}

func main() {
	// --- Load Configuration ---
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error loading configuration: %v\n", err)
		os.Exit(1)
	}

	dictionary, err := loadDictionary(cfg.Dictionary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error loading dictionary: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Successfully loaded %d words.\n", len(dictionary))

	if cfg.PersonalDictionary != "" {
		count, err := loadPersonalDictionary(cfg.PersonalDictionary, dictionary)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading personal dictionary: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Successfully loaded and merged %d words from personal dictionary.\n", count)
	}

	if pflag.NArg() < 1 {
		fmt.Println("Usage: spellchecker [flags] <file_or_directory>")
		os.Exit(1)
	}

	path := pflag.Arg(0)

	// Watch mode: stay running and re-check files on change
	if cfg.Watch {
		if path == "-" {
			fmt.Fprintln(os.Stderr, "Watch mode does not support stdin.")
			os.Exit(1)
		}
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot access path: %v\n", err)
			os.Exit(1)
		}
		if !info.IsDir() {
			fmt.Fprintln(os.Stderr, "Watch mode requires a directory path.")
			os.Exit(1)
		}
		if err := runWatcher(path, dictionary, cfg.Exclude); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		return
	}

	var allTypos map[string][]MisspelledWord
	if path == "-" {
		// Process stdin
		concurrentDict := NewConcurrentDictionary(dictionary)
		typos, err := checkStdin(concurrentDict)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing stdin: %v\n", err)
			os.Exit(1)
		}
		allTypos = map[string][]MisspelledWord{"<stdin>": typos}
	} else {
		// Process file or directory
		var err error
		allTypos, err = runConcurrentChecker(path, dictionary, cfg.Exclude, cfg.Verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing path: %v\n", err)
			os.Exit(1)
		}
	}

	// --- Fix mode: rewrite typos in place instead of producing a report ---
	if cfg.Fix {
		if path == "-" {
			fmt.Fprintln(os.Stderr, "Fix mode does not support stdin.")
			os.Exit(1)
		}
		_, skipped, err := runFixer(allTypos, cfg.DryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fixing files: %v\n", err)
			os.Exit(1)
		}
		// Dry-run: signal typos via exit code 1 so CI still fails.
		// Real fix: exit 0 only if every typo was correctable. Skipped typos
		// (no suggestion) remain in the files, so CI should fail to surface
		// them for manual review.
		if cfg.DryRun && len(allTypos) > 0 {
			os.Exit(1)
		}
		if !cfg.DryRun && skipped > 0 {
			os.Exit(1)
		}
		return
	}

	// --- REVISED OUTPUT LOGIC ---
	if cfg.Output == "" {
		// Default case: no output path. Print to stdout in the chosen format
		// (text unless --format json was requested).
		switch strings.ToLower(string(cfg.Format)) {
		case string(FormatJSON):
			if err := generateJSONReport(os.Stdout, allTypos); err != nil {
				fmt.Fprintf(os.Stderr, "Error generating JSON report: %v\n", err)
				os.Exit(1)
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
				os.Exit(1)
			}
		} else {
			// Single-file output for text, JSON, or a specific HTML file.
			file, err := os.Create(cfg.Output)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Report will be saved to: %s\n", cfg.Output)
			switch {
			case isHTML:
				generateHTMLReport(file, allTypos)
			case isJSON:
				if err := generateJSONReport(file, allTypos); err != nil {
					fmt.Fprintf(os.Stderr, "Error generating JSON report: %v\n", err)
					file.Close()
					os.Exit(1)
				}
			default:
				generateTextReport(file, allTypos)
			}
			file.Close()
		}
	}

	if len(allTypos) > 0 {
		os.Exit(1)
	}
}
