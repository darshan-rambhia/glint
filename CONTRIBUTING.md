# Contributing to Glint

Thanks for your interest in contributing to Glint! This document covers the process for contributing to this project.

## Getting Started

### Prerequisites

- Go 1.26+
- CGO enabled (required for SQLite)
- [Task](https://taskfile.dev/) (task runner)
- [templ](https://templ.guide/) (template code generation)

```bash
go install github.com/a-h/templ/cmd/templ@latest
go install github.com/go-task/task/v3/cmd/task@latest
```

### Setup

```bash
git clone https://github.com/darshan-rambhia/glint.git
cd glint
task build
```

### Running Locally

```bash
cp glint.example.yml glint.yml
# Edit glint.yml with your PVE/PBS credentials
task run
```

## Development Workflow

1. Fork the repository
2. Create a feature branch from `main` (`git checkout -b feature/my-change`)
3. Make your changes
4. Run the checks:

```bash
task generate    # Regenerate templ templates
task test        # Run tests
task lint        # Run linter
task build       # Verify it compiles
```

5. Commit your changes (see commit conventions below)
6. Push to your fork and open a Pull Request

## Code Style

- Follow standard Go conventions (`gofmt`, `goimports`)
- Run `task fmt` before committing
- The project uses [golangci-lint](https://golangci-lint.run/) with a comprehensive config — run `task lint` to check
- Template files (`.templ`) are formatted with `templ fmt`

## Commit Conventions

This project uses conventional-style commit messages:

```
feat: add disk temperature history graph
fix: handle nil pointer in PBS task parsing
docs: update configuration reference
test: add coverage for alerter cleanup
chore: update Go dependencies
```

Prefix with the type, then a short lowercase description. No period at the end.

## Testing

- Add tests for new functionality
- Maintain or improve coverage — run `task test:coverage` to check
- Tests use `github.com/stretchr/testify` for assertions
- Place test files alongside the code they test (`foo_test.go` next to `foo.go`)

## Project Structure

```
cmd/glint/           # Entry point
internal/
  api/               # HTTP handlers and middleware
  alerter/           # Alert rule evaluation
  cache/             # In-memory state cache
  collector/         # PVE and PBS data collectors
  config/            # Configuration loading and validation
  model/             # Shared data types
  notify/            # Notification providers (ntfy, webhook)
  smart/             # S.M.A.R.T. attribute parsing
  store/             # SQLite persistence and pruning
templates/           # templ HTML templates
  components/        # Reusable template components
static/              # CSS and JS assets
docs/                # MkDocs documentation
```

## Reporting Issues

- Use the [bug report](https://github.com/darshan-rambhia/glint/issues/new?template=bug_report.yml) template for bugs
- Use the [feature request](https://github.com/darshan-rambhia/glint/issues/new?template=feature_request.yml) template for ideas
- Check existing issues before opening a new one

## Security

If you discover a security vulnerability, please follow the process in [SECURITY.md](SECURITY.md). Do not open a public issue for security vulnerabilities.
