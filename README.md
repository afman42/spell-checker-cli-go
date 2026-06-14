# Spell Checker CLI

A fast, concurrent command-line spell checker for text files. Written in Go —
scans a single file, an entire directory tree, or piped input, then reports
typos with ranked "did you mean?" suggestions.

## Features

- **Fast** — checks files concurrently across all CPU cores.
- **Flexible** — works on files, directories, stdin, or watch mode.
- **Helpful** — ranked suggestions, colored terminal output, live progress bar.
- **Unicode-aware** — handles accents (café), contractions (don't, it's),
  hyphenated words (state-of-the-art).
- **Multiple outputs** — plain text with colors, responsive dark-mode HTML, or machine-readable JSON.
- **Auto-fix** — `--fix` rewrites each typo to its top suggestion in place (with `--dry-run`).
- **Watch mode** — `fsnotify` re-checks files automatically as you save.
- **CI-friendly** — JSON output and exit code 1 when typos are found.

---

## Quick Start

```bash
# Build the binary
make build

# Check a single file
./spellchecker my_document.txt

# Check every file in a directory (recursively)
./spellchecker ./my_project/

# Check text piped from another command
cat my_file.txt | ./spellchecker -

# Watch a directory and re-check files on save
./spellchecker --watch ./src/
```

The dictionary is embedded in the binary — it runs anywhere with no extra files.

### Build from source manually

Requires **Go 1.24+**.

```bash
go build -o spellchecker .
```

---

## Usage

### Options

| Flag | Description | Example |
|------|-------------|---------|
| `--dict` | Custom CSV dictionary | `--dict my_words.csv` |
| `--personal-dict` | Extra words to ignore (one per line) | `--personal-dict .words.txt` |
| `--exclude` | Comma-separated glob patterns to skip | `--exclude "*.log,*.tmp"` |
| `--format` | Output format: `txt`, `html`, or `json` | `--format json` |
| `--output` | Write report to file or directory | `--output report.html` |
| `--fix` | Rewrite each typo to its top suggestion, in place | `--fix` |
| `--dry-run` | With `--fix`: show changes without writing | `--fix --dry-run` |
| `--watch` | Watch a directory and re-check on save | `--watch` |
| `--verbose` | Log skipped (excluded/binary) files | `--verbose` |

Settings precedence: **flags > config file > defaults**.

### Examples

```bash
# Check a directory, skip logs and temp files
./spellchecker --exclude "*.log,*.tmp" ./my_project/

# Generate a single HTML report
./spellchecker --format html --output report.html ./my_project/

# Generate a multi-file HTML report (one page per file + index)
./spellchecker --format html --output ./reports/ ./my_project/

# Use a custom dictionary of technical terms
./spellchecker --dict my_technical_terms.csv ./docs/

# Ignore project-specific words
./spellchecker --personal-dict .project-words.txt ./src/

# Emit machine-readable JSON (for CI / editors)
./spellchecker --format json ./my_project/

# Auto-fix typos in place (top suggestion wins)
./spellchecker --fix ./my_project/

# Preview fixes without writing any files
./spellchecker --fix --dry-run ./my_project/

# Watch a source folder during development
./spellchecker --watch ./src/
```

### Config file

Place a `spellchecker.yaml` in the current directory or
`~/.config/spellchecker/`. It is picked up automatically.

```yaml
exclude:
  - "*.log"
  - "build/"
  - "vendor/"
personal-dictionary: ".project-words.txt"
format: "html"
output: "./reports/"
```

### Dictionary formats

**Personal dictionary** — plain text, one word per line. Lines starting with
`#` are comments.

```
Qopper
FluxCapacitor
# This is a comment
bigcorp-api
```

**Custom dictionary** — CSV with a header row. Only the first column is used.

```csv
word,part_of_speech,definition
hello,,A greeting
world,,The earth
```

### Output

```
Typos found:

--- In file notes.txt ---
- Line 2, Col 5: "wrld" appears to be a typo. Did you mean: wald, weld, wild, wold, world?
```

- In a terminal, typos are **red** and suggestions **green**.
- A live progress bar (`█░`) shows on stderr during large scans.
- Writing to a file or pipe produces plain text (no colors, no progress bar).
- Exit code **1** if any typos are found, otherwise **0**.

#### JSON output

`--format json` (or `--output report.json`) emits a structured document. All
diagnostic messages go to stderr, so stdout is clean and pipeable:

```bash
./spellchecker --format json notes.txt | jq '.summary'
```

