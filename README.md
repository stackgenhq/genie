# 🧞 genie (by Stackgen)

Generative Engine for Natural Intent Execution

> **"Your intent is my command. YOU get a stack! YOU get a stack!"**

`genie` is the world’s first **Agentic IaC CLI**, powered by [Stackgen](https://stackgen.com). We’re moving beyond the era of manual configuration and into the era of **Intent-to-Infrastructure**.

Stop writing YAML. Stop debugging Terraform modules. Just tell `genie` what you need, and consider it granted.

---

## ✨ The Stackgen Magic

`genie` doesn't just "template" files. It uses Stackgen’s core engine to understand your application’s requirements and synthesize a bespoke, production-ready stack.

* **Intent-Based:** "I need a scalable Node.js API on AWS with a Redis cache." Done.
* **DevEx 2.0:** Built for developers who want to ship, not for "YAML plumbers."
* **Agentic Intelligence:** The CLI understands context, security best practices, and cost-optimization automatically.
* **Zero Bottlenecks:** No more waiting for the DevOps team to "bless" your infrastructure. `genie` generates it pre-blessed.

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

| `genie` or `genie grant` | Interactive Intent-to-Infrastructure wizard. | "Your wish is my command." |

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
[architect]
google_search_api_key = "your-key"

[ops]
max_pages = 5
enable_verification = true

[secops.severity_thresholds]
medium = 10
```

---

## 📖 The "Genie" Workflow

1. **Rub the Lamp:** Call `genie` (or `genie grant`).
2. **Speak your Intent:** Describe your app (Language, Cloud, Database, Traffic expectations).
3. **Receive the Gift:** `genie` synthesizes the Terraform/Pulumi code using Stackgen’s agentic logic.
4. **Ship it:** Deploy immediately with built-in guardrails.

> **Why call it Genie?** Because at Stackgen, we believe infrastructure should be so easy it feels like magic. We’re being **Gen-erous** with the power of the cloud.

---

## 🤝 Join the Movement

We are redefining DevEx. If you want to contribute to the **Agentic IaC** revolution, check out our [contribution guidelines](./CONTRIBUTING.md).

1. Fork the repo.
2. Create your "Wish" (feature branch).
3. Submit a PR.

---

**Built with ✨ by [Stackgen](https://stackgen.com).**
*Infrastructure is hard. Being a Genie is easy.*

---

