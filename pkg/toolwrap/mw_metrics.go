package toolwrap

import (
	"context"
	"time"

	"github.com/stackgenhq/genie/pkg/interrupt"
	"github.com/stackgenhq/genie/pkg/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricsConfig controls the MetricsMiddleware.
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	Prefix  string `yaml:"prefix,omitempty" toml:"prefix,omitempty"`
}

// metricsState holds the lazily-initialised OTel instruments.
// Created once per MetricsMiddleware instance.
type metricsState struct {
	callCounter metric.Int64Counter
	duration    metric.Float64Histogram
	errorCount  metric.Int64Counter
}

// MetricsMiddleware returns a Middleware that records OpenTelemetry
// metrics for every tool call: a call counter, a latency histogram,
// and an error counter. Without this middleware, operators have no
// quantitative signal for tool usage patterns, p99 latencies, or
// failure rates.
func (cfg MetricsConfig) MetricsMiddleware() MiddlewareFunc {
	meter := otel.Meter(cfg.Prefix)
	state := &metricsState{}

	state.callCounter, _ = meter.Int64Counter(cfg.Prefix+".call.count",
		metric.WithDescription("Total number of tool calls"),
	)
	state.duration, _ = meter.Float64Histogram(cfg.Prefix+".call.duration_ms",
		metric.WithDescription("Tool call duration in milliseconds"),
		metric.WithUnit("ms"),
	)
	state.errorCount, _ = meter.Int64Counter(cfg.Prefix+".call.errors",
		metric.WithDescription("Total number of failed tool calls"),
	)

	return func(next Handler) Handler {
		return func(ctx context.Context, tc *ToolCallContext) (any, error) {
			start := time.Now()
			attrs := []attribute.KeyValue{
				attribute.String("tool.name", tc.ToolName),
			}

			state.callCounter.Add(ctx, 1, metric.WithAttributes(attrs...))

			output, err := next(ctx, tc)

			elapsed := time.Since(start)
			state.duration.Record(ctx, float64(elapsed.Milliseconds()), metric.WithAttributes(attrs...))

			if err != nil && !interrupt.Is(err) {
				state.errorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
			}

			logr := logger.GetLogger(ctx).With("fn", "MetricsMiddleware", "tool", tc.ToolName)
			logr.Debug("metrics recorded", "duration_ms", elapsed.String(), "errored", err != nil)

			return output, err
		}
	}
}
