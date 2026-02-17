# Genie Enterprise Features Roadmap

This document outlines the strategic roadmap for transforming Genie into an enterprise-ready, governed digital workforce. These features prioritize security, governance, observability, and seamless integration into complex corporate environments.

## 🛡️ Security & Trust

### 1. Agent Identity & Lifecycle Management (IAM for Agents)
Treat agents as employees with **Non-Human Identities (NHI)**.
-   **Deep Integration**: Provision agents via Okta/Azure AD just like humans.
-   **Lifecycle Management**: Automatic offboarding/reassignment when the human owner leaves.
-   **Audit Trail**: Every action is tied to a specific agent identity, not just a shared API key.

### 2. Sandbox-First "Shadow Mode" & "Glass Box" Debugger
Build trust by visualizing intent before execution.
-   **Simulation Sandbox**: Run agents in a safe zone (dry-run) by default. Users see exactly what *would* happen before approving live execution.
-   **Glass Box Replay**: Visual, step-by-step debugger to replay an agent's reasoning path, inspect context at each step, and "rewind/fork" execution to fix mistakes.

### 3. Real-Time Data Firewall & Secret Zero Architecture
Ensure sensitive data and credentials never leak.
-   **PII/PHI Firewall**: Middleware that automatically detects and redacts sensitive data (credit cards, SSNs) before it leaves the perimeter to an LLM provider.
-   **Secret Zero**: Agents never hold raw API keys. They use short-lived tokens or reference IDs that only the execution layer can resolve.

### 4. Vetted & Signed Skill Marketplace
Prevent unauthorized tool usage.
-   **Security-Signed Plugins**: Every skill/plugin must be checksum-verified and signed.
-   **IT-Approval Workflow**: Agents are blocked instantly if they try to usage a skill that hasn't been explicitly approved by IT/SecOps.

## ⚖️ Governance & Control

### 5. Dynamic Policy-as-Code (OPA/ABAC)
Move beyond static RBAC to **Attribute-Based Access Control**.
-   **Policy Studio**: Write custom Rego policies (e.g., "Agents can only touch AWS resources tagged 'Dev' between 9 AM - 5 PM").
-   **Budget Guardrails**: "No agent can spend more than $50/hour in API tokens without human approval."

### 6. Granular "Just-in-Time" Access
Minimize the attack surface.
-   **Temporary Elevation**: Agents operate with minimal privileges but can request *temporary* elevation for sensitive actions (like `terraform apply`), granted by a human for a specific time window.

### 7. Sovereign & Hybrid Execution ("Hybrid Logic")
Balance cloud intelligence with data residency.
-   **Split Execution**: The agent's "brain" (LLM) can be in the cloud, but the "hands" (code execution) run on-premises or in a private VPC.
-   **Air-Gap Support**: One-click switch to run entirely offline using local models (Ollama, vLLM) for high-security environments.

## 👁️ Observability & Reliability

### 8. The "AI Control Tower" & FinOps
Centralized command and control for the digital workforce.
-   **Global Dashboard**: Monitor Agent ROI (time saved vs. cost), token usage, and performance.
-   **Drift Detection**: Alerts when an agent's behavior pattern changes significantly.
-   **Global Kill Switch**: Instantly pause all agents if a security threat is detected.
-   **Smart Model Routing**: Auto-route "easy" tasks to cheaper models (Haiku/Flash) and "hard" tasks to capability leaders (Opus/GPT-4) to optimize spend.

### 9. Continuous Red-Teaming & Reasoning Evals
Proactive security and quality assurance.
-   **Attacker Agent**: A background process that continuously tries to trick internal agents into leaking data or violating policies.
-   **Reasoning Traceability**: Evals that check the *process*, not just the output. Flag when an agent skips a mandatory step (like a policy check), ensuring compliance.
-   **CI/CD for Agents**: Run a suite of "golden scenarios" on every prompt change.

## 🧠 Intelligence & Collaboration

### 10. Persistent Enterprise Memory (Knowledge Graph)
Contextual awareness that survives individual sessions.
-   **Corporate Knowledge Graph**: Understand relationships between entities (e.g., "Service A depends on Service B").
-   **Long-Term Memory**: Agents remember past decisions, vendor stances, and architectural preferences across different chat sessions.

### 11. Inter-Agent Communication Fabric (A2A)
Enable specialized agents to work together.
-   **A2A Protocol**: Native support for **Model Context Protocol (MCP)** allowing agents to hand off tasks securely (e.g., "HR Agent" asks "Finance Agent" to approve a request) without human copy-pasting.

### 12. Collaborative "Multi-Player" Workspace
Humans and AI working side-by-side.
-   **Shadow Mode Proposals**: Agents propose answers/actions in real-time in a shared UI.
-   **Human-in-the-Loop Learning**: Agents learn from human corrections and approvals, constantly improving their accuracy.

As of 2026, the AI Agent ecosystem has matured from simple chatbots into an **"Agentic Enterprise"** stack. Below are the market leaders for each of your requirements, categorized by their primary role in the lifecycle of an autonomous digital worker.

---

## 🛡️ Identity, Security & Lifecycle

*Ensuring agents are known, tracked, and denied access to secrets.*

### 1. Agent Identity & Lifecycle (IAM for Agents) 🔴 **High Complexity**
*Requires deep integration with external IdPs (Okta/Azure AD) and changes to `pkg/expert` context propagation.*

* **Microsoft Entra (Agent ID):** The clear leader for Azure-heavy shops. It assigns unique identities to agents, enabling **Conditional Access** and automatic lifecycle syncing with HR systems.
* **Okta (AI Identity):** The top choice for platform-agnostic enterprises. Okta’s **Non-Human Identity (NHI)** module allows you to provision agents as "employees" with clear human-ownership mappings.
* **Oasis Security:** The specialist leader in NHI. It provides a dedicated "Control Plane" to manage the explosion of non-human identities across multi-cloud environments.

