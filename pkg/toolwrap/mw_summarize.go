// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package toolwrap – auto-summarization middleware for large tool results.
package toolwrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/htmlutils"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
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

// defaultSummarizeCacheTTL is how long a cached summary remains valid. Matches the
// typical lifetime of a multi-step plan execution so that identical tool
// outputs within the same plan are served from cache.
const defaultSummarizeCacheTTL = 10 * time.Minute

// cachedSummary holds a previously computed summary and its expiry time.
type cachedSummary struct {
	summary   string
	expiresAt time.Time
}

// summarizeMiddleware compresses oversized tool results using an LLM
// summarizer (typically backed by a fast, large-context model like Flash).
// It includes a content-hash cache to avoid re-summarizing identical content
// across sub-agent steps.
type summarizeMiddleware struct {
	summarize SummarizeFunc
	threshold int
	cache     sync.Map // SHA-256 hex → cachedSummary
	cacheTTL  time.Duration
}

// AutoSummarizeMiddleware returns a Middleware that detects tool responses
// exceeding the configured character threshold and summarises them via the
// provided SummarizeFunc.  If summarize is nil the middleware is a no-op.
// Identical content is served from a short-lived cache to avoid redundant
// LLM calls when multiple sub-agent steps produce the same tool output.
func AutoSummarizeMiddleware(summarize SummarizeFunc, threshold int) Middleware {
	if threshold <= 0 {
		threshold = defaultSummarizeThreshold
	}
	m := &summarizeMiddleware{
		summarize: summarize,
		threshold: threshold,
		cacheTTL:  defaultSummarizeCacheTTL,
	}
	// Start a background goroutine to periodically clean up expired cache entries.
	// This prevents unbounded memory growth in long-running processes.
	go m.cleanupLoop()
	return m
}

func (m *summarizeMiddleware) cleanupLoop() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		m.cache.Range(func(key, value any) bool {
			if cs, ok := value.(cachedSummary); ok && now.After(cs.expiresAt) {
				m.cache.Delete(key)
			}
			return true
		})
	}
}

// sanitizeInput checks whether the pre-processed content is worth
// summarizing and applies a safety-net cap. It returns the (possibly
// truncated) input, an optional early-return message, and a boolean
// indicating the caller should short-circuit.
func (m *summarizeMiddleware) sanitizeInput(ctx context.Context, responseStr, inputForSummary string) (string, string, bool) {
	logr := logger.GetLogger(ctx).With(
		"fn", "summarizeMiddleware.sanitizeInput",
	)
	// Low-content detection: if a large response strips down to almost
	// nothing, skip the summarizer and return a clear signal.
	if len(inputForSummary) < lowContentThreshold {
		isHTML := looksLikeHTML(responseStr)
		logr.Debug("low-content result detected",
			"original_chars", len(responseStr),
			"stripped_chars", len(inputForSummary),
			"is_html", isHTML,
		)
		if isHTML {
			return "", fmt.Sprintf(
				"[Page returned %d chars of HTML but only %d chars of text content. "+
					"This page likely requires JavaScript rendering and cannot be read via http_request. "+
					"Do NOT retry this URL — try a different source, a search engine, or a site that serves server-rendered HTML.]",
				len(responseStr), len(inputForSummary),
			), true
		}
		return "", fmt.Sprintf(
			"[Response contained %d chars but only %d chars of meaningful content after stripping. "+
				"The output was mostly whitespace, escape sequences, or repetitive data. It does not contain useful text.]",
			len(responseStr), len(inputForSummary),
		), true
	}

	// Safety-net cap. After stripping this should rarely be hit, but
	// protects against non-HTML mega-responses (e.g. huge JSON blobs).
	if len(inputForSummary) > maxSummarizeInput {
		inputForSummary = inputForSummary[:maxSummarizeInput] +
			fmt.Sprintf("\n\n... [truncated: showing %d of %d chars for summarization]",
				maxSummarizeInput, len(inputForSummary))
		logr.Info("capped summarizer input after stripping",
			"capped_chars", maxSummarizeInput)
	}

	return inputForSummary, "", false
}

