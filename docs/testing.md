# Testing

Glint uses Go's built-in testing framework with `testify/assert` for assertions. The test suite includes unit tests, benchmark tests, fuzz tests, and integration tests.

## Running Tests

```bash
# Run all tests
task test

# Run tests with race detector
task test:race

# Run tests with coverage tracking (enforces minimum threshold)
task test:coverage
```

---

## Coverage

Coverage is tracked via `buildscripts/coverage/main.go` with an auto-ratcheting threshold stored in `buildscripts/coverage/coverage_required.txt`.

```bash
# Run coverage check
task test:coverage
```

The coverage tool will:

1. Run all tests with `-race` and `-coverprofile`
2. Print per-function coverage
3. Auto-ratchet the threshold upward if coverage improved
4. Fail the build if coverage dropped below threshold
5. Generate an HTML report at `target/reports/coverage.html`

!!! info "Ratcheting threshold"
    The coverage threshold only goes up, never down. If a PR drops coverage below the threshold, CI will fail.

View the HTML coverage report:

```bash
open target/reports/coverage.html
```

---

## Benchmark Tests

Benchmark tests measure the performance of hot paths. They live alongside unit tests in `*_test.go` files.

### Running Benchmarks

```bash
# Run all benchmarks
go test ./... -bench=. -benchmem -count=3 -run=^$

# Run benchmarks for a specific package
go test ./internal/smart/... -bench=. -benchmem -count=3 -run=^$

# Run a specific benchmark
go test ./internal/smart/... -bench=BenchmarkEvaluateDisk -benchmem -count=5 -run=^$

# Compare before/after
go test ./... -bench=. -benchmem -count=5 -run=^$ > bench-before.txt
# ... make changes ...
go test ./... -bench=. -benchmem -count=5 -run=^$ > bench-after.txt
go install golang.org/x/perf/cmd/benchstat@latest
benchstat bench-before.txt bench-after.txt
```

### Current Benchmarks

| Package | Benchmark | Description |
|---------|-----------|-------------|
| `internal/smart` | `BenchmarkEvaluateDisk` | Evaluates all SMART attributes for a 12-attribute disk (~228ns/op) |

### Writing Benchmarks

```go
func BenchmarkMyFunction(b *testing.B) {
    input := prepareInput()  // setup (not timed)
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        MyFunction(input)
    }
}
```

Good candidates for benchmarks:

- SMART threshold evaluation (called per-disk, per-attribute)
- Cache snapshot creation (called on every HTTP request)
- SQLite insert/query cycles (called every poll interval)
- Template rendering (called on every page load)

---

## Fuzz Tests

Fuzz tests exercise parsers with random inputs to find panics, crashes, and unexpected behavior. They target code that handles untrusted external data (PVE/PBS API responses).

### Running Fuzz Tests

```bash
# Run all fuzz tests for 30 seconds each
go test ./internal/smart/... -fuzz=. -fuzztime=30s

# Run a specific fuzz test
go test ./internal/smart/... -fuzz=FuzzParseATARaw -fuzztime=60s

# Longer duration for deeper coverage
go test ./internal/smart/... -fuzz=FuzzParseNVMeText -fuzztime=5m

# Fuzz the collector response parsers
go test ./internal/collector/... -fuzz=FuzzParseNodeStatus -fuzztime=30s
```

### Current Fuzz Targets

| Package | Fuzz Test | Input | Purpose |
|---------|-----------|-------|---------|
| `internal/smart` | `FuzzParseATARaw` | Random ATA `raw` strings | Tests raw value extraction from strings like `"40 (Min/Max 25/55)"` |
| `internal/smart` | `FuzzParseNVMeText` | Random smartctl text output | Tests NVMe field extraction from free-form text |
| `internal/collector` | `FuzzParseNodeStatus` | Random JSON | Tests PVE node status response parsing |
| `internal/collector` | `FuzzParseLoadAvg` | Random JSON arrays | Tests loadavg parsing (strings vs floats) |
| `internal/collector` | `FuzzParseSensorsJSON` | Random JSON | Tests `sensors -j` output parsing |

### Writing Fuzz Tests

```go
func FuzzMyParser(f *testing.F) {
    // Seed corpus with known-good inputs
    f.Add([]byte(`{"valid": "input"}`))
    f.Add([]byte(`{"edge": "case"}`))

    f.Fuzz(func(t *testing.T, data []byte) {
        result, err := MyParser(data)
        if err != nil {
            return // errors are fine, panics are not
        }
        // Validate invariants on successful parse
        if result.Value < 0 {
            t.Errorf("negative value: %d", result.Value)
        }
    })
}
```

!!! tip "Fuzz corpus"
    Fuzz corpus files are stored in `testdata/fuzz/` and committed to git. When a fuzz test finds a crash, the failing input is saved automatically and becomes a regression test.

### Best Practices

- **Seed with real data:** Add actual PVE/PBS API responses as seed corpus entries
- **Target parsers:** Focus on functions that parse external/untrusted data
- **Check invariants:** Beyond "no panic", validate that output makes sense
- **Run in CI:** Fuzz tests run as regular tests (without `-fuzz` flag) using only the seed corpus, catching regressions

---

## Integration Tests

Integration tests run against real Proxmox VE and PBS APIs. They are tagged with `//go:build integration` and skipped in normal test runs.

```bash
GLINT_PVE_URL=https://192.168.1.215:8006 \
GLINT_PVE_TOKEN_ID=glint@pam!monitor \
GLINT_PVE_TOKEN_SECRET=xxx \
go test ./... -tags=integration -v -count=1
```

---

## Linting

```bash
# Run all linters
task lint

# Run with auto-fix
golangci-lint run --fix ./...
```

See `.golangci.yml` for the full linter configuration. Key linters enabled:

| Linter | Purpose |
|--------|---------|
| `errcheck` | Unchecked errors |
| `gosec` | Security issues |
| `errorlint` | Proper error wrapping |
| `gocritic` | Opinionated code quality |
| `revive` | Go style conventions |
| `testifylint` | testify best practices |

---

## Markdown Linting

Markdown files are linted with [markdownlint](https://github.com/DavidAnson/markdownlint-cli2):

```bash
# Install
npm install -g markdownlint-cli2

# Lint all markdown
markdownlint-cli2 "**/*.md"

# Fix auto-fixable issues
markdownlint-cli2 --fix "**/*.md"
```

Configuration is in `.markdownlint.yaml`.
