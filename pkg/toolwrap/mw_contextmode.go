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
	// Disabled deactivates the context-mode middleware. Default: false (enabled).
	Disabled bool `yaml:"disabled,omitempty" toml:"disabled,omitempty"`
	// Threshold is the character count above which a response is compressed.
	// When 0, defaultContextModeThreshold (20 000) is used.
	Threshold int `yaml:"threshold,omitempty" toml:"threshold,omitempty"`
	// MaxChunks is the maximum number of scored chunks to return.
	// When 0, defaultMaxChunks (10) is used.
	MaxChunks int `yaml:"max_chunks,omitempty" toml:"max_chunks,omitempty"`
	// ChunkSize is the target character count per chunk.
	// When 0, defaultChunkSize (800) is used.
	ChunkSize int `yaml:"chunk_size,omitempty" toml:"chunk_size,omitempty"`
}

// contextModeMiddleware compresses oversized tool results using local
// BM25-like chunk scoring — no LLM call required. It sits before
// AutoSummarizeMiddleware in the chain so that a cheap first-pass
// reduction can avoid the slower (and costlier) LLM summarisation.
type contextModeMiddleware struct {
	threshold int
	maxChunks int
	chunkSize int
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

	return &contextModeMiddleware{
		threshold: threshold,
		maxChunks: maxChunks,
		chunkSize: chunkSize,
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

		responseStr := fmt.Sprintf("%v", output)
		if len(responseStr) < m.threshold {
			return output, nil
		}

		logr := logger.GetLogger(ctx).With(
			"fn", "ContextModeMiddleware",
			"tool", tc.ToolName,
			"original_chars", len(responseStr),
			"threshold", m.threshold,
		)
		logr.Info("tool response exceeds threshold, applying context-mode compression")

		// Step 1: Chunk the text on paragraph boundaries.
		chunks := chunkText(responseStr, m.chunkSize)
		if len(chunks) <= m.maxChunks {
			// Not enough chunks to warrant scoring — return as-is.
			return output, nil
		}

		// Step 2: Extract query terms from the tool's input arguments.
		queryTerms := extractQueryTerms(tc.Args)
		if len(queryTerms) == 0 {
			// No searchable terms — fall back to returning first + last
			// chunks to preserve context boundaries.
			logr.Info("no query terms extracted, returning boundary chunks")
			return buildBoundaryResult(chunks, m.maxChunks, len(responseStr)), nil
		}

		// Step 3: Score chunks using BM25-lite.
		scored := chunks.scoreChunks(queryTerms)

		// Step 4: Select top-K by score, then re-order by original position.
		topK := scored.selectTopK(m.maxChunks)

		compressed := buildResult(topK, len(chunks), len(responseStr))
		logr.Info("context-mode compression complete",
			"compressed_chars", len(compressed),
			"chunks_kept", len(topK),
			"chunks_total", len(chunks),
			"compression_ratio", fmt.Sprintf("%.1f%%", float64(len(compressed))/float64(len(responseStr))*100),
		)

		return compressed, nil
	}
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
// targetSize it is split on sentence boundaries; if still too large, on
// word boundaries.
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

// extractQueryTerms pulls searchable terms from the JSON tool arguments.
// It extracts string values from the JSON, tokenises them, lowercases,
// and filters out very short tokens (< minQueryTermLen chars).
func extractQueryTerms(args []byte) []string {
	if len(args) == 0 {
		return nil
	}

	parsed := gjson.ParseBytes(args)
	if !parsed.IsObject() && !parsed.IsArray() {
		return nil
	}

	var rawTerms []string
	parsed.ForEach(func(_, value gjson.Result) bool {
		if value.Type == gjson.String {
			rawTerms = append(rawTerms, tokenise(value.String())...)
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

// tokenise splits a string into lowercase tokens, filtering out short ones.
func tokenise(s string) []string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var tokens []string
	for _, w := range words {
		w = strings.ToLower(w)
		if len(w) >= minQueryTermLen {
			tokens = append(tokens, w)
		}
	}
	return tokens
}

// scoreChunks scores each chunk against the query terms using BM25-lite.
func (chunks chunkedStrings) scoreChunks(queryTerms []string) scoredChunks {
	n := len(chunks)

	// Pre-compute document frequency for each query term.
	df := make(map[string]int, len(queryTerms))
	for _, term := range queryTerms {
		for _, chunk := range chunks {
			if strings.Contains(strings.ToLower(chunk), term) {
				df[term]++
			}
		}
	}

	// Compute average chunk length.
	totalLen := 0
	for _, chunk := range chunks {
		totalLen += len(chunk)
	}
	avgLen := float64(totalLen) / float64(n)

	// Score each chunk.
	scored := make([]scoredChunk, n)
	for i, chunk := range chunks {
		lowerChunk := strings.ToLower(chunk)
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
	b.WriteString(fmt.Sprintf(
		"[Context Mode: compressed from %d chars to showing %d of %d chunks]\n\n",
		originalChars, len(topK), totalChunks,
	))
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
	b.WriteString(fmt.Sprintf(
		"[Context Mode: compressed from %d chars, showing %d of %d chunks (boundary selection — no query terms)]\n\n",
		originalChars, len(selected), n,
	))
	for i, s := range selected {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		b.WriteString(s)
	}
	return b.String()
}
