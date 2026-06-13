package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// OutputFormat represents valid output formats
type OutputFormat string

const (
	FormatText OutputFormat = "txt"
	FormatHTML OutputFormat = "html"
	FormatAuto OutputFormat = "" // Auto-detect from file extension
)

type Config struct {
	// Exclude is a list of file patterns to exclude.
	Exclude []string `mapstructure:"exclude"`
	// Dictionary is the path to a custom dictionary file.
	Dictionary string `mapstructure:"dictionary"`
	// PersonalDictionary is the path to a personal word list.
	PersonalDictionary string `mapstructure:"personal-dictionary"`
	// Format is the output format (txt, html).
	Format OutputFormat `mapstructure:"format"`
	// Verbose enables verbose logging.
	Verbose bool `mapstructure:"verbose"`
	// Output is the path for the report file or directory.
	Output string `mapstructure:"output"`
	// Watch enables file watching mode.
	Watch bool `mapstructure:"watch"`
}

// Validate ensures the configuration is valid
func (c *Config) Validate() error {
	if c.Format != "" && c.Format != "txt" && c.Format != "html" {
		return fmt.Errorf("invalid format: %s, must be 'txt' or 'html'", c.Format)
	}
	return nil
}

// loadConfig initializes flags and loads configuration from a file and flags.
// Precedence: Flags > Config File > Defaults.
func loadConfig() (*Config, error) {
	// --- Define Flags using pflag ---
	// pflag is a drop-in replacement for Go's flag package with more features.
	pflag.StringSlice("exclude", []string{}, "Optional: comma-separated list of file patterns to exclude.")
	pflag.String("dict", "", "Optional: path to a custom CSV dictionary file.")
	pflag.String("personal-dict", "", "Optional: path to a personal dictionary file (one word per line).")
	pflag.String("output", "", "Optional: path to an output file or directory (for HTML reports).")
	pflag.String("format", "", "Optional: output format (txt, html). Overrides filename extension.")
	pflag.Bool("verbose", false, "Enable verbose logging to show skipped files and directories.")
	pflag.Bool("watch", false, "Watch directory for changes and re-check files on save.")
	pflag.Parse()

	// --- Initialize Viper ---
	v := viper.New()
	// Set the name of the config file (without extension).
	v.SetConfigName("spellchecker")
	// Add search paths for the config file.
	v.AddConfigPath(".") // Look in the current directory.
	if homeDir, err := os.UserHomeDir(); err == nil {
		v.AddConfigPath(filepath.Join(homeDir, ".config", "spellchecker"))
	}

	// --- Bind pflags to Viper ---
	// This tells Viper to check the flag value if a key is not found in the config file.
	v.BindPFlag("exclude", pflag.Lookup("exclude"))
	v.BindPFlag("dictionary", pflag.Lookup("dict"))
	v.BindPFlag("personal-dictionary", pflag.Lookup("personal-dict"))
	v.BindPFlag("output", pflag.Lookup("output"))
	v.BindPFlag("format", pflag.Lookup("format"))
	v.BindPFlag("verbose", pflag.Lookup("verbose"))
	v.BindPFlag("watch", pflag.Lookup("watch"))

	// --- Read Config File ---
	// Find and read the config file.
	if err := v.ReadInConfig(); err != nil {
		// It's okay if the config file doesn't exist.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// --- Unmarshal to Struct ---
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation error: %w", err)
	}

	return &cfg, nil
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
	fmt.Printf("Successfully loaded %d words.\n", len(dictionary))

	if cfg.PersonalDictionary != "" {
		count, err := loadPersonalDictionary(cfg.PersonalDictionary, dictionary)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading personal dictionary: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully loaded and merged %d words from personal dictionary.\n", count)
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

	// --- REVISED OUTPUT LOGIC ---
	if cfg.Output == "" {
		// Default case: No output path provided, so print a text report to standard output.
		generateTextReport(os.Stdout, allTypos)
	} else {
		// An output path was provided. Determine the format and mode.
		format := strings.ToLower(string(cfg.Format))
		ext := strings.ToLower(filepath.Ext(cfg.Output))

		// Determine if the desired format is HTML.
		isHTML := format == string(FormatHTML) || (format == string(FormatAuto) && ext == ".html")

		// NEW: Determine if we should use the multi-file directory mode for HTML.
		// This is triggered if the format is HTML AND the path does not end in ".html".
		isMultiFileDir := isHTML && ext != ".html"

		if isMultiFileDir {
			fmt.Printf("Generating multi-file HTML report in directory: %s\n", cfg.Output)
			if err := generateMultiFileHTMLReport(cfg.Output, allTypos); err != nil {
				fmt.Fprintf(os.Stderr, "Error generating multi-file report: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Fallback to single-file output for text reports or specific HTML files.
			file, err := os.Create(cfg.Output)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Report will be saved to: %s\n", cfg.Output)
			if isHTML {
				generateHTMLReport(file, allTypos)
			} else {
				generateTextReport(file, allTypos)
			}
			file.Close()
		}
	}

	if len(allTypos) > 0 {
		os.Exit(1)
	}
}
