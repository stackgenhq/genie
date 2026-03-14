---
name: memorize-workflow
description: Analyzes recent successful actions and creates a new reusable skill following the agentskills.io standard to democratize the learned knowledge.
---

# Memorize Workflow

This skill is a "meta-skill" used to democratize knowledge. You should use this skill when you have successfully completed a novel, complex, or multi-step task and want to teach future agents how to do it.

## How to Distill Knowledge

To create a new skill from your recent experience, you must use the `create_skill` tool.

The `instructions` field of the `create_skill` tool MUST be formatted as a Markdown document following the [agentskills.io](https://agentskills.io) standard structure below.

### Standard Structure for `instructions`

You must explicitly include these four sections in the generated markdown:

1. **What it can do**: Describe the capability or the problem the skill solves. Be specific about the domain and the expected outcome.
2. **How it did it**: Provide generalized, repeatable step-by-step instructions. Remove specific hardcoded values from your current task (like specific Git commit hashes or temporary file names) and replace them with general placeholders (e.g., `<branch_name>`).
3. **What worked**: Document the successful approaches, specific tool configurations, and best practices discovered during the task. List any useful CLI flags, API endpoints, or file paths that were critical to success.
4. **What did not work**: This is critical for preventing future errors. Document the dead ends, errors, and anti-patterns encountered during the original problem-solving process. Explain *why* they didn't work so future agents avoid repeating the same mistakes.

## Example Usage

When formulating the `create_skill` request:

```json
{
  "name": "analyze-aws-costs",
  "description": "Fetches and analyzes AWS cost and usage reports to identify spending anomalies.",
  "instructions": "# Analyze AWS Costs\n\n## What it can do\nThis skill enables the agent to investigate unexpected cloud bills by querying the AWS Cost Explorer API and grouping by service and tag.\n\n## How it did it\n1. Use the `run_command` tool to execute `aws ce get-cost-and-usage --time-period Start=<start>,End=<end> --granularity MONTHLY --metrics \"UnblendedCost\" --group-by Type=DIMENSION,Key=SERVICE`\n2. Parse the JSON output and identify the top 3 services by cost.\n3. For each top service, run a deeper query grouping by `TAG`.\n\n## What worked\n- Using `--granularity MONTHLY` is much faster and less prone to rate limiting than `DAILY` for initial triage.\n- Grouping by `SERVICE` first, then `TAG` is the most effective approach for finding anomalies.\n\n## What did not work\n- DO NOT use the `read_url_content` tool to scrape the AWS billing dashboard. It requires full browser authentication and fails. Always use the `aws ce` CLI.\n- Requesting `--metrics \"UsageQuantity\"` without combining it with `UnblendedCost` provides unhelpful data because different services use different units (GB vs Hours).\n"
}
```

## Summary
By using this format, you ensure that the knowledge you accumulated is structured, reusable, and prevents future agents from making the same mistakes you did.
