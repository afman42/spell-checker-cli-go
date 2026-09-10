# Design Graph: Full Codebase Audit

## PROBLEM

Audit all 15 Go source files (~7000 LOC) in spell-checker-cli-go for:

- Correctness bugs (logic errors, race conditions, off-by-one)
- Over-engineering (unnecessary abstractions, speculative generality)
- Security issues (path traversal, injection, unsafe input handling)
- Test coverage gaps
- Performance waste
- API/contract drift between components

## SHAPES

| Shape | Role | File |
| ------- | ------ | ------ |
| Config | CLI flags + YAML merge, validation | main.go |
| Dictionary | Load embedded/custom/personal word lists | dictionary.go |
| ConcurrentDictionary | Thread-safe wrapper, lazy BK-tree | dictionary.go |
| BKTree | Fuzzy search index | suggestions.go |
| Checker | File walk, tokenize, detect typos | scan.go, runners.go, filegate.go |
| Fixer | Rewrite typos in-place, atomic write | fixer.go |
| Reporter | Text/HTML/JSON/SARIF output | reporter.go, sarif.go |
| Watcher | fsnotify loop, debounce, re-scan | watcher.go |
| Diff | Git changed-files/hunks parsing | diff.go |
| Markdown | Prose-only filtering | markdown.go |
| Spellignore | Glob exclude patterns | spellignore.go |
| TreeCache | Persist BK-tree to disk via gob | tree_cache.go |

## GRAPH

```
Config ──(1:1)──> Dictionary ──(1:1)──> ConcurrentDictionary
                                           │
                                           ├──(1:N)──> BKTree (lazy, cached)
                                           │
                                           └──(1:1)──> Checker ──(1:N)──> MisspelledWord
                                                                   │
                                     ┌─────────────────────────────┼─────────────────────────────┐
                                     │                             │                             │
                                     ▼                             ▼                             ▼
                                  Fixer                        Reporter                      Watcher
                                     │                             │                             │
                                     └──(1:1)──> writeFileAtomic   ├──(1:1)──> Text/JSON         └──(1:N)──> processBatch
                                                               │
                                                               ├──(1:1)──> HTML (single/multi)
                                                               │
                                                               └──(1:1)──> SARIF

Diff ──(1:N)──> ChangedLines ──(1:1)──> Checker (hunk-scoped)
Markdown ──(1:1)──> scanMarkdownLine ──(1:1)──> Checker
Spellignore ──(1:N)──> exclude patterns ──(1:1)──> collectFiles
TreeCache ──(1:1)──> BKTree (persist/reload)
```

## CARDINALITY

- Config → Dictionary: 1:1 (one config, one loaded dictionary)
- Dictionary → ConcurrentDictionary: 1:1 (wrapper)
- ConcurrentDictionary → BKTree: 1:1 (lazy singleton)
- Checker → MisspelledWord: 1:N (one scan, many typos)
- Fixer → writeFileAtomic: 1:1 per file
- Reporter → output format: 1:1 per run
- Watcher → processBatch: 1:N (many batches over time)
- Diff → ChangedLines: 1:N (many files, many lines)

## BOUNDARIES

| Boundary | Contract |
| ---------- | ---------- |
| Config.Validate() | Returns error for invalid format/dry-run without fix |
| Dictionary load | Returns `map[string]struct{}`, lowercased |
| ConcurrentDictionary.Suggestions | Returns `[]string`, ranked, max 5 |
| Checker → MisspelledWord | Word, Line, Column (1-based), Suggestions |
| Fixer → FixResult | FilePath, Fixes, Skipped counts |
| Reporter formats | Deterministic sorted output, escaped HTML |
| TreeCache | gob-encoded, versioned, 32 MiB cap |

## BEHAVIOR

1. **Scan path**: Config → loadDictionary → ConcurrentDictionary → walk files → tokenize → filter → suggest → collect MisspelledWord
2. **Fix path**: Scan results → buildFixRepl (column-keyed) → rewriteLines → writeFileAtomic
3. **Watch path**: fsnotify → debounce → processBatch → checkFile → report
4. **Report path**: CheckResults → sortedResultPaths → format-specific generator → writer

## SCOPE

**In scope**: All 15 .go files, go.mod, style.css, README.md
**Out of scope**: graphify-out/, .github/, test fixtures, CI config

## TEST LAYERS

| Layer | Coverage |
| ------- | ---------- |
| Unit | levenshtein, osa, keyboardAdjacent, matchCase, shouldExclude, scanMarkdownLine |
| Integration | checkFile, runConcurrentChecker, fixFile, loadDictionary |
| E2E | run() exit codes, config loading, report generation |
| Benchmark | Suggest, Levenshtein, OSA, ScanForTypos, ZstdDecompress |

## VERDICT

Audit proceeds across all layers. No blocking questions — scope is clear.
