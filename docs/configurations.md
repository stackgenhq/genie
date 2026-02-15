# Genie Configuration

Genie supports configuration via YAML and TOML files. This document details all available configuration options.

## File Format

The configuration file is structured into sections corresponding to different components of Genie.

### `ops`

Configuration for the Operations (IaC Generator) agent.

| Key | Type | Default | Description |
|---|---|---|---|
| `max_pages` | int | `5` | Maximum number of documentation pages to read during research. |
| `enable_verification` | bool | `true` | Whether to run verification steps (like `terraform validate`) after generation. |
| `max_verification_runs` | int | `3` | Maximum number of times to retry verification/failed generation fix loops. |

### `secops`

Configuration for Security Operations checks.

#### `severity_thresholds`

Defines the maximum allowed number of security violations per severity level. If the violations exceed these thresholds, the check fails.

| Key | Type | Default | Description |
|---|---|---|---|
| `high` | int | `0` | Maximum allowed HIGH severity issues. |
| `medium` | int | `42` | Maximum allowed MEDIUM severity issues. |
| `low` | int | `-1` | Maximum allowed LOW severity issues. `-1` means unlimited. |

### `model_config`

Configuration for the LLM providers.

#### `providers`

A list of provider configurations.

| Key | Type | Description |
|---|---|---|
| `provider` | string | The model provider name (e.g., `openai`, `gemini`). |
| `model_name` | string | The specific model ID to use (e.g., `gpt-4`, `gemini-pro`). |
| `variant` | string | Variant of the model/client (e.g., `default`, `azure`). |
| `good_for_task` | string | Task specialization (e.g., `planning`, `coding`). |

## Environment Variables

You can use environment variables in your configuration file using the `${VAR_NAME}` syntax.

Example:

```yaml
ops:
  max_pages: ${MAX_PAGES}
```
