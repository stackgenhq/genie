// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package docparser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/httputil"
)

const (
	providerNameDocling    = "docling"
	doclingConvertEndpoint = "/v1/convert/file"
	doclingDefaultBaseURL  = "http://localhost:5001"
	doclingRequestTimeout  = 5 * time.Minute
)

// doclingProvider calls the Docling Serve REST API to parse documents.
type doclingProvider struct {
	baseURL string
	client  *http.Client
}

func newDoclingProvider(cfg DoclingConfig) (*doclingProvider, error) {
	base := cfg.BaseURL
	if base == "" {
		base = doclingDefaultBaseURL
	}
	return &doclingProvider{
		baseURL: strings.TrimRight(base, "/"),
		client: &http.Client{
			Timeout:   doclingRequestTimeout,
			Transport: httputil.NewRoundTripper(),
		},
	}, nil
}

// doclingResponse is the JSON structure returned by Docling Serve /v1/convert/file.
type doclingResponse struct {
	Document doclingDocument `json:"document"`
}

type doclingDocument struct {
	Pages []doclingPage `json:"pages"`
	// Fallback: when Docling returns a flat markdown export instead of pages.
	ExportMarkdown string `json:"md_content"`
}

type doclingPage struct {
	PageNo  int            `json:"page_no"`
	Content string         `json:"text"`
	Tables  []doclingTable `json:"tables"`
}

type doclingTable struct {
	Content string `json:"text"`
}

// Parse uploads a file to Docling Serve and converts each page into a NormalizedItem.
func (p *doclingProvider) Parse(ctx context.Context, req ParseRequest) ([]datasource.NormalizedItem, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("files", req.Filename)
	if err != nil {
		return nil, fmt.Errorf("docling: create form file: %w", err)
	}
	if _, err := io.Copy(part, req.Reader); err != nil {
		return nil, fmt.Errorf("docling: copy file content: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("docling: close writer: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+doclingConvertEndpoint, body)
	if err != nil {
		return nil, fmt.Errorf("docling: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("docling: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("docling: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var docResp doclingResponse
	if err := json.NewDecoder(resp.Body).Decode(&docResp); err != nil {
		return nil, fmt.Errorf("docling: decode response: %w", err)
	}

	return p.toNormalizedItems(req, &docResp), nil
}

func (p *doclingProvider) toNormalizedItems(req ParseRequest, resp *doclingResponse) []datasource.NormalizedItem {
	mimeType := DetectMIME(req.Filename)

	// If pages are present, create one item per page.
	if len(resp.Document.Pages) > 0 {
		items := make([]datasource.NormalizedItem, 0, len(resp.Document.Pages))
		for _, page := range resp.Document.Pages {
			content := page.Content
			for _, table := range page.Tables {
				if table.Content != "" {
					content += "\n\n[Table]\n" + table.Content
				}
			}

			elementType := "text"
			if len(page.Tables) > 0 && page.Content == "" {
				elementType = "table"
			}

			items = append(items, datasource.NormalizedItem{
				ID:      fmt.Sprintf("%s:page:%d", req.SourceID, page.PageNo),
				Source:  "docparser",
				Content: content,
				Metadata: map[string]string{
					"element_type": elementType,
					"page_number":  strconv.Itoa(page.PageNo),
					"mime_type":    mimeType,
					"title":        req.Filename,
					"parser":       providerNameDocling,
				},
			})
		}
		return items
	}

	// Fallback: single item from markdown export.
	if resp.Document.ExportMarkdown != "" {
		return []datasource.NormalizedItem{{
			ID:      req.SourceID + ":page:1",
			Source:  "docparser",
			Content: resp.Document.ExportMarkdown,
			Metadata: map[string]string{
				"element_type": "text",
				"page_number":  "1",
				"mime_type":    mimeType,
				"title":        req.Filename,
				"parser":       providerNameDocling,
			},
		}}
	}

	return nil
}
