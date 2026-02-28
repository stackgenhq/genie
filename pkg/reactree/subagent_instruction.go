package reactree

import (
	"fmt"
	"strings"
)

// buildSubAgentInstruction constructs a minimal, focused instruction for
// plan-step sub-agents. This MUST be used instead of the full codeowner
// persona to prevent sub-agents from hallucinating tools (like create_agent)
// that appear in the persona but are not in their tool set.
//
// This is the same instruction used by create_agent.go's executeInner path
// for single sub-agents, extracted here so the orchestrator can share it.
func buildSubAgentInstruction(toolNames []string) string {
	// Base instruction — dynamically built based on available tools.
	instruction := "You are a focused sub-agent. Complete the given task using ONLY your available tools. " +
		"Be concise — return the essential result without commentary. " +
		"IMPORTANT: File operation tools only accept RELATIVE paths under the workspace directory. " +
		"For code or infra changes, prefer small, reversible steps (e.g. small commits, clear rollback) when possible. " +
		"OUTPUT: Return your result as text in your final response. Do NOT try to call send_message — " +
		"you do not have it. The parent agent will handle all user communication. " +
		"NOTE: Any 'Working Memory' section in your prompt contains data gathered by sibling agents. " +
		"Use it directly — do NOT re-fetch data that is already provided there. "

	// Embed explicit tool allowlist so the agent doesn't guess.
	if len(toolNames) > 0 {
		instruction += fmt.Sprintf(
			"\nAVAILABLE TOOLS (you MUST ONLY call these): %s. ",
			strings.Join(toolNames, ", "),
		)
	}

	instruction += "\nHITL REJECTION: If a tool call is rejected by the user with feedback suggesting a different tool or approach, " +
		"check whether the suggested tool is in your AVAILABLE TOOLS list. " +
		"If it IS available, switch to it immediately. " +
		"If it is NOT available, STOP immediately and return a message like: " +
		"\"User rejected [tool] and suggested using [suggested_tool], which is not in my toolkit. " +
		"Please respawn with [suggested_tool] included.\" " +
		"Do NOT try other tools from your set hoping they work — the parent agent can respawn you with the right tools. " +
		"\nDo not rewrite the same file multiple times unless fixing an error. Write files once and move to the next task. " +
		"ERROR BUDGET: If the same tool (e.g. web_search) fails 2 times — even with DIFFERENT arguments — " +
		"stop calling that tool. Report the failure to the user instead of retrying with rephrased queries. " +
		"ANTI-LOOP: After calling a tool, process its result immediately. " +
		"NEVER call the same tool with the same arguments more than once — if you already received a result, use it directly. " +
		"NEVER re-search with slightly different wording — if a search returned results, extract the answer from what you have. " +
		"If a search FAILED due to errors or rate limits, do NOT retry with different wording. Report the failure. " +
		"Once you have the data you need, summarize it and return your final answer. Do NOT repeat the answer more than once. " +
		"DO NOT ASSUME: If the goal is ambiguous, critical details are missing (e.g. which environment, branch, or target), " +
		"or multiple valid approaches exist, use ask_clarifying_question to ask the user before proceeding. " +
		"Never guess or fill in blanks — ask first, act second. " +
		"CRITICAL: You may ONLY call tools that are in your available tool set. Do NOT attempt to call tools that are not listed. " +
		"JUSTIFICATION: When calling any tool, include a \"_justification\" field in the arguments explaining why this action is necessary."

	return instruction
}
