# Agent Failure Learning — Acceptance Criteria

> Tests for verifying that agents learn from their failures and successes,
> storing experiences in episodic memory with verbal reflections and
> recency-weighted retrieval.

---

## 1 — Failure Episodes Are Stored (Not Discarded)

### Why
Previously, when an agent failed a task, the output was discarded and never stored in memory. The agent would repeat the same mistakes on identical requests. Now, failures are stored with a verbal reflection explaining what went wrong.

### Problem
Without failure storage, the agent has no memory of past mistakes. If a user asks the same question twice and it fails the first time, the agent has zero context about why it failed.

### Benefit
The agent builds an experience corpus that includes both successes and failures. On future similar requests, the agent sees what went wrong before and avoids repeating the same approach.

### Arrange
- Connected to the server
- Tail the server logs: `tail -f genie.log | grep -i "failure\|reflection\|episodic"`
- Have a task that you know will fail (e.g., ask the agent to access an API endpoint that doesn't exist, or a tool that's not configured)

### Act
1. Send a message that will cause a failure:
   `Use the JIRA tool to list all open tickets` (assuming JIRA is not configured)
2. Wait for the agent to fail
3. Check the logs for: `"stored failure episode with reflection"`

### Assert
- In the server logs, you see `"stored failure episode with reflection"` with `"has_reflection": true`
- The log does NOT show `"skipping episodic memory storage for error-like output"` (old behavior)
- The reflection summary in the logs is a concise, actionable description (not the raw error text)

---

## 2 — Failure Reflections Appear in Future Prompts

### Why
Stored failures are only useful if they surface in future prompts. When the agent encounters a similar goal, past failure reflections should be injected into the prompt to prevent repeating the same mistake.

### Problem
Without prompt injection of past failures, the memory is write-only — stored but never used.

### Benefit
The agent sees "⚠️ Previous Failure" warnings in its context and can adjust its approach accordingly.

### Arrange
- Connected to the server
- Tail the server logs
- The agent has previously stored at least one failure episode (from Test 1)

### Act
1. Send a message with the same or very similar goal that previously failed:
   `Use the JIRA tool to check open tickets`
2. Watch the agent's behavior — it should either try a different approach or acknowledge the limitation

### Assert
- In the server logs, you see episodic memory retrieval happening (the prompt includes "Relevant Past Experiences")
- The prompt includes the failure reflection (visible in debug logs if enabled)
- The agent's response acknowledges or adapts based on the past failure, rather than blindly repeating the same failed approach

---

## 3 — Successful Episodes Still Work As Before

### Why
The failure learning feature must not break existing success episode storage. Successful outputs should still be stored with `EpisodePending` status, and user reactions (👍/👎) should still work.

### Problem
A regression in success episode handling would break the core learning loop.

### Benefit
Both successes and failures are tracked, giving the agent a balanced view of past experiences.

### Arrange
- Connected to the server
- Tail the server logs

### Act
1. Send a simple task that will succeed:
   `What is 2 + 2?`
2. React to the agent's response with 👍
3. Send the same or similar question again

### Assert
- The agent responds correctly
- In the logs, the success episode is stored as `EpisodePending`
- After the 👍 reaction, the episode status is updated to `EpisodeSuccess`
- On the second request, the agent may reference its past successful experience

---

## 4 — Recency-Weighted Retrieval

### Why
The agent should prioritize recent experiences over old ones. A failure from 2 weeks ago should have much less influence than a failure from 1 hour ago.

### Problem
Without recency weighting, an old, no-longer-relevant failure might dominate the agent's context, causing it to avoid an approach that now works fine.

### Benefit
Natural "forgetting" — old lessons fade away over ~2 weeks without manual cleanup.

### Arrange
- Connected to the server with a persistent database (not in-memory)
- The agent has accumulated several episodes over time (or seed the database)

### Act
1. Send a task that has both old and recent relevant episodes
2. Observe the order in which past experiences appear in the agent's prompt

### Assert
- Recent episodes (within the last day) appear first in "Relevant Past Experiences"
- Old episodes (> 7 days) are ranked lower or may not appear at all
- The agent's behavior is influenced by recent lessons, not outdated ones

### Note
This test is best verified by inspecting debug logs or adding temporary logging to `RetrieveWeighted`. A unit test already validates the scoring math.

---

## 5 — Loop-Level Failure Storage

### Why
Some failures happen at the adaptive loop level, not at individual agent node level. For example, when the loop detects repetition (stuck agent) or when an iteration returns a hard failure. These loop-level failures must also be stored.

### Problem
Without loop-level failure storage, the agent misses patterns like "I always get stuck when trying to do X in a loop."

### Benefit
The agent learns from both per-step failures and overall loop-level failures, building a more complete picture.

