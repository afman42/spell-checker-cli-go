package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// versionString reported by --version.
const versionString = "spell-checker-cli v1.0.0"

// OutputFormat represents valid output formats
type OutputFormat string

const (
	FormatText  OutputFormat = "txt"
	FormatHTML  OutputFormat = "html"
	FormatJSON  OutputFormat = "json"
	FormatSarif OutputFormat = "sarif"
	FormatAuto  OutputFormat = "" // Auto-detect from file extension
)

type Config struct {
	// Exclude is a list of file patterns to exclude.
	Exclude []string `yaml:"exclude"`
	// Dictionary is the path to a custom dictionary file.
	Dictionary string `yaml:"dictionary"`
	// PersonalDictionary is the path to a personal word list.
	PersonalDictionary string `yaml:"personal-dictionary"`
	// Format is the output format (txt, html, json, sarif).
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
	// Version prints version and exits.
	Version bool
	// Quiet suppresses all stdout/stderr output; only the exit code remains.
	Quiet bool `yaml:"quiet"`
	// IgnoreWords are ad-hoc words to treat as valid, merged into the
	// dictionary at startup.
	IgnoreWords []string `yaml:"ignore-words"`
	// MinWordLength is the minimum token length to check; shorter tokens are
	// skipped (cuts false positives on 1-2 letter words).
	MinWordLength int `yaml:"min-word-length"`
	// GitDiff restricts the scan to files changed relative to a git ref.
	// "staged" means the staged/changes; any other value is treated as a ref
	// (e.g. "main") diffed against the working tree.
	GitDiff string `yaml:"git-diff"`
}

