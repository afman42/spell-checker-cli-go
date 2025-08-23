package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	// Exclude is a list of file patterns to exclude.
	Exclude []string `mapstructure:"exclude"`
	// Dictionary is the path to a custom dictionary file.
	Dictionary string `mapstructure:"dictionary"`
	// PersonalDictionary is the path to a personal word list.
	PersonalDictionary string `mapstructure:"personal-dictionary"`
	// Format is the output format (txt, html).
	Format string `mapstructure:"format"`
	// Verbose enables verbose logging.
	Verbose bool `mapstructure:"verbose"`
	// Output is the path for the report file or directory.
	Output string `mapstructure:"output"`
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
	pflag.Parse()

	// --- Initialize Viper ---
	v := viper.New()
	// Set the name of the config file (without extension).
	v.SetConfigName("spellchecker")
	// Add search paths for the config file.
	v.AddConfigPath(".")                          // Look in the current directory.
	v.AddConfigPath("$HOME/.config/spellchecker") // Look in a standard config location.
	v.AddConfigPath(`C:\Users\%USERNAME%`)        // Look in a standard config location.

	// --- Bind pflags to Viper ---
	// This tells Viper to check the flag value if a key is not found in the config file.
	v.BindPFlag("exclude", pflag.Lookup("exclude"))
	v.BindPFlag("dictionary", pflag.Lookup("dict"))
	v.BindPFlag("personal-dictionary", pflag.Lookup("personal-dict"))
	v.BindPFlag("output", pflag.Lookup("output"))
	v.BindPFlag("format", pflag.Lookup("format"))
	v.BindPFlag("verbose", pflag.Lookup("verbose"))

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
	allTypos, err := runConcurrentChecker(path, dictionary, cfg.Exclude, cfg.Verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing path: %v\n", err)
		os.Exit(1)
	}

	// --- REVISED OUTPUT LOGIC ---
	if cfg.Output == "" {
		// Default case: No output path provided, so print a text report to standard output.
		generateTextReport(os.Stdout, allTypos)
	} else {
		// An output path was provided. Determine the format and mode.
		format := strings.ToLower(cfg.Format)
		ext := strings.ToLower(filepath.Ext(cfg.Output))

		// Determine if the desired format is HTML.
		isHTML := format == "html" || (format == "" && ext == ".html")

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
			defer file.Close()

			fmt.Printf("Report will be saved to: %s\n", cfg.Output)
			if isHTML {
				generateHTMLReport(file, allTypos)
			} else {
				generateTextReport(file, allTypos)
			}
		}
	}

	if len(allTypos) > 0 {
		os.Exit(1)
	}
}
