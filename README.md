# 🧞 genie (by Stackgen)

Generative Engine for Natural Intent Execution

> **"Your intent is my command. YOU get a stack! YOU get a stack!"**

`genie` is the **Enterprise Agentic Platform**, powered by [Stackgen](https://stackgen.com). We’re moving beyond the era of manual configuration.

Stop writing YAML. Stop debugging Terraform modules. Just tell `genie` what you need, and consider it granted.

---

## ✨ Enterprise Differentiators

`genie` is a platform designed for reliability, compliance, and scale.

*   **ReAcTree Execution Engine**: A deterministic, multi-stage reasoning engine that ensures verifiable plans.
*   **Infrastructure Automation**: Powered by Stackgen, Genie synthesizes production-ready Terraform and Pulumi from natural language.
*   **Governance & Audit**: Built-in Human-in-the-Loop (HITL) guardrails and immutable audit logs ensure strict compliance.
*   **Multi-Model Routing**: Intelligently routes tasks to the best model (OpenAI, Gemini, Anthropic, Ollama, HuggingFace) for cost and performance.
*   **Enterprise Integrations**: Seamlessly integrates with **OpsVerse ObserveNow** for full-stack observability and **Aiden** for automated incident response.

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

| `genie` or `genie grant` | Interactive Infrastructure Automation wizard. | "Your wish is my command." |

---

## ⚙️ Configuration

`genie` supports configuration via **YAML** or **TOML** files. You can customize the behavior of the Architect, Operations (IaC generation), and Security checks.

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
2. **Speak your Intent:** Describe your app (Language, Cloud, Database, Traffic expectations).
3. **Receive the Gift:** `genie` synthesizes the Terraform/Pulumi code using Stackgen’s agentic logic.
4. **Ship it:** Deploy immediately with built-in guardrails.

> **Why call it Genie?** Because at Stackgen, we believe infrastructure should be so easy it feels like magic. We’re being **Gen-erous** with the power of the cloud.

---

## 🤝 Join the Movement

We are redefining Agents. If you want to contribute to the **Agentic IaC** revolution, check out our [contribution guidelines](./CONTRIBUTING.md).

1. Fork the repo.
2. Create your "Wish" (feature branch).
3. Submit a PR.

---

**Built with ✨ by [Stackgen](https://stackgen.com).**
*Infrastructure is hard. Being a Genie is easy.*

---

