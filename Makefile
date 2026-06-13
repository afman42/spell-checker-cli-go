# Spell Checker CLI — Makefile
#
# Targets:
#   build         Optimized release build (stripped, static)
#   build-fast    Quick development build (no stripping)
#   release       build + UPX compression
#   install       Build and install to $GOPATH/bin
#   test          Run all tests
#   test-race     Run all tests with the race detector
#   test-short    Run only short tests (skips benchmarks)
#   bench         Run compression benchmarks
#   bench-cmp     Run and compare gzip-vs-zstd benchmarks
#   vet           Run go vet ./...
#   fmt           Format all Go source files
#   dict          Regenerate the embedded dictionary from dictionary.csv
#   clean         Remove build artifacts
#   all           Default: build + test

# Binary name
BINARY := spellchecker

# Go build flags for a small, portable binary.
#   -s -w          strip debug info and symbol table
#   -trimpath      remove build-time file paths
#   -buildvcs=false skip VCS stamps (tiny size reduction)
#   -tags netgo osusergo  force pure-Go net & OS user lookups (static binary)
LDFLAGS  := -ldflags="-s -w"
TAGS     := -tags="netgo osusergo"
BUILD_ENV := CGO_ENABLED=0

# UPX path (set to empty to skip automatically; use upx target explicitly)
UPX := $(shell command -v upx 2>/dev/null)

# Default target
.PHONY: all
all: build test

# -------------------------------------------------------------------
# Build targets
# -------------------------------------------------------------------

.PHONY: build
build: $(BINARY)

$(BINARY): *.go dictionary.txt.zst
	$(BUILD_ENV) go build $(LDFLAGS) $(TAGS) -trimpath -buildvcs=false -o $@ .

# Cross-compile for a specific OS/ARCH.
# Usage: make build-cross GOOS=linux GOARCH=arm64 [OUTDIR=./bin]
.PHONY: build-cross
build-cross: dictionary.txt.zst
	@mkdir -p $(or $(OUTDIR),./bin)/$(GOOS)_$(GOARCH)
	$(BUILD_ENV) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) $(TAGS) -trimpath -buildvcs=false -o $(or $(OUTDIR),./bin)/$(GOOS)_$(GOARCH)/$(BINARY) .

.PHONY: build-fast
build-fast:
	go build -o $(BINARY) .

.PHONY: release
release: build
ifdef UPX
	@echo "Compressing binary with UPX..."
	$(UPX) --best $(BINARY)
else
	@echo "UPX not found — skipping compression."
endif

.PHONY: install
install:
	$(BUILD_ENV) go install $(LDFLAGS) $(TAGS) -trimpath .

# -------------------------------------------------------------------
# Dictionary
# -------------------------------------------------------------------

.PHONY: dict
dict: dictionary.txt.zst

dictionary.txt.zst: dictionary.csv gen_dict.go
	go run gen_dict.go

# -------------------------------------------------------------------
# Code quality
# -------------------------------------------------------------------

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: fmt-check
fmt-check:
	@unformatted=$$(go fmt ./...); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

.PHONY: staticcheck
staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

# -------------------------------------------------------------------
# Testing
# -------------------------------------------------------------------

.PHONY: test
test:
	go test ./... -count=1

.PHONY: test-race
test-race:
	go test ./... -race -count=1

.PHONY: test-short
test-short:
	go test ./... -short -count=1

# -------------------------------------------------------------------
# Benchmarks
# -------------------------------------------------------------------

.PHONY: bench
bench: dictionary.txt.zst
	go test ./... -bench=. -benchmem -count=3 -timeout=90s

.PHONY: bench-cmp
bench-cmp: dictionary.txt.zst
	@echo "=== Full dictionary load (decompress + scan into map) ==="
	@go test ./... -bench='BenchmarkGzipDecompress$$|BenchmarkZstdDecompress$$' -benchmem -count=3 -timeout=60s 2>&1 | grep -E '^(Benchmark|ok |FAIL|---)' || true
	@echo ""
	@echo "=== Raw decompression throughput ==="
	@go test ./... -bench='BenchmarkGzipDecompressOnly$$|BenchmarkZstdDecompressOnly$$' -benchmem -count=3 -timeout=60s 2>&1 | grep -E '^(Benchmark|ok |FAIL|---)' || true

# -------------------------------------------------------------------
# Cleanup
# -------------------------------------------------------------------

.PHONY: clean
clean:
	rm -f $(BINARY) $(BINARY)-*
	rm -rf bin/ spell-check-report/ html/
