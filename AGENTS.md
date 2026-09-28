## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, use the installed graphify skill or instructions before doing anything else.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

## Commands (authoritative: Makefile, .golangci.yml)

| Change | Run |
|--------|-----|
| Code (`*.go`) | `make test-short` + `make vet` first; then `make test-race` + `make lint` + `make vuln` before done |
| Docs-only (`*.md`) | `make fmt-check` only; no tests |
| Perf path (`dictionary.go`, `suggestions.go`, `tree_cache.go`) | `make bench` (or `make bench-cmp` for zstd-only) |
| Quick pre-commit | `go fmt ./...`, `go vet ./...`, `go test ./... -short -count=1` (see `.githooks/pre-commit`) |

Full CI mirror: `.github/workflows/build.yml` (fmt, vet, staticcheck, golangci-lint, govulncheck, race, coverage, cross-builds).
Go 1.25.13+ pinned in `go.mod`. Build: `make build` (CGO=0, static, stripped). Never copy versions here; `go.mod` and `Makefile` own them.

## Generated files — do not hand-edit

- `dictionary.txt.zst` is generated. Source: `dictionary.csv` via `gen_dict.go` (`//go:build ignore`). Rebuild: `make dict` (`go run gen_dict.go`). Commit both `.csv` and `.zst` together.
- Disposable output, never commit: `spellchecker` binary, `bin/`, `spell-check-report/`, `html/`. Clean: `make clean`.

## Hard prohibitions

- NEVER add a `go.mod` dependency without explicit user request. Check imports + `go.mod` first.
- NEVER run `git` write operations (`add`, `commit`, `reset`, `clean`, `stash`) or bypass hooks with `--no-verify`.
- NEVER `go fmt` files outside your touched scope; formatting fixes stay local.
- NEVER edit `*.zst`, lockfiles, or generated output directly.
- Exit codes are contract: `0` clean, `1` typos remain (including skipped `--fix`), `2` tool failure. Flag precedence: flags > config file > defaults.

## Conventions

- Mimic surrounding Go style (naming, error wrapping, worker-pool patterns in `runners.go`).
- Handle all errors; errcheck exclusions live in `.golangci.yml` — that file is authoritative.
- Comments explain WHY, not WHAT. No user-facing narration in code comments.
- Tests: match existing `*_test.go` style; run narrowest test covering the change first.

## Architecture (scan path)

`main.go` dispatch → `runners.go` worker pool → `filegate.go` + `markdown.go` + `spellignore.go` filter → `scan.go` tokenizer → `dictionary.go` lookup + `suggestions.go` BK-tree → `reporter.go` / `sarif.go` / `fixer.go` output → `watcher.go` (watch mode only).
Details in `README.md` (behavior, exit codes, config) and `DESIGN_GRAPH.md`. Query graph first per rules above.
