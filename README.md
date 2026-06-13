# Spell Checker CLI

A fast command-line spell checker for your text files, written in Go. It scans a
single file, an entire directory tree, or piped input, then reports typos with
ranked "did you mean" suggestions.

- **Fast** — checks files concurrently across all CPU cores.
- **Flexible** — works on files, directories, or stdin.
- **Helpful** — ranked suggestions, colored terminal output, and a live progress bar.
- **Unicode-aware** — handles accents (café), contractions (don't, it's), and hyphenated words (state-of-the-art).
- **CI-friendly** — exits with code 1 when typos are found.
- **Watch mode** — re-checks files automatically as you save them.

---

## Installation

Build from source (requires Go 1.23+):

```bash
go build -o spellchecker .
```

This produces a `spellchecker` binary with the dictionary embedded, so it runs
anywhere without extra files.

---

## Quick Start

```bash
# Check a single file
./spellchecker my_document.txt

# Check every file in a directory (recursively)
./spellchecker ./my_project/

# Check text piped from another command
cat my_file.txt | ./spellchecker -

# Watch a directory and re-check files on save
./spellchecker --watch ./src/
```

---

## Command-Line Options

| Option            | Description                                          | Example                       |
|-------------------|------------------------------------------------------|-------------------------------|
| `--dict`          | Use a custom CSV dictionary instead of the built-in  | `--dict my_words.csv`         |
| `--personal-dict` | Add project-specific words to ignore (one per line)  | `--personal-dict .words.txt`  |
| `--exclude`       | Comma-separated glob patterns to skip                | `--exclude "*.log,*.tmp"`     |
| `--format`        | Output format: `txt` or `html`                       | `--format html`               |
| `--output`        | Write the report to a file or directory              | `--output report.html`        |
| `--watch`         | Watch a directory and re-check on file changes       | `--watch`                     |
| `--verbose`       | Log skipped (excluded/binary) files                  | `--verbose`                   |

Settings precedence: **command-line flags > config file > defaults**.

---

## Examples

Check a directory while skipping logs and temp files:

```bash
./spellchecker --exclude "*.log,*.tmp" ./my_project/
```

Generate a single HTML report:

```bash
./spellchecker --format html --output report.html ./my_project/
```

Generate a multi-file HTML report (one page per file plus an index).
This happens when the format is HTML and the output path is **not** a `.html` file:

```bash
./spellchecker --format html --output ./reports/ ./my_project/
```

Use a custom dictionary of technical terms:

```bash
./spellchecker --dict my_technical_terms.csv ./docs/
```

Ignore project-specific words via a personal dictionary:

```bash
./spellchecker --personal-dict .project-words.txt ./src/
```

Watch a source folder during development:

```bash
./spellchecker --watch ./src/
```

---

## Configuration File

Instead of passing flags every time, place a `spellchecker.yaml` in the current
directory or in `~/.config/spellchecker/`.

```yaml
exclude:
  - "*.log"
  - "build/"
  - "vendor/"

personal-dictionary: ".project-words.txt"
format: "html"
output: "./reports/"
```

Then just run:

```bash
./spellchecker ./my_project/   # picks up spellchecker.yaml automatically
```

---

## Dictionary Formats

### Personal dictionary (plain text)

One word per line. Blank lines are ignored and lines starting with `#` are comments.

```
Qopper
FluxCapacitor
# This is a comment
bigcorp-api
Gregor
```

### Custom dictionary (CSV)

The first row is a header. Only the first column (the word) is used; other
columns are optional and ignored.

```csv
word,part_of_speech,definition
hello,,A greeting
world,,The earth
```

---

## Output

For each typo the checker reports the **file**, **line**, **column**, the
**misspelled word**, and up to **5 ranked suggestions** (closest match first):

```
Typos found:

--- In file notes.txt ---
- Line 2, Col 5: "wrld" appears to be a typo. Did you mean: wald, weld, wild, wold, world?
```

- In a terminal, typos are shown in red and suggestions in green.
- A live progress bar is shown on stderr during large directory scans.
- When writing to a file or pipe, output is plain text (no colors or progress bar).
- The process exits with status **1** if any typos are found, otherwise **0**.

---

## How It Works

| Component            | What it does                                                        |
|----------------------|---------------------------------------------------------------------|
| Concurrent workers   | Files are checked in parallel using a worker pool sized to your CPUs |
| Word tokenizer       | Unicode-aware regex that handles accents, contractions, and hyphens  |
| Dictionary lookup    | O(1) hash-set membership test for each word                          |
| BK-tree + Levenshtein| Finds similar words within edit distance 2 for suggestions           |
| Binary detection     | Files containing null bytes are skipped automatically                |
| Watch mode           | `fsnotify` watches directories and debounces rapid save events       |

---

## Exit Codes

| Code | Meaning                              |
|------|--------------------------------------|
| `0`  | No typos found                       |
| `1`  | Typos found, or a fatal error occurred |
