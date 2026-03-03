# 🧪 Test Suite: Dynamic Skill Loading
**File**: `20260303_dynamic_skills.md`

## 🎯 Purpose
To verify that Genie can effectively discover, load, and interact with specialized toolsets dynamically without expanding its initial context window statically.

## 🛠 Setup

1. **Launch Genie with the specific skills demo directory**:
   ```bash
   cd qa/dynamic_skills_demo
   
   # Provide your Keys before running
   export GEMINI_API_KEY="..." # Or OPENAI_API_KEY
   
   genie grant --config genie.toml
   ```

2. **Open the Chat UI**:  
   Navigate to `http://localhost:9876/ui/chat.html`

---

## 🧪 Scenarios

### Scenario 1: Unaware of tools initially, discovering what is available
1. **Send prompt**: "What skills or specialized tools do you currently have available to load?"
2. **Expect**: Genie should call `discover_skills`, retrieve the local list (which contains `jira`, `github`, `kubernetes`, and `aws`), and summarize them to you.

### Scenario 2: Loading a specific skill
1. **Send prompt**: "Please load the kubernetes skill."
2. **Expect**: 
    - Genie calls `load_skill` with `{"name": "kubernetes"}`.
    - It reports that the skill was successfully loaded.

### Scenario 3: Utilizing the loaded skill
1. **Send prompt**: "Check what pods are running."
2. **Expect**: 
    - Genie should now have access to `skill_run`.
    - It calls `skill_run` providing `kubernetes` and instructions to check pods.
    - It receives the mock result (`nginx-deployment-75675f5897-9kpqw`) and reports it back.

### Scenario 4: Max limit protection
*Based on the test `genie.toml` configuration, max_loaded_skills is restricted to 2.*
1. **Send prompt**: "Please load the jira and github skills too."
2. **Expect**: 
    - Genie attempts to load the skills using `load_skill`.
    - Once it hits the 3rd attempt (because `kubernetes` is already loaded), the loading operation will specifically error stating the max limit was reached.
    - Genie informs you that it cannot load any more skills without unloading one first.

### Scenario 5: Unloading and swapping
1. **Send prompt**: "Unload kubernetes and load aws instead."
2. **Expect**:
    - Genie explicitly calls `unload_skill` targeting `kubernetes`.
    - Genie then successfully calls `load_skill` targeting `aws`.
    - Once finished, Genie confirms the swap and proceeds.

---
## ✅ Acceptance
- [ ] Genie successfully lists unloaded skills using `discover_skills`.
- [ ] Genie selectively loads skills via `load_skill`.
- [ ] Loaded custom skills actually function correctly via their standard tools.
- [ ] Genie hits context blockers actively when attempting to hoard skills past limitations.
- [ ] `unload_skill` correctly purges an active skill to accommodate new ones.
