# Contributing & Code Standards

## Languages
- **Go**: `gofmt` + `goimports` (format); `golangci-lint` (lint).
- **TypeScript/JS** (in `fe/` when added): `prettier` (format) + `eslint` (lint).
- **Markdown/JSON**: `prettier` (format).

## Go rules
- Keep `main()` only in `cmd/`.
- `internal/` is import-private. No cross-layer cycle imports.
- Errors: wrap with context (`fmt.Errorf("context: %w", err)`).
- Logging: structured with `zerolog`.
- Tests: table-driven where possible; short-running; `*_test.go`.

## Commit style
- Conventional commits (e.g., `feat(scope): add events repo`, `chore(ci): enable lint`).

## Pre-commit checklist
Run all checks before committing:
```bash
make precommit
```

This runs (in order):
1. `make fmt` - Format code with gofmt
2. `make tidy` - Tidy Go modules
3. `make lint` - Run golangci-lint
4. `make test` - Run all tests

Or run individual commands:
- `make fmt` - Format code
- `make tidy` - Tidy modules
- `make lint` - Run linter
- `make test` - Run tests

## Tooling
- Install golangci-lint: `brew install golangci-lint` or `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`
- Prettier (later in `fe/`): `npm i -D prettier eslint`
