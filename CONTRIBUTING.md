# Contributing to Genie

Thanks for your interest in contributing! Here's how to get started.

## Prerequisites

| Requirement | Details |
|---|---|
| **Go** | 1.25+ (see `go.mod` for exact version) |
| **CGO** | `CGO_ENABLED=1` — required for SQLite via GORM. Enabled by default on macOS and most Linux distros. |
| **OS** | macOS (amd64/arm64), Linux (amd64/arm64/ppc64le/s390x), Windows (amd64) |
| **External tools** | None beyond the Go toolchain. Optional: `golangci-lint` (installed by `make deps`). |

## Getting Started

```bash
git clone https://github.com/stackgenhq/genie.git
cd genie

# Bootstrap: install deps, run go generate (creates *fakes packages), tidy, vendor
make setup

# Verify everything works
make test
```

> **Why `make setup`?** The test suite imports counterfeiter-generated `*fakes` packages (e.g. `pkg/expert/expertfakes`). These **are committed to the repo** for convenience, but if they are stale or missing after an interface change, run `make setup` to regenerate them.

## Making Changes

1. Fork the repository
2. Create a feature branch: `git checkout -b feat/my-change`
3. Make your changes
4. Run `make fmt/fix` to format code
5. Run `make test` to ensure tests pass
6. Run `make lint` to check for lint errors
7. Commit with a clear message
8. Push and open a PR against `main`

## Regenerating Fakes

If you modify an interface that has a `//counterfeiter:generate` annotation:

```bash
make generate   # runs go generate ./...
```

This regenerates all `*fakes` packages. Commit the regenerated fakes alongside your interface changes so CI and other developers don't have to regenerate them.

## Code Style

- Run `make fmt/fix` before committing — CI will reject unformatted code
- Write tests for new functionality (Ginkgo/Gomega style)
- Use Go's standard error wrapping (`fmt.Errorf("context: %w", err)`)
- Keep packages focused and well-documented
- Follow the coding standards in [Agents.md](./Agents.md)

## Testing Policy

> All new features and bug fixes **MUST** include automated tests.

- **New functionality**: Every PR that adds a feature must include corresponding test cases in the relevant `*_test.go` file(s).
- **Bug fixes**: Every bug-fix PR must include a regression test that fails without the fix and passes with it.
- **Test framework**: Use [Ginkgo](https://onsi.github.io/ginkgo/) + [Gomega](https://onsi.github.io/gomega/) for all Go tests.
- **Coverage**: CI enforces a minimum coverage threshold. Run `make test` locally to verify.
- **Race detection**: Tests run with `-race` enabled — avoid data races.

PRs that do not include tests for new or changed behavior will be asked to add them before merge.

## Build Commands

| Command | Description |
|---|---|
| `make setup` | Full bootstrap (deps + generate + tidy + vendor) |
| `make build` | Build the `genie` binary to `build/genie` |
| `make install` | `go install` to `$GOPATH/bin` |
| `make test` | Run all unit tests with Ginkgo |
| `make lint` | Run golangci-lint |
| `make fmt/fix` | Auto-format all Go files |
| `make generate` | Run `go generate ./...` (regenerate fakes) |

## Pull Requests

- Keep PRs focused — one logical change per PR
- Include a clear description of what and why
- Link related issues if applicable
- All CI checks must pass before merge

## Reporting Issues

Open a [GitHub Issue](https://github.com/stackgenhq/genie/issues/new) with:

- What you expected vs. what happened
- Steps to reproduce
- Go version and OS

## Community

- Please read and follow our [Code of Conduct](./CODE_OF_CONDUCT.md).
- To report security vulnerabilities, see [SECURITY.md](./SECURITY.md).

## License and CLA

Genie uses a dual licensing model (Apache 2.0 + BSL 1.1). By contributing, you agree to sign our [Contributor License Agreement (CLA)](./LICENSING.md), which grants StackGen the right to license your contributions under these terms. You must complete the CLA before your first pull request can be merged. When you open a pull request, a CLA status check will appear with a link to review and sign the CLA using your GitHub account; once you have signed it, the check will pass automatically for this and future PRs. See [LICENSING.md](./LICENSING.md) for details on the licensing model.