### Arrange
- Connected to the server
- Tail the server logs
- Configure a task that will cause the adaptive loop to hit its repetition detection (e.g., a task that requires a tool the agent doesn't have access to, causing repeated attempts)

### Act
1. Send a complex task that the agent can't solve (e.g., requires a tool not in its toolkit):
   `Search the internal wiki for the deployment runbook and execute step 3`
2. Wait for the loop to terminate with repetition or failure

### Assert
- In the server logs, you see repetition detection or loop failure
- A failure episode is stored with a reflection about the loop-level issue
- The reflection mentions something like "stuck in repetition" or the specific iteration error

---

## 6 — Failure Reflector Graceful Degradation

### Why
The failure reflector uses a cheap LLM call to generate reflections. If the LLM is unavailable (e.g., rate limited, model down), the system must not crash or hang.

### Problem
An LLM call for reflections that blocks or fails could degrade the main agent's response time.

### Benefit
Best-effort learning — the system records what it can, but never sacrifices the main user experience for secondary learning.

### Arrange
- Connected to the server
- (Optionally) simulate LLM unavailability by disconnecting the reflection model or setting invalid API keys, if `FailureReflector` is configured to use a separate model endpoint

### Act
1. Trigger a failure-causing task
2. If the LLM for reflections is down, verify the behavior

### Assert
- The agent still responds to the user (main flow is not blocked)
- If reflection fails, a basic fallback is stored: `"Task failed with output: <truncated error>"`
- The logs show: `"failure reflection LLM call failed, skipping"` (when the reflector LLM is down)
- The system does NOT crash, hang, or retry indefinitely

---

## Unit Test Coverage (Automated)

The following scenarios are covered by automated Ginkgo/Gomega tests:

| Test File | Suite | Tests |
|-----------|-------|-------|
| `pkg/reactree/memory/failure_learning_test.go` | Memory Suite | 12 tests |
| `pkg/reactree/memory/consolidation_test.go` | Memory Suite | 14 tests |
| `pkg/reactree/failure_reflector_test.go` | Reactree Suite | 6 tests |
| `pkg/reactree/importance_and_consolidation_test.go` | Reactree Suite | 15 tests |
| `pkg/reactree/agent_node_pvt_test.go` | Reactree Suite | 3 updated tests |

### Automated Test Scenarios

**Phase 1-2: Failure Storage & Weighted Retrieval**
1. **Episode.String()** formats failures with ⚠️ and reflection (not raw error)
2. **Episode.String()** formats successes with goal/trajectory (unchanged)
3. **Episode.String()** falls back to trajectory format for failures without reflection
4. **CreatedAt** is auto-populated when storing episodes with zero time
5. **CreatedAt** is preserved when explicitly set
6. **Reflection + Importance** roundtrip through JSON serialization
7. **RetrieveWeighted** ranks recent episodes higher than old ones
8. **RetrieveWeighted** limits results to k
9. **RetrieveWeighted** boosts high-importance episodes
10. **RetrieveWeighted** returns nil for no matches
11. **RetrieveWeighted** works with NoOpEpisodicMemory
12. **NoOpFailureReflector** returns empty string
13. **ExpertFailureReflector** generates reflections via expert
14. **ExpertFailureReflector** handles expert errors gracefully
15. **ExpertFailureReflector** truncates long reflections
16. **ExpertFailureReflector** truncates long error output in prompt
17. **ExpertFailureReflector** handles empty choices
18. **ExpertFailureReflector** uses TaskEfficiency model
19. **buildAgentPrompt** includes episodic memory (uses weighted retrieval)
20. **buildAgentPrompt** shows failure reflections with ⚠️ prefix
21. **buildAgentPrompt** shows both successes and failures together

**Phase 3: Daily Wisdom Consolidation**
22. **WisdomStore** stores and retrieves wisdom notes via memory.Service
23. **WisdomStore** limits results
24. **WisdomStore** auto-populates CreatedAt
25. **NoOpWisdomStore** returns nil and doesn't panic
26. **FormatWisdomForPrompt** returns empty for no notes
27. **FormatWisdomForPrompt** includes all wisdom notes with header
28. **EpisodeConsolidator** returns nil when dependencies are missing
29. **EpisodeConsolidator** consolidates recent episodes into wisdom note
30. **EpisodeConsolidator** skips when wisdom already exists for the period (idempotent)
31. **EpisodeConsolidator** skips when no episodes exist
32. **EpisodeConsolidator** skips old episodes outside lookback window
33. **EpisodeConsolidator** handles summarizer returning empty
34. **ExpertEpisodeSummarizer** summarizes episodes into concise wisdom
35. **ExpertEpisodeSummarizer** returns empty for no episodes
36. **ExpertEpisodeSummarizer** returns empty on LLM error
37. **ExpertEpisodeSummarizer** truncates long summaries
38. **ExpertEpisodeSummarizer** prefers reflections over raw trajectory for failures

**Phase 4: Importance Scoring**
39. **NoOpImportanceScorer** returns 0
40. **ExpertImportanceScorer** parses clean integer response
41. **ExpertImportanceScorer** extracts integer from noisy LLM response
42. **ExpertImportanceScorer** clamps scores above 10
43. **ExpertImportanceScorer** returns 0 on error
44. **ExpertImportanceScorer** returns 0 for empty choices
45. **ExpertImportanceScorer** returns 0 for unparseable response
46. **ExpertImportanceScorer** uses TaskEfficiency model

### Running Tests
```bash
# Memory-only tests (64 specs)
go test -mod=mod ./pkg/reactree/memory/... -v -count=1

# Full reactree tests (337 specs)
go test -mod=mod ./pkg/reactree/... -count=1 --ginkgo.label-filter='!integration'

# All tests (81 suites)
make test
```

