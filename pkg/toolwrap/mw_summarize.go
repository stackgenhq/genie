// Package toolwrap – auto-summarization middleware for large tool results.
package toolwrap

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/htmlutils"
	"github.com/appcd-dev/genie/pkg/logger"
)

const (
	// defaultSummarizeThreshold is the character count above which a tool
	// response is automatically summarized. 100 000 chars ≈ 25 000 tokens.
	defaultSummarizeThreshold = 100_000

	// maxSummarizeInput is the safety-net cap on characters sent to the
	// summarizer LLM. After HTML stripping this should rarely be hit.
	maxSummarizeInput = 200_000

	// lowContentThreshold: if a large response (>100K chars) strips down to
	// fewer than this many characters, the page is almost certainly a
	// JS-rendered SPA or Cloudflare challenge with no server-rendered content.
	// We skip the summarizer and return a clear signal instead.
	lowContentThreshold = 500
)

// SummarizeFunc is called with the tool response content and returns a
// condensed version.  The concrete implementation is typically backed by
// agentutils.Summarizer, wired at the Service layer to avoid importing
// agentutils (and creating a circular dependency) from this package.
type SummarizeFunc func(ctx context.Context, content string) (string, error)

// SummarizeConfig controls the auto-summarization behaviour for large tool
// results.
type SummarizeConfig struct {
	// Enabled activates the summarization middleware. Default: false (opt-in).
	Enabled bool
	// Threshold is the character count above which a result is summarized.
	// When 0, defaultSummarizeThreshold (100 000) is used.
	Threshold int
}

// summarizeMiddleware compresses oversized tool results using an LLM
// summarizer (typically backed by a fast, large-context model like Flash).
type summarizeMiddleware struct {
	summarize SummarizeFunc
	threshold int
}

// AutoSummarizeMiddleware returns a Middleware that detects tool responses
// exceeding the configured character threshold and summarises them via the
// provided SummarizeFunc.  If summarize is nil the middleware is a no-op.
func AutoSummarizeMiddleware(summarize SummarizeFunc, threshold int) Middleware {
	if threshold <= 0 {
		threshold = defaultSummarizeThreshold
	}
	return &summarizeMiddleware{
		summarize: summarize,
		threshold: threshold,
	}
}

func (m *summarizeMiddleware) Wrap(next Handler) Handler {
	// No summarizer → pass-through.
	if m.summarize == nil {
		return next
	}
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		output, err := next(ctx, tc)
		if err != nil {
			return output, err
		}

		responseStr := fmt.Sprintf("%v", output)
		if len(responseStr) < m.threshold {
			return output, nil
		}

		logr := logger.GetLogger(ctx).With(
			"fn", "AutoSummarizeMiddleware",
			"tool", tc.ToolName,
			"original_chars", len(responseStr),
			"threshold", m.threshold,
		)
		logr.Info("tool response exceeds threshold, summarizing")

		// Step 1: Parse HTML and extract text content using the proper
		// HTML parser. Handles truncated responses, unclosed tags,
		// embedded JSON blobs, and malformed HTML gracefully.
		inputForSummary := htmlutils.ExtractText(responseStr)
		logr.Info("pre-processed summarizer input",
			"stripped_chars", len(inputForSummary),
			"reduction", fmt.Sprintf("%.0f%%", (1-float64(len(inputForSummary))/float64(len(responseStr)))*100),
		)

		// Detect JS-rendered SPAs: if a large page (>100K) strips down to
		// almost nothing, the page requires a browser to render. Skip the
		// summarizer (which would produce a useless 90-char summary) and
		// return a clear signal so the LLM doesn't retry this URL.
		if len(inputForSummary) < lowContentThreshold {
			logr.Info("low-content page detected — likely JS-rendered SPA",
				"original_chars", len(responseStr),
				"stripped_chars", len(inputForSummary),
			)
			return fmt.Sprintf(
				"[Page returned %d chars of HTML but only %d chars of text content. "+
					"This page likely requires JavaScript rendering and cannot be read via http_request. "+
					"Do NOT retry this URL — try a different source, a search engine, or a site that serves server-rendered HTML.]",
				len(responseStr), len(inputForSummary),
			), nil
		}

		// Step 2: Safety-net cap. After HTML stripping this should rarely
		// be hit, but protects against non-HTML mega-responses (e.g. huge
		// JSON blobs from an API).
		if len(inputForSummary) > maxSummarizeInput {
			inputForSummary = inputForSummary[:maxSummarizeInput] +
				fmt.Sprintf("\n\n... [truncated: showing %d of %d chars for summarization]",
					maxSummarizeInput, len(inputForSummary))
			logr.Info("capped summarizer input after stripping",
				"capped_chars", maxSummarizeInput)
		}

		summary, sErr := m.summarize(ctx, inputForSummary)
		if sErr != nil {
			logr.Warn("summarization failed, returning truncated response", "error", sErr)
			// Fall back to hard truncation rather than blowing up the context.
			truncated, _ := truncateResponse(responseStr)
			return truncated, nil
		}

		logr.Info("tool response summarized",
			"summary_chars", len(summary),
			"compression_ratio", fmt.Sprintf("%.1f%%", float64(len(summary))/float64(len(responseStr))*100),
		)

		// Annotate the summary so downstream consumers know it was compressed.
		annotated := fmt.Sprintf(
			"[Auto-summarized from %d chars to %d chars]\n\n%s",
			len(responseStr), len(summary), summary,
		)
		return annotated, nil
	}
}
