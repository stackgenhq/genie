# Adaptive Loop Execution — Acceptance Criteria

> Tests for verifying the dynamic, iterative behavior of the ReAcTree adaptive loop. The system should scale its effort based on task complexity.

---

## 1 — One-Shot Task (Low Complexity)

### Why
Simple requests should be handled efficiently without unnecessary reasoning loops or redundant tool calls. This verifies the "adaptive" nature of the loop.

### Problem
In the previous static pipeline, even a simple "hello" would trigger 4+ LLM calls (Understanding → Planning → Executing → Reviewing). The adaptive loop should skip all that overhead.

### Benefit
Reduced latency and cost for trivial user requests. The agent feels snappier.

### Arrange
- Connected to the server
- Tail the server logs (`tail -f genie.log`) to watch for "Iteration" markers

### Act
Type and send: `Create a file named "quick_test.txt" with the content "done"`

### Assert
- The agent performs the action immediately
- In the server logs, you see **only "Iteration 1"** (or at most Iteration 2 if it verifies separately)
- The agent does **NOT** enter a long "Planning" or "Reviewing" loop
- The file `quick_test.txt` is created

---

## 2 — Multi-Step Reasoning (High Complexity)

### Why
Complex tasks require iterative thinking, planning, and execution. The loop must correctly accumulate context and proceed through necessary steps without giving up early.

### Problem
If the loop terminates too early (e.g., acts like a one-shot agent), complex tasks will be incomplete or hallucinated.

### Benefit
The agent is capable of handling ambiguous or multi-part requests that require "thinking" over several turns.

### Arrange
- Connected to the server
- Tail the server logs

### Act
Type and send: `Research the current directory structure, create a plan to reorganize the 'cmd' folder into a cleaner layout, and write that plan to 'refactor_plan_<YYYYMMDD>.md'. Do not actually move any files yet.`

### Assert
- The agent visibly progresses through multiple steps (e.g., `ls -R`, then `read_file`, then `write_file`)
- In the server logs, you see **multiple iterations** (e.g., Iteration 1, 2, 3...)
- The `refactor_plan.md` file is created and contains a logical plan based on the actual files
- The loop does **NOT** hit the `MaxIterations` limit (10) unless the task is extremely large
- The loop terminates naturally when the plan is written
