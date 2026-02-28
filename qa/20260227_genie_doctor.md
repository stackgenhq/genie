# genie doctor — configuration and connectivity validation

## Why

Users need a single command to verify that Genie’s config, MCP, SCM, and external secrets are correctly set up before running the agent. Without it, failures show up only at runtime with unclear causes.

## Problem

Misconfigurations (missing tokens, unreachable MCP servers, invalid SCM credentials, unresolved secrets) cause runtime errors that are hard to trace. There was no structured way to validate all of this in one go with clear error codes.

## Benefit

- **genie doctor** runs all checks in one pass and reports each problem with a stable **ErrCode** (e.g. GENIE_DOC_020, GENIE_DOC_031).
- Error codes are documented in the docs (Troubleshooting tab) with resolution steps.
- Covers: config file, external secrets, model_config, MCP servers, SCM, messenger.

## Arrange

- Genie built: `make only-build` (or `go build -o build/genie .`).
- Optional: a config file (e.g. `.genie.toml`) with or without MCP/SCM/messenger.

## Act

1. Run: `./build/genie doctor`
2. Optionally with config: `./build/genie doctor --config /path/to/.genie.toml`
3. With debug: `./build/genie doctor --log-level=debug`

## Assert

- Each reported issue is printed with: symbol (✗/⚠/✓), **ErrCode**, section, message, and detail.
- At the end: “Error codes: see docs (Troubleshooting / Error codes) for resolution steps.”
- If any check is an error, exit code is non-zero; otherwise zero.
- In the docs (Troubleshooting tab), every ErrCode (e.g. GENIE_DOC_001–GENIE_DOC_060) has a row with Code, Section, Message, Resolution.

## References

- `pkg/doctor/codes.go` — ErrCode constants.
- `pkg/doctor/doctor.go` — Check logic (config, secrets, model_config, MCP, SCM, messenger).
- `docs/data/docs.yml` — `troubleshooting.error_codes` table for resolution steps.
