// Package toolwrap provides composable middleware for tool execution.
//
// Each Middleware wraps a Handler, returning a new Handler with added
// behaviour (audit logging, HITL approval, caching, loop detection, etc.).
// The Service assembles the default middleware chain via DefaultMiddlewares
package toolwrap

import "context"

// ToolCallContext carries per-call state through the middleware chain.
// Middlewares read and write fields to communicate with each other
// (e.g. JustificationExtract sets Justification, HITLApproval reads it).
type ToolCallContext struct {
	// ToolName is the declared name of the tool being called.
	ToolName string
	// OriginalArgs is the raw JSON arguments before any middleware processing.
	OriginalArgs []byte
	// Args is the JSON arguments after middleware processing (e.g. _justification stripped).
	Args []byte
	// Justification is extracted from the _justification field by the HITL middleware.
	Justification string

	// ResumeValue is set by the executor when resuming a previously-interrupted
	// tool call. The concrete type depends on the InterruptKind:
	//   - InterruptClarify:  string (the user's answer)
	//   - InterruptApproval: *hitl.ApprovalRequest (with resolved status)
	//
	// When nil, this is a fresh (non-resumed) call.
	ResumeValue any
}

//go:generate go tool counterfeiter -generate

// Handler is a function that processes a tool call and returns its output.
// The terminal handler executes the real tool; middlewares wrap this with
// cross-cutting concerns.
type Handler func(ctx context.Context, tc *ToolCallContext) (any, error)

//counterfeiter:generate . Middleware

// Middleware wraps a Handler, returning a new Handler with added behaviour.
// Middlewares are composed in order: the first middleware in the slice is the
// outermost wrapper and executes first.
//
// The open-source default chain (from DefaultMiddlewares) is:
//
//	LoopDetection → FailureLimit → SemanticCache → FileCache →
//	HITLApproval → ContextEnrich → Audit → Execute
type Middleware interface {
	Wrap(next Handler) Handler
}

type MiddlewareFunc func(next Handler) Handler

func (f MiddlewareFunc) Wrap(next Handler) Handler {
	return f(next)
}

type CompositeMiddleware []Middleware

func (c CompositeMiddleware) Wrap(next Handler) Handler {
	return Chain(next, c...)
}

// Chain composes a slice of middlewares into a single Handler by wrapping
// the terminal handler from outermost (index 0) to innermost (last index).
// If middlewares is empty, the terminal handler is returned unchanged.
func Chain(terminal Handler, middlewares ...Middleware) Handler {
	h := terminal
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i].Wrap(h)
	}
	return h
}
