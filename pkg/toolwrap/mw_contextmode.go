// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package toolwrap – context-mode middleware for local tool-output compression.
package toolwrap

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stackgenhq/genie/pkg/hooks"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/tidwall/gjson"
)

const (
	// defaultContextModeThreshold is the character count above which the
	// context-mode middleware activates. 20 000 chars ≈ 5 000 tokens.
	defaultContextModeThreshold = 20_000

	// defaultMaxChunks is the maximum number of chunks returned when the
	// middleware compresses a large response.
	defaultMaxChunks = 10

	// defaultChunkSize is the target character count per chunk. Chunks are
	// split on paragraph boundaries (double newline) and merged/split to
	// approximate this size.
	defaultChunkSize = 800

	// bm25K1 is the BM25 term-frequency saturation parameter.
	bm25K1 = 1.2
	// bm25B is the BM25 document-length normalisation parameter.
	bm25B = 0.75

	// minQueryTermLen is the minimum character length for a query term
	// to be considered meaningful (filters out "a", "of", etc.).
	minQueryTermLen = 3
)

// ContextModeConfig controls the context-mode middleware.
type ContextModeConfig struct {
	// Enabled activates the context-mode middleware. Default: false (disabled).
	// Set to true to opt in.
	Enabled bool `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	// Threshold is the character count above which a response is compressed.
	// When 0, defaultContextModeThreshold (20 000) is used.
	Threshold int `yaml:"threshold,omitempty" toml:"threshold,omitempty"`
	// MaxChunks is the maximum number of scored chunks to return.
	// When 0, defaultMaxChunks (10) is used.
	MaxChunks int `yaml:"max_chunks,omitempty" toml:"max_chunks,omitempty"`
	// ChunkSize is the target character count per chunk.
	// When 0, defaultChunkSize (800) is used.
	ChunkSize int `yaml:"chunk_size,omitempty" toml:"chunk_size,omitempty"`
	// MinTermLen is the minimum character length for a query term to be
	// considered meaningful. When 0, the package default (3) is used.
	// Lowering this value retains shorter tokens such as 2-character IDs.
	MinTermLen int `yaml:"min_term_len,omitempty" toml:"min_term_len,omitempty"`
	// PerTool maps tool names to per-tool overrides. Non-zero fields in the
	// override replace the global defaults for that tool. This lets operators
	// tune compression aggressiveness per tool — e.g. relax thresholds for
	// run_shell output.
	PerTool map[string]ContextModeToolOverride `yaml:"per_tool,omitempty" toml:"per_tool,omitempty"`
}

// ContextModeToolOverride holds per-tool overrides for the context-mode
// middleware. Only non-zero fields take effect; zero values fall back to
// the global defaults.
type ContextModeToolOverride struct {
	Threshold  int `yaml:"threshold,omitempty" toml:"threshold,omitempty"`
	MaxChunks  int `yaml:"max_chunks,omitempty" toml:"max_chunks,omitempty"`
	ChunkSize  int `yaml:"chunk_size,omitempty" toml:"chunk_size,omitempty"`
	MinTermLen int `yaml:"min_term_len,omitempty" toml:"min_term_len,omitempty"`
}

// contextModeMiddleware compresses oversized tool results using local
// BM25-like chunk scoring — no LLM call required. It sits before
// AutoSummarizeMiddleware in the chain so that a cheap first-pass
// reduction can avoid the slower (and costlier) LLM summarisation.
type contextModeMiddleware struct {
	ContextModeToolOverride
	perTool map[string]ContextModeToolOverride
}

// ContextModeMiddleware returns a Middleware that detects tool responses
// exceeding the configured character threshold and compresses them by
// chunking the text and returning only the top-K most relevant chunks,
// scored against the tool's input arguments using BM25-lite.
//
// This middleware is complementary to AutoSummarizeMiddleware: if context
// mode reduces a 200K response to 15K (below the summarise threshold),
// the LLM summariser is never called — saving both time and API cost.
// Without this middleware, every large tool response would require an
// LLM call for compression.
func ContextModeMiddleware(cfg ContextModeConfig) Middleware {
	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = defaultContextModeThreshold
	}
	maxChunks := cfg.MaxChunks
	if maxChunks <= 0 {
		maxChunks = defaultMaxChunks
	}
	chunkSize := cfg.ChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	minTerm := cfg.MinTermLen
	if minTerm <= 0 {
		minTerm = minQueryTermLen
	}

	return &contextModeMiddleware{
		ContextModeToolOverride: ContextModeToolOverride{
			Threshold:  threshold,
			MaxChunks:  maxChunks,
			ChunkSize:  chunkSize,
			MinTermLen: minTerm,
		},
		perTool: cfg.PerTool,
	}
}

// Wrap implements Middleware. It intercepts the tool response, checks size,
// and compresses if above threshold.
func (m *contextModeMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		output, err := next(ctx, tc)
		if err != nil {
			return output, err
		}

		// Resolve effective settings (per-tool overrides win).
		resolveSettings := m.resolveSettings(tc.ToolName)

		// Adaptive compaction: if a prior iteration flagged this tool
		// for a chunk boost (compaction miss), increase MaxChunks.
		if tracker, ok := ctx.Value(hooks.CompactionTrackerKey).(hooks.CompactionTracker); ok {
			if boost := tracker.GetChunkBoost(tc.ToolName); boost > 1 {
				resolveSettings.MaxChunks *= boost
				logr := logger.GetLogger(ctx).With(
					"fn", "ContextModeMiddleware",
					"tool", tc.ToolName,
				)
				logr.Info("adaptive compaction: boosted max_chunks", "boost", boost, "effective_max_chunks", resolveSettings.MaxChunks)
			}
		}

		responseStr := fmt.Sprintf("%v", output)
		if len(responseStr) < resolveSettings.Threshold {
			return output, nil
		}

		logr := logger.GetLogger(ctx).With(
			"fn", "ContextModeMiddleware",
			"tool", tc.ToolName,
			"original_chars", len(responseStr),
			"threshold", resolveSettings.Threshold,
		)
		logr.Info("tool response exceeds threshold, applying context-mode compression")

		// Step 1: Chunk the text on paragraph boundaries.
		chunks := chunkText(responseStr, resolveSettings.ChunkSize)
		if len(chunks) <= resolveSettings.MaxChunks {
			// Not enough chunks to warrant scoring — return as-is.
			return output, nil
		}

		// Step 2: Extract query terms from the tool's input arguments.
		queryTerms := extractQueryTerms(tc.Args, resolveSettings.MinTermLen)
		if len(queryTerms) == 0 {
			// No searchable terms — fall back to returning first + last
			// chunks to preserve context boundaries.
			logr.Info("no query terms extracted, returning boundary chunks")
			return buildBoundaryResult(chunks, resolveSettings.MaxChunks, len(responseStr)), nil
		}

		// Step 3: Score chunks using BM25-lite with positional bias.
		scored := chunks.scoreChunks(queryTerms)

		// Step 4: Select top-K by score, then re-order by original position.
		topK := scored.selectTopK(resolveSettings.MaxChunks)

		compressed := buildResult(topK, len(chunks), len(responseStr))
		logr.Info("context-mode compression complete",
			"compressed_chars", len(compressed),
			"chunks_kept", len(topK),
			"chunks_total", len(chunks),
			"compression_ratio", fmt.Sprintf("%.1f%%", float64(len(compressed))/float64(len(responseStr))*100),
		)

		if tracker, ok := ctx.Value(hooks.CompactionTrackerKey).(hooks.CompactionTracker); ok {
			tracker.MarkCompressed(tc.ToolName, len(responseStr), len(compressed))
		}

		return compressed, nil
	}
}

func (c ContextModeToolOverride) clone() ContextModeToolOverride {
	return ContextModeToolOverride{
		Threshold:  c.Threshold,
		MaxChunks:  c.MaxChunks,
		ChunkSize:  c.ChunkSize,
		MinTermLen: c.MinTermLen,
	}
}

// resolveSettings returns the effective (threshold, maxChunks, chunkSize,
// minTermLen) for the given tool, applying per-tool overrides where set.
func (m *contextModeMiddleware) resolveSettings(toolName string) ContextModeToolOverride {
	override := m.clone()

	result, ok := m.perTool[toolName]
	if !ok {
		return override
	}

	if result.Threshold > 0 {
		override.Threshold = result.Threshold
	}
	if result.MaxChunks > 0 {
		override.MaxChunks = result.MaxChunks
	}
	if result.ChunkSize > 0 {
		override.ChunkSize = result.ChunkSize
	}
	if result.MinTermLen > 0 {
		override.MinTermLen = result.MinTermLen
	}

	return override
}

// --- Internal helpers (unexported) ---

// scoredChunk pairs a text chunk with its BM25 score and original index.
type scoredChunk struct {
	text  string
	score float64
	index int
}

type scoredChunks []scoredChunk

type chunkedStrings []string

// chunkText splits text into chunks of approximately targetSize characters,
// preferring paragraph boundaries (double newline). If a paragraph exceeds
// targetSize it is split on sentence boundaries; if a sentence still exceeds
// targetSize it is split on word boundaries.
func chunkText(text string, targetSize int) chunkedStrings {
	// Split on paragraph boundaries first.
	paragraphs := splitOnBoundary(text, "\n\n")

	var chunks chunkedStrings
	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		if len(para) <= targetSize {
			chunks = append(chunks, para)
			continue
		}

		// Paragraph too large — split on sentence boundaries.
		sentences := splitOnSentences(para)
		var current strings.Builder
		for _, sent := range sentences {
			sent = strings.TrimSpace(sent)
			if sent == "" {
				continue
			}
			// If a single sentence exceeds targetSize, split on words.
			if len(sent) > targetSize {
				// Flush anything accumulated so far.
				if current.Len() > 0 {
					chunks = append(chunks, strings.TrimSpace(current.String()))
					current.Reset()
				}
				chunks = append(chunks, splitOnWords(sent, targetSize)...)
				continue
			}
			if current.Len()+len(sent) > targetSize && current.Len() > 0 {
				chunks = append(chunks, strings.TrimSpace(current.String()))
				current.Reset()
			}
			if current.Len() > 0 {
				current.WriteByte(' ')
			}
			current.WriteString(sent)
		}
		if current.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(current.String()))
		}
	}

	return chunks
}

// sentenceSplitter matches sentence-ending punctuation followed by whitespace.
var sentenceSplitter = regexp.MustCompile(`([.!?])\s+`)

// splitOnSentences splits text into sentences using punctuation heuristics.
func splitOnSentences(text string) []string {
	indices := sentenceSplitter.FindAllStringIndex(text, -1)
	if len(indices) == 0 {
		return []string{text}
	}

	var sentences []string
	prev := 0
	for _, idx := range indices {
		// Include the punctuation mark in the sentence.
		end := idx[0] + 1
		sentences = append(sentences, text[prev:end])
		prev = idx[1]
	}
	if prev < len(text) {
		sentences = append(sentences, text[prev:])
	}
	return sentences
}

// splitOnBoundary splits text on a boundary string, returning non-empty parts.
func splitOnBoundary(text, boundary string) []string {
	return strings.Split(text, boundary)
}

// splitOnWords splits text that exceeds targetSize into sub-chunks on word
// boundaries. Each sub-chunk is at most targetSize characters (unless a
// single word exceeds targetSize, in which case it becomes its own chunk).
func splitOnWords(text string, targetSize int) chunkedStrings {
	words := strings.Fields(text)
	var chunks chunkedStrings
	var current strings.Builder
	for _, w := range words {
		if current.Len()+1+len(w) > targetSize && current.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(w)
	}
	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}
	return chunks
}

// hexPattern matches hex-like substrings that are at least 4 hex
// characters long. This catches pod ID prefixes (e.g. bc97d0), commit
// SHAs, container IDs, and similar infrastructure identifiers that
// would otherwise be lost by the word tokeniser.
var hexPattern = regexp.MustCompile(`[0-9a-fA-F]{4,}`)

// extractQueryTerms pulls searchable terms from the JSON tool arguments.
// It extracts string values from the JSON, tokenises them, lowercases,
// and filters out very short tokens (< minLen chars). It also extracts
// hex-like substrings separately so that infrastructure identifiers
// (pod IDs, commit SHAs) are not lost during tokenisation.
func extractQueryTerms(args []byte, minLen int) []string {
	if len(args) == 0 {
		return nil
	}
	if minLen <= 0 {
		minLen = minQueryTermLen
	}

	parsed := gjson.ParseBytes(args)
	if !parsed.IsObject() && !parsed.IsArray() {
		return nil
	}

	var rawTerms []string
	parsed.ForEach(func(_, value gjson.Result) bool {
		if value.Type == gjson.String {
			s := value.String()
			rawTerms = append(rawTerms, tokenise(s, minLen)...)
			// Also extract hex-like substrings for infrastructure IDs.
			rawTerms = append(rawTerms, extractHexTokens(s)...)
		}
		return true
	})

	// Deduplicate.
	seen := make(map[string]struct{}, len(rawTerms))
	var terms []string
	for _, t := range rawTerms {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		terms = append(terms, t)
	}
	return terms
}

// extractHexTokens returns all hex-like substrings (≥4 hex chars) found
// in s, lowercased. These capture pod IDs, commit SHAs, container IDs,
// and similar identifiers that the standard word tokeniser may miss or
// merge with surrounding text.
func extractHexTokens(s string) []string {
	matches := hexPattern.FindAllString(s, -1)
	if len(matches) == 0 {
		return nil
	}
	tokens := make([]string, 0, len(matches))
	for _, m := range matches {
		tokens = append(tokens, strings.ToLower(m))
	}
	return tokens
}

// tokenise splits a string into lowercase tokens, filtering out tokens
// shorter than minLen characters.
func tokenise(s string, minLen int) []string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var tokens []string
	for _, w := range words {
		w = strings.ToLower(w)
		if utf8.RuneCountInString(w) >= minLen {
			tokens = append(tokens, w)
		}
	}
	return tokens
}

// scoreChunks scores each chunk against the query terms using BM25-lite.
func (chunks chunkedStrings) scoreChunks(queryTerms []string) scoredChunks {
	n := len(chunks)

	// Precompute lowercase chunks to avoid repeated ToLower calls.
	lowerChunks := make([]string, n)
	totalLen := 0
	for i, chunk := range chunks {
		lowerChunks[i] = strings.ToLower(chunk)
		totalLen += len(chunk)
	}

	// Pre-compute document frequency for each query term.
	df := make(map[string]int, len(queryTerms))
	for _, term := range queryTerms {
		for _, lc := range lowerChunks {
			if strings.Contains(lc, term) {
				df[term]++
			}
		}
	}

	// Compute average chunk length.
	avgLen := float64(totalLen) / float64(n)

	// Score each chunk with BM25 + positional bias.
	// Positional bias gives a small boost to chunks near the beginning
	// and end of the document so that output headers and trailing
	// summaries ("no resources found", status lines) are preserved.
	scored := make([]scoredChunk, n)
	for i, chunk := range chunks {
		lowerChunk := lowerChunks[i]
		chunkLen := float64(len(chunk))
		score := 0.0

		for _, term := range queryTerms {
			// Term frequency: count occurrences in chunk.
			tf := float64(strings.Count(lowerChunk, term))
			if tf == 0 {
				continue
			}

			// IDF: log(N / df).
			docFreq := float64(df[term])
			idf := math.Log(float64(n) / docFreq)

			// BM25 score for this term.
			numerator := tf * (bm25K1 + 1)
			denominator := tf + bm25K1*(1-bm25B+bm25B*(chunkLen/avgLen))
			score += idf * (numerator / denominator)
		}

		// Positional bias: boost first and last 10% of chunks.
		// Use index-based window (normalized against n-1) so the very
		// last chunk always receives the maximum tail boost.
		lastIdx := float64(n - 1)
		headWindow := math.Ceil(0.1 * float64(n))
		tailStart := float64(n) - math.Ceil(0.1*float64(n))
		pos := float64(i)
		if pos < headWindow {
			score += 0.1 * (1 - pos/headWindow) // decays from 0.1→0
		}
		if lastIdx > 0 && pos >= tailStart {
			tailRange := lastIdx - tailStart
			if tailRange > 0 {
				score += 0.1 * ((pos - tailStart) / tailRange) // grows 0→0.1
			} else {
				score += 0.1 // single-element tail window gets full boost
			}
		}

		scored[i] = scoredChunk{
			text:  chunk,
			score: score,
			index: i,
		}
	}

	return scored
}

// selectTopK selects the k highest-scoring chunks and returns them
// sorted by their original position in the document.
func (scored scoredChunks) selectTopK(k int) []scoredChunk {
	if len(scored) <= k {
		return scored
	}

	// Sort by score descending.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	topK := scored[:k]

	// Re-sort by original position to preserve reading order.
	sort.Slice(topK, func(i, j int) bool {
		return topK[i].index < topK[j].index
	})

	return topK
}

// buildResult assembles the compressed output with an annotation header.
func buildResult(topK []scoredChunk, totalChunks, originalChars int) string {
	var b strings.Builder
	fmt.Fprintf(&b,
		"[Context Mode: compressed from %d chars to showing %d of %d chunks]\n\n",
		originalChars, len(topK), totalChunks,
	)
	for i, sc := range topK {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		b.WriteString(sc.text)
	}
	return b.String()
}

// buildBoundaryResult returns the first and last chunks when no query
// terms are available for scoring. This preserves context boundaries
// (headers, conclusions) which are typically the most informative parts.
func buildBoundaryResult(chunks []string, maxChunks, originalChars int) string {
	n := len(chunks)
	half := maxChunks / 2
	if half < 1 {
		half = 1
	}

	var selected []string
	// First half from the beginning.
	for i := 0; i < half && i < n; i++ {
		selected = append(selected, chunks[i])
	}
	// Second half from the end.
	startEnd := n - (maxChunks - half)
	if startEnd < half {
		startEnd = half
	}
	for i := startEnd; i < n; i++ {
		selected = append(selected, chunks[i])
	}

	var b strings.Builder
	fmt.Fprintf(&b,
		"[Context Mode: compressed from %d chars, showing %d of %d chunks (boundary selection — no query terms)]\n\n",
		originalChars, len(selected), n,
	)
	for i, s := range selected {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		b.WriteString(s)
	}
	return b.String()
}
