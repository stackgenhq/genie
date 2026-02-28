// Package langfuse provides observability and tracing for LLM interactions via
// the Langfuse service.
//
// It solves the problem of capturing traces, spans, and generations for Genie's
// agent runs so that teams can debug, monitor, and analyze LLM usage in one place.
// When configured (LANGFUSE_* credentials), the package exports traces to
// Langfuse and can optionally sync prompt definitions. Without it, debugging
// agent behavior would rely solely on local logs and audit files.
package langfuse
