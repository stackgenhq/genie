# 🧪 Test Suite: Learning Loop & Skill Discovery
**File**: `20260314_learning_loop.md`

## 🎯 Purpose
To verify that Genie automatically distills completed tasks into reusable skills — capturing not just what worked, but also **what didn't work** (dead ends, access constraints, environmental boundaries). The orchestrator should discover and inject learned skills into context before solving new problems, preventing the agent from repeating past mistakes.

## 🛠 Setup

1. **Prerequisites**:
   - Genie configured with a vector store (in-memory or Qdrant/Ollama)
   - `skills_roots` set in `.genie.toml` (e.g., `skills_roots = ["./skills"]`)
   - LLM API keys set (Gemini, OpenAI, or Anthropic in `.env`)

2. **Build Genie** (from repo root):
   ```bash
   go build -mod=mod -o /tmp/genie-test .
   ```

3. **Launch Genie with debug logging** (from example dir):
   ```bash
   cd examples/devops-copilot
   source ../../.env
   /tmp/genie-test --log-level=debug 2>&1 | tee /tmp/<agent_name>.log
   ```
   > Replace `<agent_name>` with the `agent_name` from `.genie.toml`
   > (e.g., `/tmp/devops-copilot.log`).

4. **Open the Chat UI**:
   Navigate to `http://localhost:9876/ui/chat.html`

---

## 📁 Where to Find Evidence

| Artifact               | Location |
|-------------------------|----------|
| Debug slog output       | `/tmp/<agent_name>.log` |
| Audit log (ndjson)      | `~/.genie/<agent_name>.<date>.ndjson` |
| Learned skills (files)  | `~/.genie/<agent_name>/dynamic_skills/` |
| SQLite database         | `<working_dir>/.genie.db` |

### Useful grep patterns for `/tmp/<agent_name>.log`:
```bash
# Check if learner was triggered
grep "learning.Learn" /tmp/devops-copilot.log

# Check skills initialization
grep -i "skill\|mutable" /tmp/devops-copilot.log

# Check programmatic skill recall by orchestrator
grep "recallLearnedSkills" /tmp/devops-copilot.log
```

### Useful grep patterns for audit log (`~/.genie/*.ndjson`):
```bash
# All learner audit events
grep '"learner"' ~/.genie/devops_copilot.*.ndjson

# Specific outcomes:
grep 'learning_started' ~/.genie/devops_copilot.*.ndjson
grep 'learning_skipped' ~/.genie/devops_copilot.*.ndjson
grep 'learning_failed' ~/.genie/devops_copilot.*.ndjson
grep 'skill_created' ~/.genie/devops_copilot.*.ndjson

# Check orchestrator skill recall
grep 'skills_recalled' ~/.genie/devops_copilot.*.ndjson
```

### Useful SQLite queries for `.genie.db`:
```bash
# Check sub-agent sessions are persisted
sqlite3 .genie.db "SELECT app_name, session_id, created_at FROM sessions ORDER BY updated_at DESC LIMIT 10;"

# Count session events
sqlite3 .genie.db "SELECT count(*) FROM session_events;"
```

---

## 🧪 Scenarios

### Scenario 1: Multi-Cluster EKS Discovery (Primary Example)

**Goal**: Verify that the agent learns environmental constraints — specifically, which clusters it has access to — so it doesn't waste time retrying inaccessible clusters.

> **Context**: The AWS account `339712749745_AdministratorAccess` has **3 EKS clusters** 
> but the agent only has kubeconfig/IAM access to **one**. On the first run, the agent
> will discover this the hard way (trying all 3, failing on 2). The learning loop should
> capture this environmental knowledge so future queries skip the inaccessible clusters.

1. **Send the following prompt** (first run — no prior skills):
   ```
   List all EKS clusters in AWS account 339712749745_AdministratorAccess
   and check the health of each one. Report pod counts, node status,
   and any failing deployments.
   ```
2. **Wait for the agent to complete.** Expected behaviour on the **first run**:
   - Agent calls `aws eks list-clusters` → discovers 3 clusters
   - Tries `aws eks update-kubeconfig` on each cluster
   - **Fails** on 2 clusters (AccessDenied / Unauthorized)
   - **Succeeds** on 1 cluster → runs `kubectl get nodes`, `kubectl get pods --all-namespaces`, checks failing deployments
   - Reports results for the accessible cluster and notes the other 2 are inaccessible
3. **Verify a skill was created**:
   ```bash
   grep 'skill_created' ~/.genie/devops_copilot.*.ndjson
   ls ~/.genie/devops_copilot/dynamic_skills/
   ```
