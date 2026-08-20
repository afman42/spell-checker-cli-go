# Spell Checker CLI

A fast, concurrent command-line spell checker for text files. Written in Go —
scans a single file, an entire directory tree, or piped input, then reports
typos with ranked "did you mean?" suggestions.

## Features

- **Fast** — checks files concurrently across all CPU cores.
- **Flexible** — works on files, directories, stdin, or watch mode.
- **Helpful** — smart ranked suggestions (transposition-aware, prefix-aware),
  colored terminal output, live progress bar.
- **Unicode-aware** — handles accents (café), contractions (don't, it's),
  hyphenated words (state-of-the-art).
- **Multiple outputs** — plain text with colors, responsive dark-mode HTML, machine-readable JSON, or SARIF v2.1.0 (for GitHub Code Scanning).
- **Auto-fix** — `--fix` rewrites each typo to its top suggestion in place (with `--dry-run`).
- **Watch mode** — `fsnotify` re-checks files automatically as you save.
- **Fast startup** — the suggestion index is built once, then persisted on disk
  and reused across runs (invalidation is automatic on dictionary change).
- **Prose-focused** — identifier fragments (`Mi03x_er`), binary files, and
  dependency trees (`.git`, `node_modules`, …) are skipped by default. Markdown
  files (`.md`, `.markdown`) are scanned prose-only: fenced code blocks, inline
  code, link URLs, and YAML frontmatter are stripped before tokenizing.
- **Scopeable** — restrict a scan to your changed files with `--git-diff` (against
  a ref or `staged`), keep project words valid with `--ignore-word`, and filter
  short tokens with `--min-word-length`. A `.spellignore` file glob-excludes like
  `.gitignore`.
- **CI-friendly** — deterministic output, machine-readable JSON, and distinct
  exit codes: `1` when typos are found or remain unfixed, `2` when the tool
  cannot run.

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
| `--format` | Output format: `txt`, `html`, `json`, or `sarif` | `--format sarif` |
| `--output` | Write report to file or directory | `--output report.html` |
| `--fix` | Rewrite each typo to its top suggestion, in place | `--fix` |
| `--dry-run` | With `--fix`: show changes without writing | `--fix --dry-run` |
| `--version` | Print version and exit | `--version` |
| `--watch` | Watch a directory and re-check on save | `--watch` |
| `--verbose` | Log skipped (excluded/binary) files | `--verbose` |
| `--quiet` | Suppress all output; only the exit code is meaningful | `--quiet` |
| `--ignore-word` | Word(s) to treat as valid (repeatable or comma-separated) | `--ignore-word teh,aux` |
| `--min-word-length` | Skip tokens shorter than N characters (default 0 = check all) | `--min-word-length 3` |
| `--git-diff` | Scan only files changed vs a git ref (`staged` for the index) | `--git-diff main` |
| `--config` | Explicit path to a config file (overrides the auto search) | `--config ./ci.yaml` |

Settings precedence: **flags > config file > defaults**. A flag only overrides
the config file when explicitly passed on the command line, so a value set in
`spellchecker.yaml` stays in effect if you run the binary without that flag.

#### Exclude patterns

`--exclude` accepts comma-separated glob patterns. Patterns without a slash
are matched against each file or directory **name** (the basename). Patterns
containing a slash (e.g. `third_party/**`, `src/generated/*`) are matched
against the full relative path, so you can exclude by directory path. Trailing
slashes are stripped, so `build/` and `build` behave identically. Use `*`
wildcards freely:

| Pattern | Matches | Does not match |
|---------|---------|----------------|
| `*.log` | `error.log`, `debug.log` | `log.txt` |
| `build` | a file or dir named `build` | `build/output.txt` (its contents) |
| `build/` | same as `build` (trailing slash ignored) | — |
| `node_modules` | a dir named `node_modules` (skipped recursively) | `node_modules_backup` |

To skip a directory's contents, exclude the directory itself — `filepath.Walk`
applies `SkipDir` so the whole subtree is pruned.

A set of **built-in excludes is always active**, even with no `--exclude`: VCS
metadata (`.git`, `.hg`, `.svn`), dependency stores (`node_modules`,
`bower_components`, `vendor`), virtualenvs (`.venv`, `venv`, `virtualenv`), and
common tool caches (`.cache`, `.gradle`, `.cargo`, `__pycache__`,
`.pytest_cache`, `.mypy_cache`, `.tox`, `.next`, `.idea`, `.vscode`, and more).
These are merged with — not overridden by — anything you pass via `--exclude`.

#### Line length limit

Lines up to **1 MiB** are scanned normally. Longer lines (e.g. heavily minified
files) are skipped — their content is not checked — but the line is still
counted so every following line keeps its correct number, and the rest of the
file is scanned normally. A single pathological line never fails the file.

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

