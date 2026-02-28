package interrupt

import "context"

// resumeKey is the context key for storing the resume value.
type resumeKey struct{}

// WithResumeValue returns a child context carrying the given resume value.
// The executor calls this when re-invoking a tool after an interrupt,
// passing the human's answer (string for clarifications, *hitl.ApprovalRequest
// for approvals). Tool functions retrieve it via [ResumeValueFrom].
func WithResumeValue(ctx context.Context, val any) context.Context {
	return context.WithValue(ctx, resumeKey{}, val)
}

// ResumeValueFrom extracts the resume value from ctx.
// Returns (nil, false) if this is a fresh (non-resumed) call.
func ResumeValueFrom(ctx context.Context) (any, bool) {
	v := ctx.Value(resumeKey{})
	if v == nil {
		return nil, false
	}
	return v, true
}
