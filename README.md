# Spell Checker CLI

A fast and efficient command-line spell checker that helps you find typos in your text files. Works with individual files, entire directories, or piped input from other commands.

## Features

- **Fast**: Uses concurrent processing to check multiple files simultaneously
- **Flexible**: Works with single files, directories, or stdin input
- **Configurable**: Supports custom dictionaries and exclusion patterns
- **Multiple Formats**: Output in text, HTML, or multi-file HTML reports
- **Smart Matching**: Handles contractions (don't, we're) and hyphenated words (state-of-the-art)

## Installation

To build from source:

```bash
# Clone and build the project
go build -o spellchecker .
```

## Quick Start

Check a single file:
```bash
./spellchecker my_document.txt
```

Check an entire directory:
```bash
./spellchecker ./my_project/
```

Check text from stdin:
```bash
cat my_file.txt | ./spellchecker -
```

## Basic Usage

```bash
# Check a single file
./spellchecker <file>

# Check all files in a directory
./spellchecker <directory>

# Check text piped from stdin
cat file.txt | ./spellchecker -
```

## Command-Line Options

| Option | Description | Example |
|--------|-------------|---------|
| `--dict` | Custom CSV dictionary file | `--dict my_words.csv` |
| `--exclude` | Files/patterns to skip | `--exclude "*.log,*.tmp"` |
| `--format` | Output format (txt, html) | `--format html` |
| `--output` | Where to save results | `--output report.html` |
| `--personal-dict` | Personal words to ignore | `--personal-dict my_words.txt` |
| `--verbose` | Show skipped files | `--verbose` |

## Examples

**Check a directory, excluding certain file types:**
```bash
./spellchecker --exclude "*.log,*.tmp" ./my_project/
```

**Generate an HTML report:**
```bash
./spellchecker --format html --output report.html ./my_project/
```

**Use a custom dictionary:**
```bash
./spellchecker --dict my_technical_terms.csv ./docs/
```

**Check files with a personal dictionary (for project-specific terms):**
```bash
./spellchecker --personal-dict .project-words.txt ./src/
```

## Configuration Files

Instead of command-line options, you can use a configuration file:

**spellchecker.yaml:**
```yaml
exclude:
  - "*.log"
  - "build/"
  - "vendor/"

personal-dictionary: ".project-words.txt"
format: "html"
output: "./reports/"
```

**Usage:**
```bash
./spellchecker ./my_project/  # Uses settings from spellchecker.yaml
```

## Personal Dictionary Format

Create a file with one word per line. Comments start with `#`:

```
Qopper
FluxCapacitor
# This is a comment
bigcorp-api
Gregor
Samsa
```

## Custom Dictionary Format (CSV)

For more advanced dictionaries in CSV format:

```csv
word,part_of_speech,definition
hello,,A greeting
world,,The earth
```

## Output

The spell checker will:
- Show typos with line numbers and column positions
- Provide suggestions for misspelled words
- Exit with status 1 if typos are found (useful for CI/CD pipelines)
- Output results to terminal or specified file/directory

## How It Works

The spell checker uses:
- **Concurrent processing**: Checks multiple files at once
- **Efficient matching**: Fast word extraction and dictionary lookup
- **BK-tree algorithm**: Quickly finds similar words for suggestions
- **Binary detection**: Automatically skips binary files
- **Smart regex**: Properly handles contractions and hyphens
