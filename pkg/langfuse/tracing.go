package langfuse

import (
	"context"
	"fmt"
	"os"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/telemetry/langfuse"
)

func StartTrace(ctx context.Context) error {
	if LangfusePublicKey == "" || LangfuseSecretKey == "" {
		return nil
	}
	// Initialize Langfuse tracing
	cleanup, err := langfuse.Start(ctx,
		langfuse.WithHost(LangfuseHost),
		langfuse.WithPublicKey(LangfusePublicKey),
		langfuse.WithSecretKey(LangfuseSecretKey),
	)
	if err != nil {
		// Log the error but don't panic - allow genie to continue without tracing
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize Langfuse tracing: %v\n", err)
		return nil
	}

	// Ensure cleanup is called with proper timeout for trace flushing
	go func() {
		<-ctx.Done()
		// Create a context with timeout to allow traces to be flushed
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := cleanup(cleanupCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error during Trace cleanup: %v\n", err)
		}
	}()

	return nil
}
