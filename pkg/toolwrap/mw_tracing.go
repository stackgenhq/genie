// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap

import (
	"context"

	"github.com/stackgenhq/genie/pkg/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TracingConfig controls the TracingMiddleware.
type TracingConfig struct {
	Enabled bool `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
}

// tracerName is the OpenTelemetry instrumentation scope for tool calls.
const tracerName = "toolwrap"

// TracingMiddleware returns a Middleware that creates an OpenTelemetry span
// for every tool call. The span records the tool name, argument size, and
// any error that occurs. It also injects the span context into the downstream
// context so nested tool calls (e.g. sub-agents) are correlated as children.
// Without this middleware, tool calls are invisible in distributed traces,
// making it impossible to diagnose latency or failure chains.
func TracingMiddleware() MiddlewareFunc {
	tracer := otel.Tracer(tracerName)

	return func(next Handler) Handler {
		return func(ctx context.Context, tc *ToolCallContext) (any, error) {
			ctx, span := tracer.Start(ctx, "tool."+tc.ToolName,
				trace.WithAttributes(
					attribute.String("tool.name", tc.ToolName),
					attribute.Int("tool.args.size", len(tc.Args)),
				),
				trace.WithSpanKind(trace.SpanKindInternal),
			)
			defer span.End()

			output, err := next(ctx, tc)

			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, "")
			}

			logr := logger.GetLogger(ctx).With("fn", "TracingMiddleware", "tool", tc.ToolName)
			logr.Debug("tracing span completed",
				"traceID", span.SpanContext().TraceID().String(),
				"spanID", span.SpanContext().SpanID().String(),
			)

			return output, err
		}
	}
}
