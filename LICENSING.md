# Genie Licensing Model

Genie uses a [hybrid open-core licensing model](https://en.wikipedia.org/wiki/Open-core_model) to balance a thriving open-source developer ecosystem with commercial viability.

Our codebase is split into two tiers:

## 1. Core Product (Apache License 2.0)
The vast majority of the Genie codebase (over 90%) is fully open-source under the **[Apache License 2.0](./LICENSE)**. You can freely use, modify, and distribute these components, including in your own commercial products.

This includes:
- All LLM expert abstractions (`pkg/expert/...`)
- All tools and tool providers (`pkg/tools/...`)
- Vector and Graph memory systems (`pkg/memory/...`)
- Human-in-the-loop (HITL) capabilities (`pkg/hitl/...`)
- Messenger integrations (Slack, Discord, MS Teams, etc.) (`pkg/messenger/...`)
- MCP protocol implementation (`pkg/mcp/...`)
- Skills system (`pkg/skills/...`)
- Basic audit logging, basic PII redaction, scheduling, observability, configuration, and infrastructure.

## 2. Execution Engine (Business Source License 1.1)
The novel algorithms that make up Genie’s execution engine are licensed under **[BSL 1.1](./LICENSE-BSL)**. This protects StackGen from cloud providers building "Managed Genie" services while keeping the source code accessible. 

The packages under BSL are:
- `pkg/reactree/` (The ReAcTree execution engine)
- `pkg/halguard/` (The hallucination guard system)
- `pkg/orchestrator/` (The multi-agent orchestrator)
- `pkg/semanticrouter/` (The semantic intent router)

**What this means for you:**
Most users will not be affected by the BSL. The Additional Use Grant explicitly allows you to use these components for any purpose (including your own internal production use or embedding it in your product) EXCEPT providing a commercially hosted agent orchestration service or platform that competes directly with Genie.

**Conversion to Open Source:**
3 years after each release, the BSL-licensed code automatically converts to the **Apache License 2.0**.