// Validate ensures the configuration is valid
func (c *Config) Validate() error {
	switch c.Format {
	case "", FormatText, FormatHTML, FormatJSON, FormatSarif:
	default:
		return fmt.Errorf("invalid format: %s, must be 'txt', 'html', 'json', or 'sarif'", c.Format)
	}
	if c.DryRun && !c.Fix {
		return fmt.Errorf("--dry-run requires --fix")
	}
	if c.MinWordLength < 0 {
		return fmt.Errorf("min-word-length must be >= 0, got %d", c.MinWordLength)
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
	formatFlag := fs.String("format", "", "Optional: output format (txt, html, json, sarif). Overrides filename extension.")
	verboseFlag := fs.Bool("verbose", false, "Enable verbose logging to show skipped files and directories.")
	watchFlag := fs.Bool("watch", false, "Watch directory for changes and re-check files on save.")
	fixFlag := fs.Bool("fix", false, "Rewrite each typo to its top suggestion, in place.")
	dryRunFlag := fs.Bool("dry-run", false, "With --fix: report what would change without writing files.")
	versionFlag := fs.Bool("version", false, "Print version and exit.")
	quietFlag := fs.Bool("quiet", false, "Suppress all output; only the exit code is meaningful.")
	configFlag := fs.String("config", "", "Optional: explicit path to a spellchecker YAML config file, overriding the default search.")
	ignoreWordFlag := fs.StringSlice("ignore-word", nil, "Optional: word(s) to treat as valid (repeatable, or comma-separated).")
	minWordLengthFlag := fs.Int("min-word-length", 0, "Optional: minimum token length to check; shorter tokens are skipped (default 0 = check all).")
	gitDiffFlag := fs.String("git-diff", "", "Optional: scan only files changed relative to a git ref (e.g. 'main'). Use 'staged' for staged changes.")
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
	// --- Apply explicit --config override (replaces auto-discovered cfg) ---
	if fs.Lookup("config").Changed && *configFlag != "" {
		cfg = &Config{}
		data, err := os.ReadFile(*configFlag)
		if err != nil {
			return nil, nil, fmt.Errorf("error reading config file %s: %w", *configFlag, err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, nil, fmt.Errorf("error parsing config file %s: %w", *configFlag, err)
		}
	}

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
	if fs.Lookup("version").Changed {
		cfg.Version = *versionFlag
	}
	if fs.Lookup("quiet").Changed {
		cfg.Quiet = *quietFlag
	}
	if fs.Lookup("ignore-word").Changed {
		cfg.IgnoreWords = *ignoreWordFlag
	}
	if fs.Lookup("min-word-length").Changed {
		cfg.MinWordLength = *minWordLengthFlag
	}
	if fs.Lookup("git-diff").Changed {
		cfg.GitDiff = *gitDiffFlag
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

	// Early exit: --version after config load, before heavy dictionary build.
	if cfg.Version {
		fmt.Println(versionString)
		return exitOK
	}
	// --quiet: suppress all stdout/stderr output so only the exit code is
	// meaningful (CI, scripts). Restore originals on return so concurrent
	origOut, origErr := os.Stdout, os.Stderr
	if cfg.Quiet {
		// os.Stdout/Stderr are *os.File, not io.Writer, so route them at a
		// real discard sink. /dev/null preserves the type and any caller that
		// inspects the fd still sees a valid FILE*.
		if devNull, derr := os.OpenFile(os.DevNull, os.O_WRONLY, 0); derr == nil {
			os.Stdout, os.Stderr = devNull, devNull
			defer func() {
				os.Stdout, os.Stderr = origOut, origErr
				devNull.Close()
			}()
		}
	}
	// Active scan configuration, read inside scanForTypos/scanLinesForTypos.
	scanOpts.MinWordLength = cfg.MinWordLength
	scanOpts.Markdown = true // code-fence/URL/frontmatter stripping is cheap and always safe

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

	// --ignore-word: ad-hoc words treated as valid, merged (lowercased) into
	// the shared dictionary so the ConcurrentDictionary treats them as known.
	for _, w := range cfg.IgnoreWords {
		for _, tok := range strings.Split(w, ",") {
			if t := strings.TrimSpace(tok); t != "" {
				dictionary[strings.ToLower(t)] = struct{}{}
			}
		}
	}

	if len(positionals) < 1 {
		fmt.Println("Usage: spellchecker [flags] <file_or_directory>")
		return exitError
	}

	path := positionals[0]

	// .spellignore: auto-loaded exclude patterns (one glob per line, # comments)
	// merged with --exclude and built-in excludes before scanning.
	cfg.Exclude = append(cfg.Exclude, loadSpellignore(".")...)

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
	var stdinData []byte
	if path == "-" {
		// Process stdin. Read the stream into a buffer once so checking and
		// fixing both see the same input (second read of os.Stdin is empty).
		stdinData, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			return exitError
		}
		concurrentDict := NewConcurrentDictionary(dictionary)
		typos, err := checkStdin(bytes.NewReader(stdinData), concurrentDict)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing stdin: %v\n", err)
			return exitError
		}
		allTypos = map[string][]MisspelledWord{}
		if len(typos) > 0 {
			allTypos["<stdin>"] = typos
		}
	} else {
		// Process file or directory, or the git-changed file set when
		// --git-diff is set. Individual file failures are aggregated into
		// checkErr but successful files still come back in allTypos, so a
		// report is produced for what could be scanned.
		concurrentDict := NewConcurrentDictionary(dictionary)
		if cfg.GitDiff != "" {
			allTypos, checkErr = runGitDiffChecker(cfg.GitDiff, path, concurrentDict, cfg.Exclude, cfg.Verbose)
		} else {
			allTypos, checkErr = runConcurrentCheckerWithDict(path, concurrentDict, cfg.Exclude, cfg.Verbose)
		}
		if checkErr != nil {
			// git-diff failure (not in repo, unknown ref) is a tooling
			// error, not a spelling problem. allTypos is nil when git itself
			// failed, as opposed to per-file errors during scanning which
			// still produce a partial result map.
			if cfg.GitDiff != "" && allTypos == nil {
				fmt.Fprintf(os.Stderr, "git-diff error: %v\n", checkErr)
				return exitError
			}
			// Same rule for the directory scan: nothing was scanned at all
			// (missing/unreadable root, walk failure), so this is a tooling
			// error, not a spelling problem. CI must be able to tell
			// "couldn't run" (2) apart from "typos found" (1).
			if allTypos == nil {
				fmt.Fprintf(os.Stderr, "Error: could not scan %s: %v\n", path, checkErr)
				return exitError
			}
			fmt.Fprintf(os.Stderr, "Warning: some files could not be checked:\n%v\n", checkErr)
		}
	}
	// --- Fix mode: rewrite typos in place instead of producing a report ---
	if cfg.Fix {
		var skipped, err = 0, error(nil)
		if path == "-" {
			// Apply fixes to piped stdin, writing corrected stream to stdout.
			_, skipped, err = fixStdin(bytes.NewReader(stdinData), allTypos["<stdin>"])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fixing stdin: %v\n", err)
				return exitError
			}
		} else {
			_, skipped, err = runFixer(allTypos, cfg.DryRun)
		}
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

	// If scanning failed before collecting anything (e.g. a missing root path),
	// the warning above is the report; don't emit a misleading "No typos found".
	if len(allTypos) > 0 || checkErr == nil {
		if cfg.Output == "" {
			// Default case: no output path. Print to stdout in the chosen format
			// (text unless --format json was requested).
			switch cfg.Format {
			case FormatJSON:
				if err := generateJSONReport(os.Stdout, allTypos); err != nil {
					fmt.Fprintf(os.Stderr, "Error generating JSON report: %v\n", err)
					return exitError
				}
			case FormatSarif:
				if err := generateSARIFReport(os.Stdout, allTypos); err != nil {
					fmt.Fprintf(os.Stderr, "Error generating SARIF report: %v\n", err)
					return exitError
				}
			default:
				if err := generateTextReport(os.Stdout, allTypos); err != nil {
					fmt.Fprintf(os.Stderr, "Error generating text report: %v\n", err)
					return exitError
				}
			}
		} else {
			// An output path was provided. Determine the format and mode.
			format := string(cfg.Format)
			ext := strings.ToLower(filepath.Ext(cfg.Output))

			// Determine if the desired format is HTML.
			isHTML := format == string(FormatHTML) || (format == string(FormatAuto) && ext == ".html")
			isJSON := format == string(FormatJSON) || (format == string(FormatAuto) && ext == ".json")
			isSARIF := format == string(FormatSarif) || (format == string(FormatAuto) && ext == ".sarif")

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
					if err := generateHTMLReport(file, allTypos); err != nil {
						fmt.Fprintf(os.Stderr, "Error generating HTML report: %v\n", err)
						file.Close()
						return exitError
					}
				case isJSON:
					if err := generateJSONReport(file, allTypos); err != nil {
						fmt.Fprintf(os.Stderr, "Error generating JSON report: %v\n", err)
						file.Close()
						return exitError
					}
				case isSARIF:
					if err := generateSARIFReport(file, allTypos); err != nil {
						fmt.Fprintf(os.Stderr, "Error generating SARIF report: %v\n", err)
						file.Close()
						return exitError
					}
				default:
					if err := generateTextReport(file, allTypos); err != nil {
						fmt.Fprintf(os.Stderr, "Error generating text report: %v\n", err)
						file.Close()
						return exitError
					}
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
