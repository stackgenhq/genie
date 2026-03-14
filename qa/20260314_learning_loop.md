# 🧪 Test Suite: Learning Loop & Skill Discovery
**File**: `20260314_learning_loop.md`

## 🎯 Purpose
To verify that Genie automatically distills completed tasks into reusable skills and that the orchestrator discovers learned skills via `memory_search` before solving new problems.

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

# Check memory_search for learned skills
grep 'learned_skill' ~/.genie/devops_copilot.*.ndjson
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

### Scenario 1: Skill Distillation (Background Learning)

**Goal**: Verify that complex tasks trigger background skill creation.

1. **Send a complex multi-step prompt** (example FinOps question):
   ```
   What are the top cost drivers in our AWS account 339712749745_AdministratorAccess?
   Show me a FinOps breakdown.
   ```
2. **Wait for the task to complete** and verify the response is returned promptly.
3. **Check the audit log** for learning activity:
   ```bash
   grep '"learner"' ~/.genie/devops_copilot.*.ndjson | tail -5
   ```
4. **Expect** (one of):
   - `learning_started` → `skill_created`: Skill file appears in `~/.genie/<agent>/dynamic_skills/`
   - `learning_started` → `learning_skipped` with `reason: below_novelty_threshold`: Correct for routine tasks
   - `learning_skipped` with `reason: skill_repo_nil`: MutableRepository not initialized (bug)
   - `learning_failed` with `reason: llm_call_error`: LLM issue

### Scenario 2: Skill Indexed in Vector Store

**Goal**: Verify the learned skill is discoverable via `memory_search`.

1. **After Scenario 1 creates a skill**, send a new prompt:
   ```
   Search my memory for any available skills.
   ```
2. **Expect**: The orchestrator uses `memory_search` and returns results containing `type: learned_skill` with the skill name and description.
3. **Verify** the vector entry is lightweight (name + description + `load_skill()` hint), NOT the full instructions.

### Scenario 3: Skill Discovery Before Task Execution

**Goal**: Verify the orchestrator surfaces learned skills to the user.

1. **Start a new session** (fresh conversation thread).
2. **Send a prompt similar to the task that created the skill** in Scenario 1.
3. **Expect**: The orchestrator:
   - Runs `memory_search` during Phase 1 (ANALYZE).
   - Finds the learned skill match.
   - **Mentions it to the user** and asks if they'd like to use it.
   - Does NOT silently load the skill without user awareness.

### Scenario 4: User Confirms Skill Usage

1. **From Scenario 3**, reply: "Yes, use that skill."
2. **Expect**: The orchestrator loads the skill via `load_skill` and follows its instructions to solve the problem.

### Scenario 5: Routine Tasks Are NOT Distilled

**Goal**: Verify that simple tasks don't create skills.

1. **Send a simple prompt**: "What time is it?" or "Hello!"
2. **Expect**: No skill is created. Audit log shows:
   - `learning_skipped` with `reason: empty_input` or `reason: below_novelty_threshold`

### Scenario 6: Sub-Agent Sessions Persisted

**Goal**: Verify that sub-agent conversations are stored in the database.

1. **After any complex task** that spawns sub-agents, query the DB:
   ```bash
   sqlite3 .genie.db "SELECT app_name, session_id, created_at FROM sessions ORDER BY updated_at DESC LIMIT 10;"
   ```
2. **Expect**: Sub-agent names (e.g., `aws-finops-analyzer`) appear as `app_name` entries with their own `session_id`.

---

## ✅ Acceptance Criteria

- [ ] Complex multi-step tasks trigger background skill distillation.
- [ ] User-facing response is NOT delayed by the learning process.
- [ ] Skills are created in `agentskills.io` format (What it can do / How / What worked / What didn't).
- [ ] Learned skills appear in `memory_search` results with `type: learned_skill`.
- [ ] Vector entries are lightweight (name + description), not full instructions.
- [ ] Orchestrator surfaces relevant skills to user and asks before loading.
- [ ] Routine/trivial tasks do NOT create skills.
- [ ] Duplicate skill creation is handled gracefully.
- [ ] Sub-agent sessions are persisted to the database.
- [ ] All learner decisions are visible in the audit log (`~/.genie/*.ndjson`).
