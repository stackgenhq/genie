# Feature: Keyring-Backed Secrets and back-to-bottle

## Why
This feature was developed so users can store API keys and tokens in the system keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service) instead of config files or environment variables. The `genie back-to-bottle` command then removes all Genie keyring entries for a clean reset or before handing off a machine.

## Problem
Previously, secrets had to live in config (as placeholders like `${VAR}`) or in environment variables. That made it harder to keep config files clean and to revoke access without editing files or unsetting env vars. There was no single command to clear all Genie-stored secrets from the device.

## Benefit
- **Clean config**: Config can reference `keyring://genie/ACCOUNT_NAME`; the secret is resolved at runtime from the keyring.
- **Setup wizard**: The wizard can store pasted tokens and API keys in the keyring so `.genie.toml` never contains raw secrets.
- **Reset**: `genie back-to-bottle` removes all known Genie keyring entries so credentials can be cleared in one step.

## Test 1: Store secret via setup and verify keyring reference

### Arrange
- Run `genie setup` (or equivalent flow that stores a secret in the keyring, e.g. API key or Telegram token).
- When prompted, provide a value and choose to store in keyring (or use the path that stores in keyring by default).

### Act
1. Complete setup.
2. Open the generated config file (e.g. `.genie.toml`).

### Assert
1. Config contains a keyring reference (e.g. `keyring:///OPENAI_API_KEY` or `keyring://genie/TELEGRAM_BOT_TOKEN`) under `[security.secrets]` or the relevant section, not the raw secret.
2. Genie can start and resolve the secret (e.g. agent runs or messenger connects using that token).

## Test 2: back-to-bottle removes keyring entries

### Arrange
- At least one Genie secret has been stored in the keyring (e.g. from Test 1 or a previous setup).
- CLI available: `genie back-to-bottle`.

### Act
1. Run `genie back-to-bottle`.

### Assert
1. Command exits with success (exit code 0).
2. Output indicates removal (e.g. “Removed N Genie keyring entry/entries” or “No Genie keyring entries found” if already empty).
3. After running, Genie no longer has access to that secret: start Genie and trigger a flow that needs the removed secret (e.g. OpenAI API key or Telegram token); expect a clear failure or error indicating the secret is missing, not a successful auth.

## Test 3: back-to-bottle when keyring is empty

### Arrange
- Keyring has no Genie entries (e.g. after Test 2 or on a fresh system).

### Act
1. Run `genie back-to-bottle`.

### Assert
1. Command exits with success.
2. Output states that no Genie keyring entries were found (e.g. “No Genie keyring entries found; nothing to remove.”).