```json
{
  "summary": { "files": 1, "typos": 1, "suggestions": 5 },
  "files": [
    {
      "file": "notes.txt",
      "typos": [
        { "word": "wrld", "line": 2, "column": 5,
          "suggestions": ["wald", "weld", "wild", "wold", "world"] }
      ]
    }
  ]
}
```

Files are sorted by path for deterministic output, and words with no suggestion
serialize as `"suggestions": []`.

#### Fix mode

`--fix` rewrites each typo to its top-ranked suggestion, in place. Writes are
atomic (temp file + rename) and file permissions are preserved.

```bash
./spellchecker --fix ./src/             # apply fixes
./spellchecker --fix --dry-run ./src/   # preview only, write nothing
```

- Typos with no suggestion are left untouched and reported as skipped.
- A real fix exits **0** (files were corrected); `--dry-run` exits **1** if
  typos remain, so it still fails CI.
- Fix mode does not read from stdin.

> Note: the "top suggestion" is ranked by edit distance, then alphabetically —
> so `wrld` becomes `wald`, not `world`. Review changes (or use `--dry-run`)
> before committing automated fixes.

---

## Development

### Prerequisites

- Go 1.24+
- `make` (for the Makefile targets)
- `upx` (optional, for release binary compression)

### Makefile targets

| Target | Description |
|--------|-------------|
| `make` / `make all` | Build + test |
| `make build` | Optimized binary (stripped, static, no CGO, ~5.7 MB) |
| `make build-cross` | Cross-compile: `make build-cross GOOS=linux GOARCH=arm64` |
| `make release` | Build + UPX compression (auto-detects `upx`) |
| `make setup` | Activate the pre-commit hook (`git config core.hooksPath .githooks`) |
| `make install` | Install to `$GOPATH/bin` |
| `make test` | Run all tests |
| `make test-race` | Run tests with the race detector |
| `make test-short` | Quick tests (skips benchmarks) |
| `make vet` | Run `go vet` |
| `make fmt` | Format all Go source files |
| `make fmt-check` | Verify all files are `go fmt`-compliant (CI-safe) |
| `make staticcheck` | Run static analysis |
| `make bench` | Run compression benchmarks |
| `make bench-cmp` | Compare gzip vs zstd decompression |
| `make dict` | Regenerate `dictionary.txt.zst` from `dictionary.csv` |
| `make clean` | Remove build artifacts |

### Pre-commit hook

A pre-commit hook is included at `.githooks/pre-commit`. It runs
`go fmt`, `go vet`, and short tests before each commit.

Activate it on a fresh clone:

```bash
git config core.hooksPath .githooks
```

Skip it for a single commit:

```bash
git commit --no-verify
```

### Project structure

```
├── main.go              Entry point, config, output routing
├── checker.go           Scanner, concurrent worker pool, word tokenizer
├── dictionary.go        Dictionary loading (zstd-compressed embedded dict)
├── suggestions.go       BK-tree + Levenshtein distance for suggestions
├── reporter.go          Text, HTML, and JSON output generation
├── fixer.go             Auto-fix mode (--fix / --dry-run), atomic writes
├── watcher.go           fsnotify-based watch mode
├── gen_dict.go          Dictionary generator (//go:build ignore)
├── Makefile                 Build, test, lint, benchmark targets
├── dictionary.txt.zst       Embedded word list (zstd-compressed, 324 KB)
├── dictionary.csv           Source CSV for dictionary generation
├── dictionary_bench_test.go Decompression speed benchmarks (gzip vs zstd)
├── .githooks/pre-commit     Git pre-commit hook
├── test/                    Integration test fixtures
└── .github/workflows/       CI pipeline
```

### Architecture

| Component | What it does |
|-----------|-------------|
| **Worker pool** | Files are checked in parallel, one goroutine per CPU core |
| **Word tokenizer** | Unicode-aware regex (`\p{L}+`) for accents, contractions, hyphens |
| **Dictionary** | Zstd-compressed word list embedded at compile time; O(1) hash-set lookup |
| **Suggestions** | BK-tree for O(log n) fuzzy search; Levenshtein edit distance ≤ 2 |
| **Binary detection** | Files with null bytes in the first 512 bytes are skipped |
| **Watch mode** | `fsnotify` watches directories, debounces rapid save events (200 ms) |

---

## CI/CD

The GitHub Actions workflow at `.github/workflows/build.yml`:

- **Tests** — formatting, `go vet`, `staticcheck`, tests with race detector, code coverage
- **Builds** — cross-compiles for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
- **UPX** — compresses Linux binaries with UPX 5.2.0
- **Integration tests** — runs the binary against test fixtures with various flags
