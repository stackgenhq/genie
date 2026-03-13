// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package docparser

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"google.golang.org/genai"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
)

const (
	providerNameGemini = "gemini"
	defaultGeminiModel = "gemini-2.0-flash"
)

// geminiProvider uses the Gemini API to parse documents by uploading the file
// and prompting for structured text extraction.
type geminiProvider struct {
	client *genai.Client
	model  string
}

func newGeminiProvider(ctx context.Context, cfg GeminiConfig, sp security.SecretProvider) (*geminiProvider, error) {
	apiKey, _ := sp.GetSecret(ctx, security.GetSecretRequest{
		Name:   "GEMINI_API_KEY",
		Reason: toolcontext.GetJustification(ctx),
	})
	if apiKey == "" {
		// Fallback to GOOGLE_API_KEY.
		apiKey, _ = sp.GetSecret(ctx, security.GetSecretRequest{
			Name:   "GOOGLE_API_KEY",
			Reason: toolcontext.GetJustification(ctx),
		})
	}
	if apiKey == "" {
		return nil, fmt.Errorf("docparser/gemini: GEMINI_API_KEY or GOOGLE_API_KEY is required")
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("docparser/gemini: create client: %w", err)
	}

	model := cfg.Model
	if model == "" {
		model = defaultGeminiModel
	}

	return &geminiProvider{
		client: client,
		model:  model,
	}, nil
}

// geminiExtractionPrompt is the system prompt for document extraction.
const geminiExtractionPrompt = `You are a document parser. Extract the text content from the uploaded document.

Rules:
1. Preserve the document structure: headings, paragraphs, lists, tables.
2. Format tables as markdown tables.
3. Separate each page with "--- PAGE N ---" markers.
4. Do NOT summarize or paraphrase — extract the original text verbatim.
5. For images or charts, describe them briefly in [Image: ...] or [Chart: ...] markers.
6. Output only the extracted content, no preamble.`

// Parse uploads a file to Gemini and extracts structured text per page.
func (p *geminiProvider) Parse(ctx context.Context, req ParseRequest) ([]datasource.NormalizedItem, error) {
	data, err := io.ReadAll(req.Reader)
	if err != nil {
		return nil, fmt.Errorf("docparser/gemini: read file: %w", err)
	}

	mimeType := DetectMIME(req.Filename)

	result, err := p.client.Models.GenerateContent(ctx, p.model, []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{
			genai.NewPartFromBytes(data, mimeType),
			genai.NewPartFromText(geminiExtractionPrompt),
		}, genai.RoleUser),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("docparser/gemini: generate content: %w", err)
	}

	text := extractGeminiText(result)
	if text == "" {
		return nil, nil
	}

	return splitPages(text, req, mimeType, providerNameGemini), nil
}

// extractGeminiText extracts the text content from a Gemini GenerateContentResponse.
func extractGeminiText(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.Text != "" {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}

// splitPages splits extracted text on "--- PAGE N ---" markers into separate NormalizedItems.
// If no page markers are found, returns a single item.
func splitPages(text string, req ParseRequest, mimeType, parser string) []datasource.NormalizedItem {
	// Try to split on page markers.
	pages := SplitOnPageMarkers(text)

	items := make([]datasource.NormalizedItem, 0, len(pages))
	for i, pageContent := range pages {
		pageContent = strings.TrimSpace(pageContent)
		if pageContent == "" {
			continue
		}
		pageNum := i + 1
		items = append(items, datasource.NormalizedItem{
			ID:      fmt.Sprintf("%s:page:%d", req.SourceID, pageNum),
			Source:  "docparser",
			Content: pageContent,
			Metadata: map[string]string{
				"element_type": "text",
				"page_number":  strconv.Itoa(pageNum),
				"mime_type":    mimeType,
				"title":        req.Filename,
				"parser":       parser,
			},
		})
	}
	return items
}

// SplitOnPageMarkers splits text on "--- PAGE N ---" markers.
// Returns at least one element (the whole text) if no markers found.
func SplitOnPageMarkers(text string) []string {
	// Look for "--- PAGE" pattern (case-insensitive).
	lines := strings.Split(text, "\n")
	var pages []string
	var current strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "--- PAGE") && strings.HasSuffix(upper, "---") {
			if current.Len() > 0 {
				pages = append(pages, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	if current.Len() > 0 {
		pages = append(pages, current.String())
	}

	if len(pages) == 0 {
		return []string{text}
	}
	return pages
}