# Emit SARIF for GitHub Code Scanning
./spellchecker --format sarif --output results.sarif ./my_project/

# Scan only files changed on this branch vs main
./spellchecker --git-diff main ./my_project/

# Scan only staged files in a pre-commit hook
./spellchecker --git-diff staged .

# Treat project jargon as valid for one run
./spellchecker --ignore-word aux,k8s ./src/

# Skip 1-2 letter tokens to cut false positives
./spellchecker --min-word-length 3 ./docs/

# Run quietly in CI: only the exit code matters
./spellchecker --quiet ./src/

### Config file

Place a `spellchecker.yaml` (or `spellchecker.yml`) in the current directory or
`~/.config/spellchecker/`. It is picked up automatically — no flag needed.

```yaml
exclude:
  - "*.log"
  - "build/"
  - "vendor/"
personal-dictionary: ".project-words.txt"
format: "html"
output: "./reports/"
```

The cwd config wins over the home-directory config. Any flag you pass on the
command line overrides the corresponding config key; keys you don't pass fall
through to the config file, then to defaults.
### Scoping and filtering

#### `.spellignore`

A `.spellignore` file in the working directory holds one glob pattern per line
(`#` comments and blank lines ignored). Its patterns are merged with `--exclude`
and the built-in excludes, so you don't need to repeat them on every run:

```
# .spellignore
vendor/
*.generated.go
third_party/**
```

The `.spellignore` file itself is always skipped.

#### `--git-diff`

Restrict the scan to files changed relative to a git ref. Pass `staged` for the
index, or any ref (`main`, `HEAD~1`, a tag). The resulting file list is filtered
by the same exclude + binary rules as a directory walk, so ignored or binary
files are still skipped:

```bash
./spellchecker --git-diff main .            # files changed vs main
./spellchecker --git-diff staged .          # files in the index (pre-commit)
```

Outside a git repo, or with an unknown ref, the tool exits `2` with a git error.

#### Markdown-aware scanning

Files ending in `.md` or `.markdown` are scanned prose-only: fenced code blocks
(`\`\`\`` or `~~~`), inline code spans, link destinations (`[text](url)`), bare
URLs, and YAML frontmatter (`---` … `---`) are stripped before tokenizing, so
code and metadata don't get flagged. Line and column numbers stay accurate
against the original file.

#### `--ignore-word` and `--min-word-length`

`--ignore-word` accepts project jargon for a single run (repeatable, or
comma-separated). Words are lowercased and merged into the dictionary:

```bash
./spellchecker --ignore-word teh --ignore-word aux,k8s ./src/
```

`--min-word-length N` skips tokens shorter than N characters — useful to cut
false positives on 1-2 letter abbreviations. `0` (default) checks every token.

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
Typos found (2 total):

--- In file notes.txt ---
- Line 2, Col 5: "wrld" appears to be a typo. Did you mean: world, wald, weld, wild, wold?
```

- In a terminal, typos are **red** and suggestions **green**.
- A live progress bar (`█░`) shows on stderr during large scans.
- Writing to a file or pipe produces plain text (no colors, no progress bar).
- Files are listed in sorted path order for deterministic output across runs.

#### Exit codes

| Scenario | Exit code |
|----------|-----------|
| No typos found | `0` |
| Typos found (default report mode) | `1` |
| `--fix` applied and **every** typo was correctable | `0` |
| `--fix` applied but some typos had no suggestion and were skipped | `1` |
| `--fix --dry-run` with typos present | `1` |
| Fatal error (bad usage, unknown flag, config/dictionary load failure, missing/unreadable scan path) | `2` |

`1` means "spelling problems exist", `2` means "couldn't run at all", so CI can
tell the difference between a lint failure and a tooling failure. The "skipped
typos still fail CI" rule is intentional: uncorrected typos remain in the
files, so CI should surface them for manual review rather than silently pass.

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
          "suggestions": ["world", "wald", "weld", "wild", "wold"] }
      ]
    }
  ]
}
```

Files are sorted by path for deterministic output, and words with no suggestion
serialize as `"suggestions": []`.

#### SARIF output

`--format sarif` (or `--output results.sarif`) emits a SARIF v2.1.0 log — the
format GitHub Code Scanning ingests. Each typo becomes one `warning` result
under rule `spellcheck/typo`, with file/line/column location and a stable
`partialFingerprints.primary` (`path:line:word`) so GitHub de-duplicates
findings across runs:

```bash
./spellchecker --format sarif --output results.sarif ./my_project/
```

Upload it from a workflow:

```yaml
- uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: results.sarif
```

#### Fix mode

`--fix` rewrites each typo to its top-ranked suggestion, in place. Writes are
atomic (temp file + `fsync` + rename) and file permissions are preserved, so a
crash never leaves a half-written file.