### 2. Sandbox-First "Shadow Mode" & Glass Box 🟡 **Medium Complexity**
*We have `pkg/tools` which can be updated to support a `DryRun` flag, and `pkg/audit` for logging, but visualizing it requires UI work.*

* **Maxim AI:** Known for its "Agent Simulation" environment. It allows users to visualize an agent’s proposed "chain of thought" and tool calls in a safe sandbox before committing.
* **LangSmith (LangChain):** The developer favorite for "Glass Box" debugging. It offers high-fidelity visual traces that allow you to "fork" a failed execution and test a fix in real-time.

### 3. Real-Time Data Firewall & Secret Zero 🟢 **Low Complexity**
*Basic redaction can be implemented in `pkg/logger` or `pkg/audit` using regex or specialized libraries relative easily.*

* **Aembit:** The leader in **Workload IAM**. It enforces "Secret Zero" by using a proxy that injects short-lived tokens into agent requests, so the agent never sees the raw API key.
* **Lakera Guard:** A top-tier "AI Firewall" that sits between the agent and the LLM to redact PII (SSNs, names) and block prompt injections in real-time.
* **Prompt Security:** Specifically focused on the **Model Context Protocol (MCP)**, providing a security layer for agents using third-party tools.

---

## ⚖️ Governance & Control

*Managing what agents can do and how much they can spend.*

### 4. Vetted & Signed Skill Marketplace 🔴 **High Complexity**
*Requires building a registry, cryptographic signing infrastructure, and verification logic in `pkg/skills`.*

* **HiddenLayer:** Leads the market in "AI Supply Chain Security." It scans and signs AI models and plugins to ensure they haven't been tampered with or backdoored.
* **CalypsAI Moderator:** Provides an execution-level safeguard that blocks agents from using "unvetted" tools or APIs based on IT-defined risk scores.

### 5. Dynamic Policy-as-Code (OPA/ABAC) 🟡 **Medium Complexity**
*Integration point exists in `pkg/toolwrap`, but requires embedding an OPA engine and managing policy files.*

* **Styra (Open Policy Agent - OPA):** The gold standard for Policy-as-Code. Its **Rego** language is now being used to define granular agent permissions (e.g., "Agent cannot spend >$10 on GPT-4o after 6 PM").
* **Topaz:** An emerging leader that provides a cloud-native data plane for OPA, perfect for real-time authorization in high-velocity agentic workflows.

### 6. Granular "Just-in-Time" (JIT) Access 🔴 **High Complexity**
*Requires distinct permission handling, potentially creating temporary credentials on the fly.*

* **CyberArk (Identity Security Platform):** The traditional PAM leader has pivoted to AI, offering "Just-in-Time" elevation for agents that need temporary admin rights to perform sensitive cloud tasks.
* **Apono:** Specialized in "Cloud Privilege Access," automating the JIT approval workflow so humans can approve an agent’s request for 30-minute access to a DB via Slack.

---

## 👁️ Observability & Reliability

*Monitoring the "Health" and "ROI" of your digital workforce.*

### 7. AI Control Tower & FinOps 🟢 **Low Complexity**
*We already have `pkg/audit` logging usage. `pkg/expert/expert.go` already has `modelProvider` logic that can be enhanced for smart routing.*

* **Helicone:** The leader in "Smart Model Routing" and FinOps. It provides a global dashboard to track costs and automatically switch between models (e.g., Haiku vs. Opus) based on task complexity.
* **Fiddler AI:** The enterprise-grade choice for regulated industries. It provides deep observability into "model drift" and behavioral changes in agents.

### 8. Continuous Red-Teaming & Evals 🟡 **Medium Complexity**
*Building a "golden set" of prompts is easy, but automating the "Attacker Agent" loop requires a new workflow.*

* **Giskard:** The top platform for "Adversarial Evals." It runs a background "Attacker Agent" that continuously tries to break your internal agents.
* **Galileo:** Known for its **Luna** evaluators, which provide real-time "Reasoning Traceability" to flag when an agent skips a mandatory policy check.

---

## 🧠 Intelligence & Collaboration

*The "Brain" and "Fabric" connecting humans and machines.*

### 9. Persistent Enterprise Memory 🔴 **High Complexity**
*Requires integrating a Vector DB (Milvus/Weaviate) and complex RAG logic for "knowledge graph" relationships.*

* **Mem0:** The market leader in **Graph-Based Memory**. It allows agents to maintain a "Corporate Knowledge Graph" that persists across sessions and users.
* **Zilliz (Milvus):** The high-scale vector database leader providing the infrastructure for "Long-Term Memory" in massive multi-agent deployments.

### 10. Inter-Agent Fabric & Collaborative Workspace 🟡 **Medium Complexity**
*`pkg/mcp` already exists. Extending it for peer-to-peer agent talk is feasible but non-trivial.*

* **Anthropic (MCP Protocol):** While a protocol, Anthropic is the leader in pushing the **Model Context Protocol** which is becoming the standard for Agent-to-Agent (A2A) handoffs.
* **Vellum:** A leader in building "Multi-Player" AI workspaces where humans and agents can collaborate on workflows, with built-in "Human-in-the-Loop" checkpoints.

---

### Would you like me to...

1. **Draft a Reference Architecture** combining a few of these (e.g., Okta + Aembit + LangSmith)?
2. **Compare the "Self-Hosted" vs "SaaS" options** for a high-security (air-gapped) environment?
