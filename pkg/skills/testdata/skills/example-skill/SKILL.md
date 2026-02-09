---
name: example-skill
description: A simple example skill for testing text processing
---

# Example Skill

This is a simple example skill that demonstrates the agentskills.io format.

## Capabilities

- Process text files
- Convert text to uppercase
- Count words in text

## Usage

### Basic Text Processing

```bash
python3 scripts/process.py <input_file> <output_file>
```

### With Options

```bash
# Convert to uppercase
python3 scripts/process.py input.txt output.txt --uppercase

# Count words
python3 scripts/process.py input.txt output.txt --count-words
```

## Parameters

- `input_file` (required): Path to input text file
- `output_file` (required): Path to output file
- `--uppercase`: Convert text to uppercase
- `--count-words`: Count words in the text

## Dependencies

- Python 3.8+