```bash
./spellchecker --fix ./src/             # apply fixes
./spellchecker --fix --dry-run ./src/   # preview only, write nothing
```

- Typos with no suggestion are left untouched and reported as skipped.
- Exit code reflects the result — see the [Exit codes](#exit-codes) table above.
  In short: a clean fix exits `0`; a fix that left skipped typos behind exits `1`
  so CI surfaces them for review.
- Fix mode works on piped stdin (`--fix -` writes the corrected stream to stdout; `--dry-run` affects only the exit code, not the output). Replacements preserve typo capitalization (`Teh` to `The`, `TEH` to `THE`).

> **Note:** suggestions are ranked by edit distance, then by transposition
> matches (`teh` → `the`), letter ordering, and shared prefix — so `worl` becomes
> `world`, not `whorl`. Ranking is still heuristic, so review changes (or use
> `--dry-run`) before committing automated fixes.

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
| `make build` | Optimized binary (stripped, static, no CGO, ~5.3 MB) |
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
| `make bench-cmp` | Run zstd decompression benchmarks |
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
├── checker.go           Scanner, concurrent worker pool, word tokenizer, binary detection
├── dictionary.go        Dictionary loading (zstd-compressed embedded dict)
├── suggestions.go       BK-tree + Levenshtein distance for suggestions
├── tree_cache.go        On-disk persistence of the built suggestion tree
├── fixer.go             Auto-fix with atomic writes (--fix)
├── watcher.go           Watch mode (fsnotify)
├── reporter.go          Text, HTML, and JSON output generation
├── gen_dict.go          Dictionary generator (//go:build ignore)
├── diff.go              Git-diff file list (--git-diff)
├── markdown.go          Markdown noise stripping (fences, inline code, URLs)
├── sarif.go             SARIF v2.1.0 report generation
├── spellignore.go       .spellignore file loader
├── main_test.go             Config validation + config-file loader tests
├── checker_test.go          Scanner, tokenizer, and file-collection tests
├── dictionary_test.go       Dictionary parsing/loading tests
├── suggestions_test.go      BK-tree + Levenshtein suggestion tests
├── tree_cache_test.go       Suggestion-tree cache round-trip tests
├── reporter_test.go         Text/HTML report + relLink path tests
├── json_report_test.go      JSON report shape and escaping tests
├── fixer_test.go            Auto-fix and atomic-write tests
├── watcher_test.go          Watch-mode batch and directory-watch tests
├── markdown_test.go         Markdown stripping tests
├── sarif_report_test.go     SARIF report shape tests
├── spellignore_test.go      .spellignore loader tests
├── Makefile                 Build, test, lint, benchmark targets
├── dictionary.txt.zst       Embedded word list (zstd-compressed, 324 KB)
├── dictionary.csv           Source CSV for dictionary generation
├── dictionary_bench_test.go Decompression speed benchmarks (zstd)
├── .githooks/pre-commit     Git pre-commit hook
├── test/                    Integration test fixtures
└── .github/workflows/       CI pipeline
```

### Architecture

| Component | What it does |
|-----------|-------------|
| **Worker pool** | Files are checked in parallel, one goroutine per CPU core; multiple file errors are aggregated and reported together |
| **Word tokenizer** | Unicode-aware regex (`\p{L}+`) for accents, contractions, hyphens; lines up to 1 MiB; identifier fragments next to digits/underscores are skipped; over-long lines are skipped without failing the file |
| **Dictionary** | Zstd-compressed word list embedded at compile time; O(1) hash-set lookup |
| **Suggestions** | BK-tree for O(log n) fuzzy search; Levenshtein edit distance ≤ 2; ranking is transposition- and prefix-aware |
| **Startup cache** | The built suggestion tree is persisted to the user cache directory (keyed and versioned) and loaded on later runs — first-typo startup drops from seconds to <1s |
| **Binary detection** | Extensions (`.pdf`, images, archives, fonts, …) plus NUL/control-byte sniffing in the first 512 bytes are skipped |
| **Default excludes** | `.git`, `node_modules`, `vendor`, venvs, and tool caches are always skipped; `--exclude` patterns merge on top |
| **Watch mode** | `fsnotify` watches directories, debounces rapid save events (200 ms) |
| **Atomic writes** | `--fix` writes a temp file, `fsync`s it, renames over the target, then syncs the directory — crash-safe and permission-preserving |

---

## CI/CD

The GitHub Actions workflow at `.github/workflows/build.yml`:

- **Tests** — formatting, `go vet`, `staticcheck`, tests with race detector, code coverage
- **Builds** — cross-compiles for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
- **UPX** — compresses Linux binaries with UPX 5.2.0
- **Integration tests** — runs the binary against test fixtures with various flags