func (m *summarizeMiddleware) Wrap(next Handler) Handler {
	// No summarizer → pass-through.
	if m.summarize == nil {
		return next
	}
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		skipped := false
		ctx = toolcontext.WithSkipSummarizeSetter(ctx, func() { skipped = true })

		output, err := next(ctx, tc)
		if err != nil {
			return output, err
		}

		if skipped {
			logger.GetLogger(ctx).Debug("skipping auto-summarization due to context marker", "tool", tc.ToolName)
			return output, nil
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

		// Step 1: Pre-process content. Use HTML extraction for HTML
		// content and ANSI/whitespace stripping for everything else
		// (shell output, JSON blobs, etc.).
		var inputForSummary string
		if looksLikeHTML(responseStr) {
			inputForSummary = htmlutils.ExtractText(responseStr)
		} else {
			inputForSummary = preProcessNonHTML(responseStr)
		}
		logr.Info("pre-processed summarizer input",
			"stripped_chars", len(inputForSummary),
			"reduction", fmt.Sprintf("%.0f%%", (1-float64(len(inputForSummary))/float64(len(responseStr)))*100),
		)

		// Validate and cap the pre-processed input; short-circuit on
		// low-content results that aren't worth summarizing.
		inputForSummary, earlyMsg, done := m.sanitizeInput(ctx, responseStr, inputForSummary)
		if done {
			return earlyMsg, nil
		}

		// Step 3: Check content-hash cache. Identical tool outputs
		// (e.g. sequential sub-agents running the same kubectl command)
		// return the cached summary instead of re-invoking the LLM.
		cacheKey := contentHash(inputForSummary)
		if cached, ok := m.cache.Load(cacheKey); ok {
			entry := cached.(cachedSummary)
			if time.Now().Before(entry.expiresAt) {
				logr.Info("returning cached summary",
					"cache_key", cacheKey[:12],
					"summary_chars", len(entry.summary),
				)
				return fmt.Sprintf(
					"[Auto-summarized from %d chars — cached]\n\n%s",
					len(responseStr), entry.summary,
				), nil
			}
			// Expired — delete and re-summarize.
			m.cache.Delete(cacheKey)
		}

		summary, sErr := m.summarize(ctx, fmt.Sprintf("Tool with the name %q was invoked with arguments %v, and the following was the response:\n\n%s", tc.ToolName, tc.Args, inputForSummary))
		if sErr != nil {
			logr.Warn("summarization failed, returning original response", "error", sErr)
			return inputForSummary, nil
		}

		// Store in cache for future identical content.
		m.cache.Store(cacheKey, cachedSummary{
			summary:   summary,
			expiresAt: time.Now().Add(m.cacheTTL),
		})

		logr.Info("tool response summarized",
			"summary_chars", len(summary),
			"compression_ratio", fmt.Sprintf("%.1f%%", float64(len(summary))/float64(len(responseStr))*100),
			"cache_key", cacheKey[:12],
		)

		// Annotate the summary so downstream consumers know it was compressed.
		annotated := fmt.Sprintf(
			"[Auto-summarized from %d chars to %d chars]\n\n%s",
			len(responseStr), len(summary), summary,
		)
		return annotated, nil
	}
}

// ansiEscapeRe matches ANSI escape sequences (colors, cursor movement, etc.)
// commonly found in terminal/shell output.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// preProcessNonHTML strips ANSI escape codes and collapses runs of whitespace
// from non-HTML content (shell output, JSON blobs, log files). This reduces
// the token count sent to the summarizer without losing semantic content.
// Without this, raw kubectl JSON or colored terminal output wastes tokens
// on escape sequences and repetitive whitespace.
func preProcessNonHTML(content string) string {
	// Strip ANSI escape codes.
	cleaned := ansiEscapeRe.ReplaceAllString(content, "")
	// Collapse runs of whitespace (spaces, tabs) into single spaces.
	// Preserve newlines for structure.
	var sb strings.Builder
	sb.Grow(len(cleaned))
	prevSpace := false
	for _, r := range cleaned {
		if r == ' ' || r == '\t' {
			if !prevSpace {
				sb.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		sb.WriteRune(r)
	}
	return sb.String()
}

// looksLikeHTML checks the first portion of content for common HTML tags.
// Returns true if the content appears to be HTML, false for plain text,
// JSON, or shell output.
func looksLikeHTML(content string) bool {
	// Check the first 500 chars for HTML indicators.
	prefix := content
	if len(prefix) > 500 {
		prefix = prefix[:500]
	}
	lower := strings.ToLower(prefix)
	return strings.Contains(lower, "<html") ||
		strings.Contains(lower, "<body") ||
		strings.Contains(lower, "<div") ||
		strings.Contains(lower, "<!doctype")
}

// contentHash returns a hex-encoded SHA-256 hash of the given content.
// Used as a cache key for content-based deduplication of summarization.
func contentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}
