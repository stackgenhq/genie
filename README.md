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

### Installation

```bash
# Get the genie out of the bottle
brew install stackgenhq/homebrew-stackgen/genie
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

### Environment Variables

Configuration values matching the pattern `${VAR_NAME}` will be automatically expanded from the environment variables.

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

**Built with ✨ by [Stackgen](https://stackgen.com).**
*Complex tasks are hard. Being a Genie is easy.*

---
