package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

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
	GitDiff          string `yaml:"git-diff"`
	OnlyChangedLines bool   `yaml:"only-changed-lines"`
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
	if c.OnlyChangedLines && c.GitDiff == "" {
		return fmt.Errorf("--only-changed-lines requires --git-diff")
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

// loadYAMLConfig reads and parses a spellchecker YAML config file into a
// fresh Config.
func loadYAMLConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading config file %q: %w", path, err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("error parsing config file %q: %w", path, err)
	}
	return cfg, nil
}

// Exit codes: 0 = clean, 1 = typos found (or fixincomplete), 2 = usage/config/
// internal error. 1 vs 2 lets CI distinguish "spelling problems" from "couldn't
// run".
const (
	exitOK    = 0
	exitTypos = 1
	exitError = 2
)

const stdinKey = "<stdin>"

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
	onlyChangedLinesFlag := fs.Bool("only-changed-lines", false, "With --git-diff: only report typos on added lines (parsed from git diff hunks).")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}

	// --- Load Config File (YAML) — single load, explicit --config wins ---
	cfg := &Config{}
	if fs.Lookup("config").Changed && *configFlag != "" {
		loaded, err := loadYAMLConfig(*configFlag)
		if err != nil {
			return nil, nil, err
		}
		cfg = loaded
	} else if path := findConfigFile(configSearchDirs()); path != "" {
		loaded, err := loadYAMLConfig(path)
		if err != nil {
			return nil, nil, err
		}
		cfg = loaded
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
	if fs.Lookup("only-changed-lines").Changed {
		cfg.OnlyChangedLines = *onlyChangedLinesFlag
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runWithContext(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// writeReport renders results in the given format to w. HTML is only selected
// when the caller resolves it explicitly (file output); any other format,
// including unknown values, falls back to the plain-text report.
func writeReport(w io.Writer, results CheckResults, format OutputFormat) error {
	var (
		genErr error
		kind   string
	)
	switch format {
	case FormatHTML:
		genErr, kind = generateHTMLReport(w, results), "HTML"
	case FormatJSON:
		genErr, kind = generateJSONReport(w, results), "JSON"
	case FormatSarif:
		genErr, kind = generateSARIFReport(w, results), "SARIF"
	default:
		genErr, kind = generateTextReport(w, results), "text"
	}
	if genErr != nil {
		return fmt.Errorf("error generating %s report: %w", kind, genErr)
	}
	return nil
}

// run executes the CLI with the given arguments and returns the exit code.
// Kept for backward compat with tests that call run(args) directly.
func run(args []string) int {
	return runWithContext(context.Background(), args, os.Stdout, os.Stderr)
}

func runWithContext(ctx context.Context, args []string, outW, errW io.Writer) int {
	// --- Load Configuration ---
	cfg, positionals, err := loadConfig(args)
	if err != nil {
		fmt.Fprintln(errW, fmt.Sprintf("Fatal error loading configuration: %v", err))
		return exitError
	}

	// Early exit: --version after config load, before heavy dictionary build.
	if cfg.Version {
		fmt.Fprintln(outW, versionString)
		return exitOK
	}
	if cfg.Quiet {
		outW = io.Discard
		errW = io.Discard
	}
	scanOpts := scanOptions{MinWordLength: cfg.MinWordLength, Verbose: cfg.Verbose}

	dictionary, err := loadDictionary(cfg.Dictionary)
	if err != nil {
		fmt.Fprintln(errW, fmt.Sprintf("Fatal error loading dictionary: %v", err))
		return exitError
	}
	if cfg.Verbose {
		fmt.Fprintf(errW, "Successfully loaded %d words.\n", len(dictionary))
	}

	if cfg.PersonalDictionary != "" {
		count, err := loadPersonalDictionary(cfg.PersonalDictionary, dictionary)
		if err != nil {
			fmt.Fprintf(errW, "Error loading personal dictionary: %v\n", err)
			return exitError
		}
		if cfg.Verbose {
			fmt.Fprintf(errW, "Successfully loaded and merged %d words from personal dictionary.\n", count)
		}
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
	// merged with --exclude and built-in excludes before scanning. The cwd file
	// always applies; when the target is a directory, its own .spellignore is
	// loaded too so a scanned tree can carry its exclusions.
	cfg.Exclude = append(cfg.Exclude, loadSpellignore(".")...)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		cfg.Exclude = append(cfg.Exclude, loadSpellignore(path)...)
	}

	// Watch mode: stay running and re-check files on change
	if cfg.Watch {
		if path == "-" {
			fmt.Fprintln(errW, "Watch mode does not support stdin.")
			return exitError
		}
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(errW, "Cannot access path: %v\n", err)
			return exitError
		}
		if !info.IsDir() {
			fmt.Fprintln(errW, "Watch mode requires a directory path.")
			return exitError
		}
		if err := runWatcherWithContext(ctx, path, dictionary, cfg.Exclude, outW, errW); err != nil {
			fmt.Fprintln(errW, err)
			return exitError
		}
		return exitOK
	}

	var allTypos CheckResults
	var checkErr error
	var stdinData []byte
	if path == "-" {
		stdinData, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(errW, "Error reading stdin: %v\n", err)
			return exitError
		}
		concurrentDict := NewConcurrentDictionary(dictionary)
		typos, err := checkStdin(bytes.NewReader(stdinData), concurrentDict, scanOpts)
		if err != nil {
			fmt.Fprintf(errW, "Error processing stdin: %v\n", err)
			return exitError
		}
		allTypos = CheckResults{}
		if len(typos) > 0 {
			allTypos[stdinKey] = typos
		}
	} else {
		concurrentDict := NewConcurrentDictionary(dictionary)
		if cfg.GitDiff != "" {
			if cfg.OnlyChangedLines {
				hunks, herr := gitDiffHunks(ctx, cfg.GitDiff)
				if herr != nil {
					fmt.Fprintf(errW, "git-diff error: %v\n", herr)
					return exitError
				}
				changedLines := parseChangedLines(hunks)
				allTypos, checkErr = runGitDiffCheckerWithHunks(ctx, cfg.GitDiff, path, concurrentDict, cfg.Exclude, cfg.Verbose, changedLines, scanOpts)
			} else {
				allTypos, checkErr = runGitDiffCheckerWithContext(ctx, cfg.GitDiff, path, concurrentDict, cfg.Exclude, cfg.Verbose, scanOpts)
			}
		} else {
			allTypos, checkErr = runConcurrentCheckerWithDictAndContext(ctx, path, concurrentDict, cfg.Exclude, cfg.Verbose, scanOpts)
		}
		if checkErr != nil {
			if errors.Is(checkErr, context.Canceled) {
				fmt.Fprintln(errW, "Interrupted — partial results below.")
			} else if cfg.GitDiff != "" && allTypos == nil {
				fmt.Fprintf(errW, "git-diff error: %v\n", checkErr)
				return exitError
			} else if allTypos == nil {
				fmt.Fprintf(errW, "Error: could not scan %s: %v\n", path, checkErr)
				return exitError
			} else {
				fmt.Fprintf(errW, "Warning: some files could not be checked:\n%v\n", checkErr)
			}
		}
		select {
		case <-ctx.Done():
			if checkErr == nil {
				checkErr = ctx.Err()
			}
		default:
		}
	}
	// --- Fix mode: rewrite typos in place instead of producing a report ---
	if cfg.Fix {
		var skipped, err = 0, error(nil)
		if path == "-" {
			// Apply fixes to piped stdin, writing corrected stream to stdout.
			_, skipped, err = fixStdin(bytes.NewReader(stdinData), allTypos[stdinKey])
			if err != nil {
				fmt.Fprintf(errW, "Error fixing stdin: %v\n", err)
				return exitError
			}
		} else {
			_, skipped, err = runFixer(allTypos, cfg.DryRun)
		}
		if err != nil {
			fmt.Fprintf(errW, "Error fixing files: %v\n", err)
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
			// Default case: no output path. Print to stdout in the chosen
			// format. Quirk: --format html without --output still prints
			// plain text; HTML is only produced when writing a file.
			stdoutFmt := cfg.Format
			if stdoutFmt == FormatHTML {
				stdoutFmt = FormatAuto
			}
			if err := writeReport(outW, allTypos, stdoutFmt); err != nil {
				fmt.Fprintf(errW, "%v\n", err)
				return exitError
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
				fmt.Fprintf(outW, "Generating multi-file HTML report in directory: %s\n", cfg.Output)
				if err := generateMultiFileHTMLReport(cfg.Output, allTypos); err != nil {
					fmt.Fprintf(errW, "Error generating multi-file report: %v\n", err)
					return exitError
				}
			} else {
				// Single-file output for text, JSON, or a specific HTML file.
				// Extension-based auto-detection (FormatAuto) applies only
				// here, when an output path was given.
				file, err := os.Create(cfg.Output)
				if err != nil {
					fmt.Fprintf(errW, "Error creating output file: %v\n", err)
					return exitError
				}

				fmt.Fprintf(outW, "Report will be saved to: %s\n", cfg.Output)
				outFmt := FormatAuto
				switch {
				case isHTML:
					outFmt = FormatHTML
				case isJSON:
					outFmt = FormatJSON
				case isSARIF:
					outFmt = FormatSarif
				}
				if err := writeReport(file, allTypos, outFmt); err != nil {
					fmt.Fprintf(errW, "%v\n", err)
					file.Close()
					return exitError
				}
				if cerr := file.Close(); cerr != nil {
					fmt.Fprintf(errW, "Error closing output file %s: %v\n", cfg.Output, cerr)
					return exitError
				}
			}
		}
	}

	if len(allTypos) > 0 || checkErr != nil {
		return exitTypos
	}
	return exitOK
}
