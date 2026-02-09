# Skills System

This directory contains example skills that demonstrate the agentskills.io specification.

## Skills

### Developer Persona Skills

#### kubernetes-debug
Debug Kubernetes pods, services, and deployments with comprehensive diagnostic tools.
- Get pod logs with filtering
- Describe resources in detail
- Check pod health and status
- Analyze container issues

#### docker-optimize
Analyze and optimize Dockerfiles for better performance, security, and smaller image sizes.
- Security best practices validation
- Multi-stage build recommendations
- Layer optimization suggestions
- Image size reduction tips

#### git-workflow
Automate common Git workflows including feature branches, rebasing, and pull request preparation.
- Create feature branches with naming conventions
- Prepare branches for PR with rebase
- Generate PR descriptions
- Clean up merged branches

#### code-review-checklist
Generate comprehensive code review checklists tailored to programming language and framework.
- Language-specific checklists (Python, Go, JavaScript, etc.)
- Framework-specific checks (Django, React, Gin, etc.)
- Security and performance considerations
- Testing and documentation requirements

### DevOps Persona Skills

#### terraform-validate
Validate Terraform configurations and enforce best practices for infrastructure as code.
- Syntax and format validation
- Security best practices checks
- Cost optimization recommendations
- Compliance verification

#### aws-cost-analyzer
Analyze AWS resources for cost optimization opportunities and generate savings recommendations.
- EC2 instance right-sizing
- Unattached EBS volumes detection
- Reserved instance opportunities
- S3 lifecycle policy recommendations

#### monitoring-setup
Set up monitoring dashboards and alerts for applications and infrastructure using Prometheus and Grafana.
- Generate Prometheus configurations
- Create Grafana dashboards
- Configure alerting rules
- SLO/SLI monitoring setup

#### incident-response
Guide through incident response procedures with runbooks and automated response tasks.
- Structured incident workflow
- Diagnostic automation
- Timeline documentation
- Post-mortem templates

### Example Skill

#### example-skill

A simple text processing skill that demonstrates:
- Reading input files
- Processing text (uppercase conversion, word counting)
- Writing output files

## Usage

To use these skills with Genie:

1. Set the skills roots in your configuration:

```toml
# genie.toml
skills_roots = ["./examples/skills"]
```

Or use the environment variable:

```bash
export SKILLS_ROOT=./examples/skills
```

2. The skills will be automatically loaded and available as tools:
   - `list_skills` - List all available skills
   - `skill_load` - Load a skill's instructions
   - `skill_run` - Execute a skill's script

## Creating New Skills

To create a new skill:

1. Create a directory with a lowercase, hyphenated name (e.g., `my-skill`)
2. Create a `SKILL.md` file with YAML frontmatter:

```markdown
---
name: my-skill
description: A brief description of what this skill does
---

# My Skill

Detailed instructions on how to use this skill...

## Usage

\`\`\`bash
python3 scripts/run.py <args>
\`\`\`
```

3. (Optional) Add scripts in a `scripts/` directory
4. (Optional) Add reference documentation in `references/` directory
5. (Optional) Add assets in `assets/` directory

## Skill Naming Rules

Skill names must:
- Be lowercase
- Use hyphens to separate words (not underscores or spaces)
- Only contain letters (a-z), numbers (0-9), and hyphens (-)
- Not start or end with a hyphen
- Not contain consecutive hyphens
- Be 64 characters or less

Valid examples: `text-processor`, `image-resizer`, `data-analyzer`
Invalid examples: `Text-Processor`, `image_resizer`, `-data-analyzer`, `skill--name`
