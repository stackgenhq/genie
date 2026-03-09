package db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/graph"
)

// retryCheckpointSaver wraps a graph.CheckpointSaver and retries write
// operations (Put, PutWrites, PutFull) on transient database errors such as
// connection refused, connection reset, or broken pipe. Read operations are
// not retried since the graph executor treats read failures as "no checkpoint
// found" and proceeds.
//
// This is needed because Karpenter node consolidation can briefly take
// PostgreSQL offline, and the upstream graph executor logs a hard ERROR when
// a checkpoint save fails. Retrying with exponential backoff bridges the gap
// until the new pod is ready.
type retryCheckpointSaver struct {
	inner      graph.CheckpointSaver
	maxRetries int
	baseDelay  time.Duration
	logger     *slog.Logger
}

// RetryOption configures the retry behaviour of a retryCheckpointSaver.
type RetryOption func(*retryCheckpointSaver)

// WithMaxRetries sets the maximum number of retry attempts (default 3).
func WithMaxRetries(n int) RetryOption {
	return func(r *retryCheckpointSaver) { r.maxRetries = n }
}

// WithBaseDelay sets the initial backoff delay (default 500ms).
// Each subsequent retry doubles the delay.
func WithBaseDelay(d time.Duration) RetryOption {
	return func(r *retryCheckpointSaver) { r.baseDelay = d }
}

// WithRetryLogger sets the logger for retry warnings.
func WithRetryLogger(l *slog.Logger) RetryOption {
	return func(r *retryCheckpointSaver) { r.logger = l }
}

// NewRetryCheckpointSaver wraps a CheckpointSaver with automatic retry on
// transient DB errors.
func NewRetryCheckpointSaver(inner graph.CheckpointSaver, opts ...RetryOption) graph.CheckpointSaver {
	r := &retryCheckpointSaver{
		inner:      inner,
		maxRetries: 3,
		baseDelay:  500 * time.Millisecond,
		logger:     slog.Default(),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// --- Reads (no retry, passed through) ---

func (r *retryCheckpointSaver) Get(ctx context.Context, config map[string]any) (*graph.Checkpoint, error) {
	return r.inner.Get(ctx, config)
}

func (r *retryCheckpointSaver) GetTuple(ctx context.Context, config map[string]any) (*graph.CheckpointTuple, error) {
	return r.inner.GetTuple(ctx, config)
}

func (r *retryCheckpointSaver) List(ctx context.Context, config map[string]any, filter *graph.CheckpointFilter) ([]*graph.CheckpointTuple, error) {
	return r.inner.List(ctx, config, filter)
}

// --- Writes (retried on transient errors) ---

func (r *retryCheckpointSaver) Put(ctx context.Context, req graph.PutRequest) (map[string]any, error) {
	var result map[string]any
	err := r.retryOp(ctx, "Put", func() error {
		var e error
		result, e = r.inner.Put(ctx, req)
		return e
	})
	return result, err
}

func (r *retryCheckpointSaver) PutWrites(ctx context.Context, req graph.PutWritesRequest) error {
	return r.retryOp(ctx, "PutWrites", func() error {
		return r.inner.PutWrites(ctx, req)
	})
}

func (r *retryCheckpointSaver) PutFull(ctx context.Context, req graph.PutFullRequest) (map[string]any, error) {
	var result map[string]any
	err := r.retryOp(ctx, "PutFull", func() error {
		var e error
		result, e = r.inner.PutFull(ctx, req)
		return e
	})
	return result, err
}

func (r *retryCheckpointSaver) DeleteLineage(ctx context.Context, lineageID string) error {
	return r.retryOp(ctx, "DeleteLineage", func() error {
		return r.inner.DeleteLineage(ctx, lineageID)
	})
}

func (r *retryCheckpointSaver) Close() error {
	return r.inner.Close()
}

// retryOp retries op up to maxRetries times if the error is transient.
func (r *retryCheckpointSaver) retryOp(ctx context.Context, opName string, op func() error) error {
	var lastErr error
	delay := r.baseDelay

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		lastErr = op()
		if lastErr == nil {
			if attempt > 0 {
				r.logger.Info("checkpoint retry succeeded",
					"op", opName,
					"attempt", attempt+1,
				)
			}
			return nil
		}

		if !isTransientDBError(lastErr) {
			return lastErr
		}

		if attempt < r.maxRetries {
			r.logger.Warn("transient DB error on checkpoint save, retrying",
				"op", opName,
				"attempt", attempt+1,
				"maxRetries", r.maxRetries,
				"delay", delay,
				"error", lastErr,
			)

			select {
			case <-ctx.Done():
				return fmt.Errorf("checkpoint %s context cancelled during retry: %w", opName, ctx.Err())
			case <-time.After(delay):
			}
			delay *= 2 // exponential backoff
		}
	}
	return fmt.Errorf("checkpoint %s failed after %d retries: %w", opName, r.maxRetries, lastErr)
}

// isTransientDBError returns true if the error is likely caused by a
// temporary database connectivity issue (connection refused, reset, broken
// pipe, EOF, etc.).
func isTransientDBError(err error) bool {
	if err == nil {
		return false
	}

	// Check for net.OpError (connection refused, connection reset, etc.)
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}

	// Check for unexpected EOF (connection dropped)
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}

	// String-match fallback for driver-level errors that don't expose typed errors.
	msg := err.Error()
	transientPatterns := []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"dial error",
		"dial tcp",
		"connect: connection refused",
		"server closed the connection unexpectedly",
		"connection timed out",
		"no connection to the server",
		"the database system is starting up",
		"the database system is shutting down",
	}
	for _, p := range transientPatterns {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(p)) {
			return true
		}
	}

	return false
}
