# Contributing to Genie

Thanks for your interest in contributing! Here's how to get started.

## Prerequisites

- **Go 1.25+**
- **CGO enabled** (required for SQLite via GORM)
- A GitHub account

## Getting Started

```bash
git clone https://github.com/appcd-dev/stackgen-genie.git
cd stackgen-genie
make test
```

## Making Changes

1. Fork the repository
2. Create a feature branch: `git checkout -b feat/my-change`
3. Make your changes
4. Run `make fmt/fix` to format code
5. Run `make test` to ensure tests pass
6. Commit with a clear message
7. Push and open a PR against `main`

## Code Style

- Run `make fmt/fix` before committing — CI will reject unformatted code
- Write tests for new functionality
- Use Go's standard error wrapping (`fmt.Errorf("context: %w", err)`)
- Keep packages focused and well-documented

## Pull Requests

- Keep PRs focused — one logical change per PR
- Include a clear description of what and why
- Link related issues if applicable
- All CI checks must pass before merge

## Reporting Issues

Open a [GitHub Issue](https://github.com/appcd-dev/stackgen-genie/issues/new) with:

- What you expected vs. what happened
- Steps to reproduce
- Go version and OS

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
