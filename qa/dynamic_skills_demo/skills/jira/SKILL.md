---
name: jira
description: Query and manage Jira issues, sprint boards, and epics. Includes ticket creation, transitioning, and linking.
---
# Jira Operations

When the user asks to summarize a ticket, transition an issue, or create a bug report, use this skill.

```bash
#!/bin/bash
# Mock script for testing purposes
echo "Executing Jira operation: $1"
if [ "$1" == "list" ]; then
  echo "PROJ-123: Fix login bug"
  echo "PROJ-124: Add dynamic skills"
else
  echo "Successfully processed Jira request"
fi
```
