// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package langfuse

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/langfuse"
)

var once sync.Once

func (c Config) Init(ctx context.Context) {
	once.Do(func() {
		defaultClient = c.NewClient()
		c.startTrace(ctx)
	})
}

func (c Config) startTrace(ctx context.Context) {
	logger := logger.GetLogger(ctx).With("fn", "langfuse.startTrace")
	if c.PublicKey == "" || c.SecretKey == "" || c.Host == "" {
		logger.Warn("No Langfuse configuration found")
		return
	}

	// Initialize Langfuse tracing
	cleanup, err := langfuse.Start(ctx,
		langfuse.WithHost(c.langfuseOTLPEndpoint()),
		langfuse.WithPublicKey(c.PublicKey),
		langfuse.WithSecretKey(c.SecretKey),
	)
	if err != nil {
		logger.Warn("could not start the tracer", "fn", "langfuse.Init", "error", err)
		return
	}
	logger.Info("langfuse tracing started", "host", c.langfuseOTLPEndpoint())

	// Ensure cleanup is called with proper timeout for trace flushing
	go func() {
		<-ctx.Done()
		logger.Info("Stopping langfuse tracer")
		// Create a context with timeout to allow traces to be flushed
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := cleanup(cleanupCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error during Trace cleanup: %v\n", err)
		}
	}()
}
