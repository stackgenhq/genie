// Package toolwrap – PII rehydration of tool-call arguments before execution.
package toolwrap

import (
	"context"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/pii"
)

// PIIRehydrateMiddleware returns a Middleware that rehydrates [HIDDEN:hash]
// placeholders in tool-call arguments back to original values before the tool
// runs. The LLM sees redacted content (e.g. [HIDDEN:53233f] instead of an
// email) and may echo that in tool arguments; without rehydration, tools like
// email_send would receive invalid values and fail (e.g. "recipient address
// <[HIDDEN:53233f]> is not a valid RFC 5321 address"). The replacer is stored
// in context by the model BeforeModel callback (pii.WithReplacer). When
// present, this middleware replaces placeholders in tc.Args so the tool
// receives real addresses, tokens, etc.
func PIIRehydrateMiddleware() MiddlewareFunc {
	return func(next Handler) Handler {
		return func(ctx context.Context, tc *ToolCallContext) (any, error) {
			replacer := pii.ReplacerFromContext(ctx)
			if replacer != nil && len(tc.Args) > 0 {
				rehydrated := replacer.Replace(string(tc.Args))
				if rehydrated != string(tc.Args) {
					tc.Args = []byte(rehydrated)
					logr := logger.GetLogger(ctx).With("fn", "PIIRehydrateMiddleware", "tool", tc.ToolName)
					logr.Debug("rehydrated PII in tool arguments")
				}
			}
			return next(ctx, tc)
		}
	}
}
