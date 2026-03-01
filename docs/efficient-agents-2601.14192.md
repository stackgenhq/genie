# Toward Efficient Agents: Memory, Tool Learning, and Planning (arXiv:2601.14192)

This document maps the survey [Toward Efficient Agents: Memory, Tool learning, and Planning](https://arxiv.org/abs/2601.14192) to Genie and trpc-agent-go so we can tune memory and token usage. See also: model provider option `enable_token_tailoring` in the Config Builder and `config.toml.example`.

## Paper summary

The paper frames agent **efficiency** across three pillars:

1. **Memory** — Bounding context via compression and retrieval; working memory + external memory.
2. **Tool learning** — Minimizing tool invocation (budgeted use, caching, cost-aware policies).
3. **Planning** — Controlled search (hierarchical planning, pruning, adaptive budgeting).

Efficiency is characterized as:

- Effectiveness under a **fixed cost budget** (tokens, latency, steps).
- **Cost at comparable effectiveness** (Pareto frontier).

## Mapping to Genie and trpc-agent-go

### Memory (context bounding and compression)

| Paper idea | Genie / trpc-agent-go |
|------------|------------------------|
| Bounding context | **trpc-agent-go** `model.TailoringStrategy` (MiddleOutStrategy, HeadOut, TailOut) and `TokenTailoringConfig`; **Genie** enables token tailoring in modelprovider so messages are trimmed to model context window. |
| Working memory compression | **Genie** `pkg/toolwrap/mw_summarize.go`: auto-summarize tool results above a character threshold (default 100K chars). |
| History limits | **Genie** `ExpertConfig.MaxHistoryRuns`; **trpc-agent-go** `llmagent.WithMaxHistoryRuns()` and message filter modes (`RequestContext` etc.). |
| Episodic / external memory | **Genie** `pkg/reactree/memory/episodic.go` + `memory.Service`; vector memory in `pkg/memory/vector`. |
| Masking processed context | **trpc-agent-go** `session.Session.MaskEvents()` / `GetVisibleEvents()` (Pensieve-style pruning). |

### Tool learning (fewer, cheaper tool calls)

| Paper idea | Genie / trpc-agent-go |
|------------|------------------------|
| Budgeted tool use | **Genie** `TreeConfig.ToolBudgets` (e.g. `create_agent: 3`, `ask_clarifying_question: 1`). |
| Iteration caps | **Genie** `ExpertConfig.MaxLLMCalls`, `MaxToolIterations`; **trpc-agent-go** `llmagent.WithMaxLLMCalls`, `WithMaxToolIterations`. |
| Caching to avoid repeat calls | **Genie** `WorkingMemory` caches file-read results; **Genie** summarizer condenses large tool outputs. |

### Planning (controlled search)

| Paper idea | Genie / trpc-agent-go |
|------------|------------------------|
| Depth and node limits | **Genie** `TreeConfig.MaxDepth`, `MaxDecisionsPerNode`, `MaxTotalNodes`, `MaxIterations`. |
| Pareto-style presets | **Genie** `CostOptimizedConfig()`, `DefaultExpertConfig()`, `HighPerformanceConfig()` in `pkg/expert/expert_config.go`. |

## Concrete knobs to tweak

- **Token tailoring (context bounding)**  
  Genie turns on token tailoring when building OpenAI, Gemini, Anthropic, and Ollama models so trpc-agent-go can trim conversation history to the model’s context window (MiddleOut by default).

- **ExpertConfig**  
  Use `CostOptimizedConfig()` for low token/step usage; `HighPerformanceConfig()` for harder tasks; adjust `MaxHistoryRuns` to control how much history is included.

- **TreeConfig**  
  Lower `MaxIterations`, `MaxDecisionsPerNode`, or `ToolBudgets` to reduce steps and tool calls.

- **Summarization**  
  Enable `toolwrap.AutoSummarizeMiddleware` and set `SummarizeConfig.Threshold` so large tool responses are summarized before being sent back to the LLM.

- **trpc-agent-go**  
  `Session.MaskEvents` / `GetVisibleEvents` can be used to hide already-processed events from the prompt while keeping them for audit.

---

## How context is bounded (two stages)

1. **History building** — Genie and trpc-agent-go limit how much history is included in the first place: `ExpertConfig.MaxHistoryRuns`, message filter mode (`RequestContext`), and (in trpc-agent-go) which events are visible via `GetVisibleEvents()`.
2. **Token tailoring** — If the resulting message list still exceeds the model’s context window, trpc-agent-go’s tailoring (e.g. MiddleOut) trims it: system messages and the last turn are kept; messages are dropped from the middle first. So you get “fewer runs, each small” or “more runs, then trimmed” depending on these knobs.

Tuning both stages gives you control over the Pareto trade-off between context length and cost.

---

## Observability and regression

- **When tailoring runs** — trpc-agent-go logs at debug when it computes `maxInputTokens` (e.g. “auto-calculated max input tokens: model=…, contextWindow=…, maxInputTokens=…”). Genie logs at debug when token tailoring is enabled for a provider (“token tailoring enabled for model provider”).
- **Unknown model** — If the model name is not recognized by trpc-agent-go, the context window may be wrong and tailoring can over-trim or under-trim. Prefer known model names or set explicit limits in the provider if needed.
- **Long conversations** — After enabling token tailoring, long chats may see more context dropped than before. Monitor success rates, user feedback, and token/latency metrics; use `enable_token_tailoring = false` for a provider if you need full history for that model.

---

## Privacy and audit

- **Trimmed content** — Trimming only affects what is sent to the model in that request. It does not delete history from the session or store. Full conversation history can still be persisted and audited elsewhere.
- **Sensitive content** — Dropped (middle) turns may have contained PII or critical instructions. Rely on retention and access policy for stored history; consider keeping `MaxHistoryRuns` and tailoring settings consistent with compliance needs.
