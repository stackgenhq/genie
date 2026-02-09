# Skills System User Guide

## Overview

Genie supports a skills system based on the [agentskills.io specification](https://agentskills.io/specification). Skills are reusable, self-contained capabilities that agents can discover, load, and execute.

## Quick Start

### 1. Create a Skills Directory

```bash
mkdir -p skills/my-first-skill
```

### 2. Create a SKILL.md File

```markdown
---
name: my-first-skill
description: A simple example skill that processes text
---

# My First Skill

This skill demonstrates basic text processing.

## Usage

```bash
python3 scripts/process.py <input_file>
```

## Inputs

- `input.txt` - Text file to process

## Outputs

- `output.txt` - Processed text
```

### 3. Add a Script (Optional)

```bash
mkdir skills/my-first-skill/scripts
cat > skills/my-first-skill/scripts/process.py << 'EOF'
#!/usr/bin/env python3
import os
import sys

# Read from workspace input directory
input_dir = os.environ.get('WORKSPACE', '.') + '/input'
output_dir = os.environ.get('OUTPUT_DIR', './output')

# Process files
with open(f'{input_dir}/input.txt', 'r') as f:
    text = f.read()

# Simple processing: uppercase
processed = text.upper()

# Write to output directory
os.makedirs(output_dir, exist_ok=True)
with open(f'{output_dir}/output.txt', 'w') as f:
    f.write(processed)

print(f"Processed {len(text)} characters")
EOF

chmod +x skills/my-first-skill/scripts/process.py
```

### 4. Configure Genie

Add to your `genie.toml`:

```toml
skills_path = "./skills"
```

Or set environment variable:

```bash
export SKILLS_ROOT=./skills
```

## Remote Skills Support

Genie supports loading skills from remote HTTPS URLs in addition to local directories. This enables:
- Sharing skills across teams via HTTP(S)
- Centralized skill repositories
- Version-controlled skill distribution
- Mix of local and remote skills

### Configuration

Use `skills_roots` array to specify multiple skill locations:

```toml
# config.toml
skills_roots = [
  "./local-skills",                                    # Local directory
  "https://example.com/skills/developer",             # Remote HTTPS URL
  "https://raw.githubusercontent.com/org/repo/main/skills",  # GitHub raw content
]
```

### Caching

Remote skills are automatically cached by the underlying `trpc-agent-go` library:
- Cache location: `$SKILLS_CACHE_DIR` or default cache directory
- Skills are downloaded once and reused
- Updates require cache invalidation or restart

### Best Practices

1. **Use HTTPS**: Always use HTTPS URLs for security
2. **Version Control**: Pin to specific commits or tags for reproducibility
   ```toml
   skills_roots = [
     "https://raw.githubusercontent.com/org/repo/v1.2.3/skills"
   ]
   ```
3. **Local Development**: Keep local skills for development, remote for production
4. **Fallback**: List local paths first for faster loading
5. **Access Control**: Ensure remote URLs are accessible from your environment

### Example: GitHub Skills Repository

```toml
skills_roots = [
  "./examples/skills",  # Local examples
  "https://raw.githubusercontent.com/your-org/genie-skills/main/developer",
  "https://raw.githubusercontent.com/your-org/genie-skills/main/devops",
]
```

### 5. Use Skills in Code

```go
package main

import (
    "context"
    "github.com/appcd-dev/genie/pkg/config"
    "github.com/appcd-dev/genie/pkg/expert"
    "github.com/appcd-dev/genie/pkg/expert/modelprovider"
)

func main() {
    ctx := context.Background()
    
    // Load configuration
    cfg, _ := config.LoadGenieConfig("genie.toml")
    
    // Load skill tools
    skillTools := expert.LoadSkillTools(ctx, cfg)
    
    // Create expert with skills
    bio := expert.ExpertBio{
        Name:        "MyExpert",
        Description: "An expert with skills",
        Tools:       skillTools, // Add skill tools
    }
    
    modelProvider, _ := modelprovider.NewModelProvider(ctx, cfg.ModelConfig)
    myExpert, _ := bio.ToExpert(ctx, modelProvider)
    
    // Use the expert
    response, _ := myExpert.Do(ctx, expert.Request{
        Message: "List available skills",
    })
}
```

## Skill Structure

### Required Files

- **`SKILL.md`** - Skill definition with YAML frontmatter

### Optional Directories

- **`scripts/`** - Executable scripts (Python, Shell, JavaScript, Ruby)
- **`references/`** - Additional documentation
- **`assets/`** - Images, data files, etc.

### YAML Frontmatter

```yaml
---
name: skill-name          # Required: lowercase, hyphens only
description: Brief description  # Required: what the skill does
---
```

### Skill Naming Rules

- Lowercase letters (a-z), numbers (0-9), and hyphens (-) only
- Cannot start or end with hyphen
- No consecutive hyphens
- Maximum 64 characters

**Valid**: `text-processor`, `image-resizer`, `data-analyzer`  
**Invalid**: `Text-Processor`, `image_resizer`, `-data-analyzer`, `skill--name`

## Available Tools

When skills are configured, three tools are automatically available to agents:

### 1. `list_skills`

Lists all available skills.

**Request**: `{}`

**Response**:
```json
{
  "skills": [
    {
      "name": "my-first-skill",
      "description": "A simple example skill"
    }
  ],
  "count": 1
}
```

### 2. `skill_load`

Loads full skill instructions and documentation.

**Request**:
```json
{
  "skill_name": "my-first-skill"
}
```

**Response**:
```json
{
  "name": "my-first-skill",
  "description": "A simple example skill",
  "instructions": "# My First Skill\n\n...",
  "auxiliary_docs": [
    {
      "path": "references/guide.md",
      "content": "..."
    }
  ]
}
```

### 3. `skill_run`

Executes a skill script.

**Request**:
```json
{
  "skill_name": "my-first-skill",
  "script_path": "scripts/process.py",
  "args": ["--verbose"],
  "input_files": {
    "input.txt": "Hello, World!"
  },
  "timeout_seconds": 30
}
```

**Response**:
```json
{
  "output": "Processed 13 characters\n",
  "exit_code": 0,
  "output_files": {
    "output.txt": "HELLO, WORLD!"
  }
}
```

## Script Execution Environment

### Environment Variables

Scripts receive these environment variables:

- `SKILL_PATH` - Absolute path to skill directory
- `WORKSPACE` - Temporary workspace directory
- `OUTPUT_DIR` - Directory for output files

### Workspace Structure

```
workspace/
├── input/          # Input files from skill_run request
└── output/         # Output files collected after execution
```

### Supported Interpreters

- **Python**: `.py` files → `python3`
- **Shell**: `.sh` files → `bash`
- **JavaScript**: `.js` files → `node`
- **Ruby**: `.rb` files → `ruby`

## Troubleshooting

### Skills Not Loading

**Problem**: Skills tools not available to agents.

**Solutions**:
1. Check `skills_path` in config file
2. Verify `SKILLS_ROOT` environment variable
3. Check logs for skill loading errors
4. Ensure skills directory exists and is readable

### Skill Not Found

**Problem**: `skill_load` or `skill_run` returns "skill not found".

**Solutions**:
1. Run `list_skills` to see available skills
2. Check skill name matches directory name
3. Verify `SKILL.md` exists and has valid frontmatter
4. Check skill name follows naming rules

### Script Execution Fails

**Problem**: `skill_run` returns non-zero exit code.

**Solutions**:
1. Check script has execute permissions
2. Verify interpreter is installed (python3, bash, node, ruby)
3. Check script reads from `$WORKSPACE/input/`
4. Check script writes to `$OUTPUT_DIR/`
5. Review script output in response for error messages

### Invalid SKILL.md

**Problem**: Skill not discovered or loaded.

**Solutions**:
1. Verify YAML frontmatter is between `---` markers
2. Check `name` and `description` fields are present
3. Validate skill name follows naming rules
4. Ensure no tabs in YAML (use spaces)

## Best Practices

### 1. Keep Skills Focused

Each skill should do one thing well. Create multiple skills rather than one complex skill.

### 2. Document Thoroughly

Include clear usage instructions, input/output specifications, and examples in `SKILL.md`.

### 3. Handle Errors Gracefully

Scripts should:
- Validate inputs
- Provide helpful error messages
- Exit with appropriate codes (0 = success, non-zero = error)

### 4. Use Relative Paths

Scripts should use environment variables (`$WORKSPACE`, `$OUTPUT_DIR`) rather than hardcoded paths.

### 5. Test Locally

Test scripts manually before deploying:

```bash
cd skills/my-skill
export WORKSPACE=/tmp/test-workspace
export OUTPUT_DIR=/tmp/test-workspace/output
mkdir -p $WORKSPACE/input $OUTPUT_DIR
echo "test" > $WORKSPACE/input/input.txt
python3 scripts/process.py
cat $OUTPUT_DIR/output.txt
```

### 6. Version Control

Keep skills in version control and document changes.

## Examples

See `examples/skills/` for complete skill examples.

## Security Considerations

- Skills execute with the same permissions as Genie
- Review skill scripts before using untrusted skills
- Consider running Genie in a container for isolation
- Set appropriate timeouts to prevent runaway scripts

## Advanced Topics

### Custom Executors

For advanced use cases, implement a custom `Executor` interface to add sandboxing, resource limits, or remote execution.

### Skill Caching

Future versions may support caching skill execution results based on inputs.

### Skill Repositories

Future versions may support remote skill repositories and skill versioning.