4. **Expected skill content** (e.g., `eks-multi-cluster-health-check`):
   - **What it can do**: Check EKS cluster health across an AWS account  
   - **How it did it**: `list-clusters` → `update-kubeconfig` → `kubectl get nodes/pods` → deployment status
   - **What worked**: Successfully accessed cluster `<name>` using the default kubeconfig
   - **What did not work**: 2 of 3 clusters returned AccessDenied — only `<accessible-cluster>` is reachable with current IAM permissions. Skip the others to avoid wasted API calls.

### Scenario 2: Learned Skill Prevents Repeated Mistakes

**Goal**: Verify that the **second run** skips inaccessible clusters because the learned skill captured the constraint.

1. **Start a new session** (fresh conversation thread).
2. **Send a similar prompt**:
   ```
   Check the health of our EKS clusters in account 339712749745_AdministratorAccess.
   Are there any pods in CrashLoopBackOff?
   ```
3. **Expected behaviour with recalled skill**:
   - `recallLearnedSkills()` fires → finds `eks-multi-cluster-health-check` skill (similarity > 0.5)
   - Skill instructions injected into orchestrator prompt as `## Relevant Learned Skills`
   - Orchestrator **mentions the skill to the user** and notes it knows only one cluster is accessible
   - Sub-agent skips the 2 inaccessible clusters entirely → goes straight to the accessible one
   - Faster execution, no wasted AccessDenied errors
4. **Verify in logs**:
   ```bash
   grep "recallLearnedSkills" /tmp/devops-copilot.log
   grep "skills_recalled" ~/.genie/devops_copilot.*.ndjson
   ```

### Scenario 3: Skill Indexed in Vector Store

**Goal**: Verify the learned skill is discoverable via vector search.

1. **After Scenario 1 creates a skill**, check the audit log:
   ```bash
   grep 'learned_skill' ~/.genie/devops_copilot.*.ndjson
   ```
2. **Expect**: A `vectorstore.upsert` entry with `type: learned_skill`, the skill name, and a one-line description.
3. **Verify** the vector entry is lightweight (name + description), NOT the full instructions.

### Scenario 4: Routine Tasks Are NOT Distilled

**Goal**: Verify that simple tasks don't create skills.

1. **Send a simple prompt**: "What time is it?" or "Hello!"
2. **Expect**: No skill is created. Audit log shows:
   - `learning_skipped` with `reason: empty_input` or `reason: below_novelty_threshold`

### Scenario 5: Duplicate Skill Deduplication

**Goal**: Verify that repeating a similar complex task doesn't create a duplicate skill.

1. **After Scenario 1**, send a third EKS-related prompt:
   ```
   Show me resource utilization across our EKS clusters in 339712749745.
   ```
2. **Expect**: The learner evaluates novelty and scores it below threshold:
   - `learning_started` → novelty ≤ 6 → `learning_skipped` with `reason: below_novelty_threshold`

### Scenario 6: Sub-Agent Sessions Persisted

**Goal**: Verify that sub-agent conversations are stored in the database.

1. **After any complex task** that spawns sub-agents, query the DB:
   ```bash
   sqlite3 .genie.db "SELECT app_name, session_id, created_at FROM sessions ORDER BY updated_at DESC LIMIT 10;"
   ```
2. **Expect**: Sub-agent names (e.g., `eks-health-checker`) appear as `app_name` entries with their own `session_id`.

---

## ✅ Acceptance Criteria

- [ ] Complex multi-step tasks trigger background skill distillation.
- [ ] User-facing response is NOT delayed by the learning process.
- [ ] Skills capture **what didn't work** (access constraints, dead ends) not just what worked.
- [ ] Skills are created in `agentskills.io` format (What it can do / How / What worked / What didn't).
- [ ] Learned skills appear in vector store with `type: learned_skill`.
- [ ] Vector entries are lightweight (name + description), not full instructions.
- [ ] Orchestrator programmatically injects relevant skills into context via `recallLearnedSkills()`.
- [ ] Orchestrator mentions recalled skills to user before acting on them.
- [ ] Second execution avoids repeating the same mistakes (e.g., skips inaccessible clusters).
- [ ] Routine/trivial tasks do NOT create skills.
- [ ] Duplicate skill creation is handled gracefully (deduplication via novelty scoring).
- [ ] Sub-agent sessions are persisted to the database.
- [ ] All learner decisions are visible in the audit log (`~/.genie/*.ndjson`).
