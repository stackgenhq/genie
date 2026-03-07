package modelprovider

// TaskType represents different categories of tasks that LLMs are benchmarked against.
// These task types help in selecting the most appropriate model based on the specific
// requirements of the work being performed.
type TaskType string

const (
	// TaskToolCalling represents tasks requiring reliable generation of executable code and API calls.
	// Benchmark: BFCL v4 (Berkeley Function Calling Leaderboard)
	// Top performers: Llama 3.1 405B Instruct (88.50%), Claude Opus 4.5 FC (77.47%)
	// Use this for: Function calling, API integration, structured code generation
	TaskToolCalling TaskType = "tool_calling"

	// TaskPlanning represents tasks involving agentic planning and coding for real-world software engineering.
	// Benchmark: SWE-Bench (Software Engineering Benchmark)
	// Top performers: Claude Sonnet 4.5 Parallel (82.00%), Claude Opus 4.5 (80.90%)
	// Use this for: Complex refactoring, multi-file changes, architectural decisions
	TaskPlanning TaskType = "planning"

	// TaskCoding represents pure code generation, algorithmic problem solving, and script writing.
	// Benchmarks: HumanEval (pioneered by Codex), MBPP, LiveCodeBench
	// Top performers: Claude Sonnet 4.5, GPT-5.2
	// Use this for: Single-function generation, copilot-style autocomplete, algorithmic coding
	TaskCoding TaskType = "coding"

	// TaskTerminalCalling represents tasks requiring precision in command-line interfaces and terminal operations.
	// Benchmark: Terminal Execution Bench 2.0
	// Top performers: Claude Sonnet 4.5 (61.30%), Claude Opus 4.5 (59.30%)
	// Use this for: Shell scripting, CLI tool usage, system administration tasks
	TaskTerminalCalling TaskType = "terminal_calling"

	// TaskScientificReasoning represents tasks requiring PhD-level scientific reasoning and logic.
	// Benchmark: GPQA Diamond (Graduate-Level Google-Proof Q&A)
	// Top performers: Gemini 3 Pro Deep Think (93.80%), GPT-5.2 (92.40%)
	// Use this for: Complex analysis, research tasks, domain-specific expertise
	TaskScientificReasoning TaskType = "scientific_reasoning"

	// TaskNovelReasoning represents tasks testing abstract visual pattern solving for never-before-seen problems.
	// Benchmark: ARC-AGI 2 (Abstraction and Reasoning Corpus)
	// Top performers: GPT-5.2 Pro High (54.20%), Poetiq Gemini 3 Pro Refine (54.00%)
	// Use this for: Novel problem-solving, pattern recognition, creative solutions
	TaskNovelReasoning TaskType = "novel_reasoning"

	// TaskGeneralTask represents broad knowledge tasks and general reasoning capabilities.
	// Benchmark: Humanity's Last Exam
	// Top performers: Gemini 3 Pro Deep Think (41.00%), Gemini 3 Pro Standard (37.50%)
	// Use this for: General knowledge queries, broad reasoning, interdisciplinary tasks
	TaskGeneralTask TaskType = "general_task"

	// TaskMathematical represents high-level competition mathematics and quantitative reasoning.
	// Benchmark: AIME 2025 (American Invitational Mathematics Examination)
	// Top performers: GPT-5.2 (100.00%), Gemini 3 Pro (100.00%), Grok 4.1 Heavy (100.00%)
	// Use this for: Mathematical proofs, quantitative analysis, algorithmic optimization
	TaskMathematical TaskType = "mathematical"

	// TaskLongHorizonAutonomy represents extended autonomous operation capabilities.
	// Benchmark: METR (measured in minutes before 50% failure rate)
	// Top performers: GPT-5 Medium (137.3 min), Claude Sonnet 4.5 (113.3 min)
	// Use this for: Long-running autonomous agents, multi-step workflows, sustained reasoning
	TaskLongHorizonAutonomy TaskType = "long_horizon_autonomy"

	// TaskEfficiency represents operational speed and cost efficiency considerations.
	// Benchmarks: Throughput (tokens/sec) and Cost (USD per 1M input tokens)
	// Throughput leaders: Llama 4 Scout (2600 t/s), Grok 4.1 (455 t/s)
	// Cost leaders: Grok 4.1 ($0.20), Gemini 3 Flash ($0.50)
	// Use this for: High-volume processing, cost-sensitive operations, real-time applications
	TaskEfficiency TaskType = "efficiency"

	// TaskSummarizer represents tasks requiring large-context summarization of
	// verbose tool outputs. Typically mapped to a model with a very large context
	// window (e.g., 1M tokens) so it can ingest and compress raw HTML, API
	// responses, or other bulk data before handing it back to a smaller agent.
	// Use this for: Auto-summarizing oversized tool results, condensing documents
	TaskSummarizer TaskType = "summarizer"

	// TaskComputerOperations represents native computer-use capabilities.
	// Benchmark: OSWorld-Verified, WebArena Verified, APEX-Agents
	// Top performers: GPT-5.4 Pro
	// Use this for: Operating applications via keyboard and mouse commands
	TaskComputerOperations TaskType = "computer_operations"
)
