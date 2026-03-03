# 🧞 genie (by Stackgen)

[![Go Reference](https://pkg.go.dev/badge/github.com/stackgenhq/genie.svg)](https://pkg.go.dev/github.com/stackgenhq/genie)
[![Go Report Card](https://goreportcard.com/badge/github.com/stackgenhq/genie)](https://goreportcard.com/report/github.com/stackgenhq/genie)
[![CI](https://github.com/stackgenhq/genie/actions/workflows/go-test.yml/badge.svg)](https://github.com/stackgenhq/genie/actions/workflows/go-test.yml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/12058/badge)](https://www.bestpractices.dev/projects/12058)
[![Release](https://img.shields.io/github/v/release/stackgenhq/genie)](https://img.shields.io/github/v/release/stackgenhq/genie)
[![Scorecard](https://api.scorecard.dev/projects/github.com/stackgenhq/genie/badge)](https://scorecard.dev/viewer/?uri=github.com/stackgenhq/genie)
[![codecov](https://codecov.io/gh/stackgenhq/genie/graph/badge.svg?token=8DPGF0MP08)](https://codecov.io/gh/stackgenhq/genie)

Generative Engine for Natural Intent Execution

> **"Your intent is my command."**

`genie` is an **Enterprise Agentic Platform** powered by [Stackgen](https://stackgen.com). Describe what you need — across code, operations, security, and beyond — and let `genie` plan and execute it.

> **🚧 Early Stage — API Surface Evolving**
>
> Genie is under active development and not yet at v1.0. Library interfaces, method signatures, and configuration schemas may change between minor releases as we refine the platform based on real-world usage and community feedback. We follow [Semantic Versioning](https://semver.org/) — breaking changes are always documented in the [CHANGELOG](./CHANGELOG.md). If you're building on top of Genie, we recommend pinning to a specific release and watching the changelog for migration notes.

---

## ✨ Enterprise Differentiators

`genie` is a platform designed for reliability, compliance, and scale.

* **ReAcTree Execution Engine**: A deterministic, multi-stage reasoning engine that ensures verifiable plans.
* **Agentic Execution**: Plans, decomposes, and executes complex multi-step tasks autonomously with sub-agent orchestration.
* **Governance & Audit**: Built-in Human-in-the-Loop (HITL) guardrails and immutable audit logs ensure strict compliance.
* **Multi-Model Routing**: Intelligently routes tasks to the best model (OpenAI, Gemini, Anthropic, Ollama, HuggingFace) for cost and performance.
* **Enterprise Integrations**: Seamlessly integrates with **OpsVerse ObserveNow** for full-stack observability and **Aiden** for automated incident response.
* **Skills & MCP**: Extensible via a file-based skills system and full MCP protocol support.

---

## 🚀 Get Started

### Prerequisites

* **Go 1.25+** (only for building from source)
* **CGO enabled** — required for SQLite via GORM (`CGO_ENABLED=1` is set by default on most platforms)

### Installation

**Homebrew (macOS / Linux):**

```bash
brew install stackgenhq/homebrew-stackgen/genie
```

**Go install:**

```bash
CGO_ENABLED=1 go install -mod=mod github.com/stackgenhq/genie@latest
```

**Build from source:**

```bash
git clone https://github.com/stackgenhq/genie.git
cd genie
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
[Releases](https://github.com/stackgenhq/genie/releases) page.

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

### Deploying on Cloud Instances (EC2, Azure VM, GCP Compute)

Want to run Genie as a shared service for your team? Deploy it on a cloud instance with persistent storage and systemd management.

#### Option 1: Docker Deployment (Recommended)

```bash
# 1. Install Docker on your instance
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# 2. Create a config directory
mkdir -p ~/.genie
nano ~/.genie/genie.toml  # Add your configuration

# 3. Run Genie with Docker
docker run -d \
  --name genie \
  --restart unless-stopped \
  -p 9876:9876 \
  -v ~/.genie:/home/genie/.config \
  -v /var/lib/genie/data:/workspace \
  -e OPENAI_API_KEY="${OPENAI_API_KEY}" \
  ghcr.io/stackgenhq/genie:latest grant
```

#### Option 2: Binary Installation with Systemd

**For EC2 (Amazon Linux / Ubuntu):**

```bash
# 1. Download and install the binary
curl -L https://github.com/stackgenhq/genie/releases/latest/download/genie_Linux_x86_64.tar.gz -o genie.tar.gz
tar xzf genie.tar.gz
sudo mv genie /usr/local/bin/
sudo chmod +x /usr/local/bin/genie

# 2. Create a genie user
sudo useradd -r -s /bin/false genie
sudo mkdir -p /etc/genie /var/lib/genie
sudo chown genie:genie /var/lib/genie

# 3. Create configuration
sudo nano /etc/genie/genie.toml
# Add your model config, see config.toml.example

# 4. Create systemd service
sudo tee /etc/systemd/system/genie.service > /dev/null <<EOF
[Unit]
Description=Genie Agentic Platform
After=network.target

[Service]
Type=simple
User=genie
Group=genie
WorkingDirectory=/var/lib/genie
Environment="OPENAI_API_KEY=your-key-here"
ExecStart=/usr/local/bin/genie grant --config /etc/genie/genie.toml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# 5. Start the service
sudo systemctl daemon-reload
sudo systemctl enable genie
sudo systemctl start genie
sudo systemctl status genie
```

#### Security Recommendations

* **Use IAM roles** (AWS) or equivalent for secret management instead of hardcoded keys
* **Configure firewall**: Only expose port 9876 to trusted IPs/VPCs
* **Enable HTTPS**: Use a reverse proxy (nginx/caddy) with Let's Encrypt certificates
* **Set rate limits**: Configure `[agui] rate_limit` and `max_concurrent` in your config
* **Regular updates**: Set up automated security updates for your instance
* **Use cloud secret managers**: Configure `[security.secrets]` for AWS Secrets Manager, GCP Secret Manager, or Azure Key Vault

**Example with AWS Secrets Manager:**

```toml
[security.secrets]
OPENAI_API_KEY = "awssecretsmanager://genie/openai?region=us-east-1&decoder=string"
ANTHROPIC_API_KEY = "awssecretsmanager://genie/anthropic?region=us-east-1&decoder=string"
```

**Nginx reverse proxy example:**

```nginx
server {
    listen 443 ssl http2;
    server_name genie.example.com;

    ssl_certificate /etc/letsencrypt/live/genie.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/genie.example.com/privkey.pem;

    location / {
        proxy_pass http://localhost:9876;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
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

* **Default (env vars):** Without a `[security]` section, placeholders resolve from environment variables — fully backward compatible.
* **Cloud / file backends:** Add a `[security.secrets]` section to resolve secrets from GCP Secret Manager, AWS Secrets Manager, Azure Key Vault, mounted files, or any [gocloud.dev/runtimevar](https://gocloud.dev/howto/runtimevar/) backend.
* **Diagnostics:** If a secret-like key (`token`, `api_key`, `password`, etc.) resolves to empty, `genie` emits a warning pointing to the missing variable name and config path.

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
