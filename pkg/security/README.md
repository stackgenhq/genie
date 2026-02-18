# 🔐 pkg/security — Enterprise Secret Management

> **Zero-Trust Secret Resolution** for production-grade AI agent deployments.

## Overview

The `security` package provides a centralized, pluggable secret management layer
that replaces scattered `os.Getenv` calls with a single `SecretProvider` interface.
Secrets can be resolved from cloud-native secret stores (GCP Secret Manager,
AWS Secrets Manager, Azure Key Vault, HashiCorp Vault, etc.) or fall back to
environment variables for local development — all without changing a single line
of application code.

Built on [gocloud.dev/runtimevar](https://gocloud.dev/howto/runtimevar/), the
package supports any backend with a Go CDK URL scheme.

## Architecture

```
┌───────────────────────────────────────────────────────────────────┐
│                       SecretProvider                              │
│                    (interface: GetSecret)                         │
├──────────────────────────────┬────────────────────────────────────┤
│                              │                                    │
│     ┌────────────────┐       │       ┌──────────────────────┐     │
│     │  envProvider    │       │       │  Manager             │     │
│     │                │       │       │                      │     │
│     │  os.Getenv()   │       │       │  runtimevar URL →    │     │
│     │  zero-config   │       │       │  lazy open + cache   │     │
│     │  backward      │       │       │  env fallback        │     │
│     │  compatible    │       │       │                      │     │
│     └────────────────┘       │       └──────┬───────────────┘     │
│                              │              │                     │
│                              │     ┌────────▼─────────┐          │
│                              │     │ gocloud.dev       │          │
│                              │     │ runtimevar        │          │
│                              │     │                   │          │
│                              │     │ • GCP SM          │          │
│                              │     │ • AWS SM          │          │
│                              │     │ • Azure KV        │          │
│                              │     │ • File            │          │
│                              │     │ • Constant (test) │          │
│                              │     └───────────────────┘          │
└───────────────────────────────────────────────────────────────────┘
```

## Quick Start

### Local Development (zero-config)

No configuration needed. The default `envProvider` reads from environment variables:

```go
sp := security.NewEnvProvider()
val, err := sp.GetSecret(ctx, "OPENAI_API_KEY")
// equivalent to os.Getenv("OPENAI_API_KEY")
```

### Production (cloud secret store)

Configure the `[security]` section in your `.genie.toml`:

```toml
[security]

[security.secrets]
OPENAI_API_KEY    = "gcpsecretmanager://projects/my-project/secrets/openai-key?decoder=string"
ANTHROPIC_API_KEY = "awssecretsmanager://my-secret?region=us-east-1&decoder=string"
LANGFUSE_SECRET   = "azurekeyvault://my-vault.vault.azure.net/secrets/langfuse?decoder=string"
```

Then in Go:

```go
mgr := security.NewManager(cfg.Security)
defer mgr.Close()

// Resolves from GCP Secret Manager, AWS, Azure, etc.
val, err := mgr.GetSecret(ctx, "OPENAI_API_KEY")

// Unmapped secrets fall back to os.Getenv
val, err := mgr.GetSecret(ctx, "SOME_LOCAL_VAR")
```

## Components

### `SecretProvider` (interface)

```go
type SecretProvider interface {
    GetSecret(ctx context.Context, name string) (string, error)
}
```

The core contract. Every component that needs a secret depends on this interface,
making it trivial to mock in tests.

### `envProvider`

- Created via `NewEnvProvider()`
- Wraps `os.Getenv` — always returns `""` with `nil` error for missing vars
- Default for local development and backward compatibility

### `Manager`

- Created via `NewManager(cfg Config) *Manager`
- Resolves secrets from `gocloud.dev/runtimevar` URL mappings
- **Lazy loading**: Variables are opened on first access and cached
- **Thread-safe**: Uses `sync.RWMutex` with double-check locking
- **Env fallback**: Unmapped secret names fall through to `os.Getenv`
- **Must close**: Call `Close()` to release runtimevar connections

### `Config`

```go
type Config struct {
    Secrets map[string]string `yaml:"secrets" toml:"secrets"`
}
```

Maps secret names to [runtimevar URL](https://gocloud.dev/concepts/urls/) strings.

## Supported Backends

| Backend | URL Scheme | Example |
|---------|-----------|---------|
| GCP Secret Manager | `gcpsecretmanager://` | `gcpsecretmanager://projects/p/secrets/s?decoder=string` |
| AWS Secrets Manager | `awssecretsmanager://` | `awssecretsmanager://my-secret?region=us-east-1&decoder=string` |
| AWS Parameter Store | `awsparamstore://` | `awsparamstore://my-param?region=us-east-1&decoder=string` |
| Azure Key Vault | `azurekeyvault://` | `azurekeyvault://vault.vault.azure.net/secrets/name?decoder=string` |
| etcd | `etcd://` | `etcd://my-key?decoder=string` |
| File (watch) | `file://` | `file:///etc/secrets/api-key?decoder=string` |
| Constant (testing) | `constant://` | `constant://?val=test-value&decoder=string` |

> **Note**: Each backend requires the corresponding Go CDK driver import
> in your `main.go` or an init package. Without the import, `runtimevar.OpenVariable`
> will return `"no driver registered"` at runtime.

### Required Driver Imports

Add blank imports for the backends you use:

```go
import (
    // Always available (tests + local dev)
    _ "gocloud.dev/runtimevar/constantvar"

    // Production backends — import only what you need:
    _ "gocloud.dev/runtimevar/gcpsecretmanager"  // GCP
    _ "gocloud.dev/runtimevar/awssecretsmanager" // AWS Secrets Manager
    _ "gocloud.dev/runtimevar/awsparamstore"     // AWS Parameter Store
    // _ "gocloud.dev/runtimevar/azurekeyvault"   // Azure (requires additional setup)
    // _ "gocloud.dev/runtimevar/etcdvar"         // etcd
    _ "gocloud.dev/runtimevar/filevar"            // File-based (watched)
)
```

> The `constantvar` driver is imported by `manager.go` automatically.
> Cloud drivers are **not** imported by default to avoid pulling in cloud SDKs
> that projects may not need.

## Testing

### Using the `constant://` driver

```go
cfg := security.Config{
    Secrets: map[string]string{
        "MY_SECRET": "constant://?val=test-value&decoder=string",
    },
}
mgr := security.NewManager(cfg)
defer mgr.Close()

val, err := mgr.GetSecret(ctx, "MY_SECRET")
// val == "test-value"
```

### Using counterfeiter fakes

```go
//go:generate go tool counterfeiter -generate

mockSP := &fakes.FakeSecretProvider{}
mockSP.GetSecretReturns("my-api-key", nil)

val, _ := mockSP.GetSecret(ctx, "OPENAI_API_KEY")
// val == "my-api-key"
```

## File Index

| File | Purpose |
|------|---------|
| `provider.go` | `SecretProvider` interface + counterfeiter annotation |
| `manager.go` | `Manager` — runtimevar-backed provider with caching |
| `env_provider.go` | `envProvider` — zero-config `os.Getenv` fallback |
| `config.go` | `Config` struct for YAML/TOML deserialization |
| `fakes.go` | Counterfeiter `go:generate` directive |
| `security_test.go` | Ginkgo tests (13 specs) |
| `init_test.go` | Ginkgo suite bootstrap |
