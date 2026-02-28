// Package agentutils provides utilities used by the agent and orchestrator layer.
//
// It solves the problem of condensing verbose tool outputs and content into
// structured formats (JSON, Text, YAML) before passing them to the LLM or
// downstream consumers. Without it, long tool responses would consume context
// window and obscure the relevant information.
//
// The main component is Summarizer: it uses a lightweight LLM model to produce
// context-aware summaries in a requested output format, so that agents can
// work with concise, structured data instead of raw payloads.
package agentutils
