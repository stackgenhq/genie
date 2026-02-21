# 🧞 genie (by Stackgen)

Generative Engine for Natural Intent Execution

> **"Your intent is my command."**

`genie` is an **Enterprise Agentic Platform** powered by [Stackgen](https://stackgen.com). Describe what you need — across code, operations, security, and beyond — and let `genie` plan and execute it.

---

## ✨ Enterprise Differentiators

`genie` is a platform designed for reliability, compliance, and scale.

*   **ReAcTree Execution Engine**: A deterministic, multi-stage reasoning engine that ensures verifiable plans.
*   **Agentic Execution**: Plans, decomposes, and executes complex multi-step tasks autonomously with sub-agent orchestration.
*   **Governance & Audit**: Built-in Human-in-the-Loop (HITL) guardrails and immutable audit logs ensure strict compliance.
*   **Multi-Model Routing**: Intelligently routes tasks to the best model (OpenAI, Gemini, Anthropic, Ollama, HuggingFace) for cost and performance.
*   **Enterprise Integrations**: Seamlessly integrates with **OpsVerse ObserveNow** for full-stack observability and **Aiden** for automated incident response.
*   **Skills & MCP**: Extensible via a file-based skills system and full MCP protocol support.

---

## 🚀 Get Started

### Prerequisites

- **Go 1.25+** (only for building from source)
- **CGO enabled** — required for SQLite via GORM (`CGO_ENABLED=1` is set by default on most platforms)

### Installation

**Homebrew (macOS / Linux):**
```bash
brew install stackgenhq/homebrew-stackgen/genie
```

**Go install:**
```bash
CGO_ENABLED=1 go install -mod=mod github.com/appcd-dev/genie@latest
```

**Build from source:**
```bash
git clone https://github.com/appcd-dev/stackgen-genie.git
cd stackgen-genie
make build        # binary at build/genie
make install      # install to $GOPATH/bin
```

**Docker:**
```bash
docker run --rm -it \
  -v ~/.genie.toml:/home/genie/.genie.toml \
  -v $(pwd):/workspace \
  ghcr.io/stackgenhq/genie:latest grant
```

**GitHub Releases:**
Download pre-built binaries for macOS, Linux, and Windows from the
[Releases](https://github.com/appcd-dev/stackgen-genie/releases) page.

**Windows (Scoop):**
```powershell
scoop bucket add stackgen https://github.com/stackgenhq/homebrew-stackgen
scoop install genie
```

### Grant Your First Wish

Ready to see the magic? Run the interactive wizard:

```bash
genie
```

Or use the explicit command:

```bash
genie grant
```

---

## 🛠 Commands

| Command | Description |
|---|---|
| `genie` or `genie grant` | Interactive agentic wizard. |

---

## ⚙️ Configuration

`genie` supports configuration via **YAML** or **TOML** files. You can customize model routing, tool access, security policies, and more.

By default, `genie` looks for `.genie.yaml`, `.genie.yml`, `genie.yaml`, or `genie.yml` in the current directory, and then falling back to `$HOME/.genie.yaml`.

You can also specify a config file explicitly:

```bash
genie grant --config /path/to/my-config.toml
```

### Environment Variables & Secret Providers

Configuration values matching the pattern `${VAR_NAME}` are resolved through the configured **SecretProvider**:

- **Default (env vars):** Without a `[security]` section, placeholders resolve from environment variables — fully backward compatible.
- **Cloud / file backends:** Add a `[security.secrets]` section to resolve secrets from GCP Secret Manager, AWS Secrets Manager, Azure Key Vault, mounted files, or any [gocloud.dev/runtimevar](https://gocloud.dev/howto/runtimevar/) backend.
- **Diagnostics:** If a secret-like key (`token`, `api_key`, `password`, etc.) resolves to empty, `genie` emits a warning pointing to the missing variable name and config path.

See the [Security section](docs/docs.html) in the docs for full configuration details.

### Example Configuration

Check out [config.toml.example](./config.toml.example) for a full example of available options.

```toml
[[model_config]]
provider = "openai"
model_name = "gpt-5.2"
token = "${OPENAI_API_KEY}"
good_for_task = "planning"

[[model_config]]
provider = "ollama"
model_name = "llama3"
host = "http://localhost:11434"
good_for_task = "tool_calling"

# Skills: Enable reusable agent capabilities
skills_roots = ["./skills"]  # Paths to skills directories
```

### Skills System

`genie` supports a skills system based on the [agentskills.io specification](https://agentskills.io/specification). Skills are reusable, self-contained capabilities that agents can discover, load, and execute.

**Quick Start:**

1. Create a skills directory:
   ```bash
   mkdir -p skills/my-skill
   ```

2. Add a `SKILL.md` file with YAML frontmatter:
   ```markdown
   ---
   name: my-skill
   description: What this skill does
   ---
   
   # My Skill
   
   Detailed instructions...
   ```

3. Configure in `genie.toml`:
   ```toml
   skills_roots = ["./skills"]
   ```
   
   Or use environment variable:
   ```bash
   export SKILLS_ROOT=./skills
   ```

**Learn More:** See [docs/skills.md](./docs/skills.md) for complete documentation, examples, and best practices.

---

## 📖 The "Genie" Workflow

1. **Rub the Lamp:** Call `genie` (or `genie grant`).
2. **Speak your Intent:** Describe what you want to accomplish.
3. **Watch the Magic:** `genie` plans, decomposes, and executes using multi-model orchestration.
4. **Review & Ship:** Approve results through HITL guardrails and deploy with confidence.

> **Why call it Genie?** Because at Stackgen, we believe complex tasks should feel like magic. Just express your intent and consider it granted.

---

## 🤝 Join the Movement

We are redefining agentic automation. If you want to contribute, check out our [contribution guidelines](./CONTRIBUTING.md).

1. Fork the repo.
2. Create your feature branch.
3. Submit a PR.

---

## 📜 Community & Governance

| Document | Purpose |
|---|---|
| [Contributing Guide](./CONTRIBUTING.md) | How to contribute, build, and test |
| [Code of Conduct](./CODE_OF_CONDUCT.md) | Community behavior standards |
| [Security Policy](./SECURITY.md) | How to report vulnerabilities |
| [Changelog](./CHANGELOG.md) | Release notes and history |
| [License](./LICENSE) | Apache License 2.0 |

---

**Built with ✨ by [Stackgen](https://stackgen.com).**
*Complex tasks are hard. Being a Genie is easy.*

---
