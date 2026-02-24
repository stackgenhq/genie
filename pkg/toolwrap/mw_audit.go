package toolwrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/interrupt"
)

type auditMiddleware struct {
	auditor audit.Auditor
}

func (a auditMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		output, err := next(ctx, tc)
		errStr := ""
		switch {
		case interrupt.Is(err):
			errStr = "interrupted" // expected flow control, not a failure
		case err != nil:
			errStr = err.Error()
		}
		responseStr, truncated := truncateResponse(fmt.Sprintf("%v", output))
		a.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventToolCall,
			Actor:     "expert",
			Action:    tc.ToolName,
			Metadata: map[string]interface{}{
				"args":            redactSensitiveArgs(tc.Args),
				"justification":   tc.Justification,
				"response_length": len(responseStr),
				"truncated":       truncated,
				"error":           errStr,
			},
		})
		return output, err
	}
}

// AuditMiddleware returns a Middleware that writes tool call results to
// the audit trail for compliance and debugging. Without this middleware,
// tool calls would produce no durable trace.
func AuditMiddleware(auditor audit.Auditor) Middleware {
	if auditor == nil {
		auditor = &basicAuditor{
			w: os.Stderr,
		}
	}
	return auditMiddleware{auditor: auditor}
}

// basicAuditor implements Auditor and writes structured JSON logs to stderr
type basicAuditor struct {
	w io.Writer
}

func (s *basicAuditor) Log(ctx context.Context, req audit.LogRequest) {
	data, err := json.Marshal(req)
	if err != nil {
		_, _ = fmt.Fprintf(s.w, "error marshaling audit log: %v\n", err)
		return
	}
	_, _ = fmt.Fprintf(s.w, "%s\n", data)
}

func (s *basicAuditor) Close() error {
	return nil
}
