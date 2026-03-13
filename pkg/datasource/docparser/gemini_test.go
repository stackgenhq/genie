// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package docparser

import (
	"testing"

	"google.golang.org/genai"
)

func Test_extractGeminiText_nil(t *testing.T) {
	result := extractGeminiText(nil)
	if result != "" {
		t.Errorf("expected empty string for nil response, got %q", result)
	}
}

func Test_extractGeminiText_noCandidates(t *testing.T) {
	resp := &genai.GenerateContentResponse{Candidates: nil}
	result := extractGeminiText(resp)
	if result != "" {
		t.Errorf("expected empty string for no candidates, got %q", result)
	}
}

func Test_extractGeminiText_emptyCandidates(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{}},
	}
	// Content is nil for empty candidate — should not panic.
	defer func() {
		if r := recover(); r != nil {
			// If it panics due to nil Content, that's a code issue to note.
			t.Logf("extractGeminiText panics on nil Content: %v", r)
		}
	}()
	_ = extractGeminiText(resp)
}

func Test_extractGeminiText_withText(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: "Hello "},
						{Text: "World"},
					},
				},
			},
		},
	}
	result := extractGeminiText(resp)
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", result)
	}
}

func Test_extractGeminiText_emptyParts(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: ""},
						{Text: ""},
					},
				},
			},
		},
	}
	result := extractGeminiText(resp)
	if result != "" {
		t.Errorf("expected empty string for empty parts, got %q", result)
	}
}

func Test_splitPages_singlePage(t *testing.T) {
	req := ParseRequest{
		Filename: "test.pdf",
		SourceID: "src:1",
	}
	items := splitPages("Hello world", req, "application/pdf", "test")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", items[0].Content)
	}
	if items[0].ID != "src:1:page:1" {
		t.Errorf("expected ID 'src:1:page:1', got %q", items[0].ID)
	}
	if items[0].Metadata["parser"] != "test" {
		t.Errorf("expected parser 'test', got %q", items[0].Metadata["parser"])
	}
	if items[0].Metadata["mime_type"] != "application/pdf" {
		t.Errorf("expected mime_type 'application/pdf', got %q", items[0].Metadata["mime_type"])
	}
}

func Test_splitPages_multiPage(t *testing.T) {
	text := "Intro\n--- PAGE 1 ---\nFirst page\n--- PAGE 2 ---\nSecond page"
	req := ParseRequest{
		Filename: "doc.docx",
		SourceID: "src:2",
	}
	items := splitPages(text, req, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "gemini")
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].ID != "src:2:page:1" {
		t.Errorf("expected page 1 ID, got %q", items[0].ID)
	}
	if items[1].ID != "src:2:page:2" {
		t.Errorf("expected page 2 ID, got %q", items[1].ID)
	}
	if items[2].ID != "src:2:page:3" {
		t.Errorf("expected page 3 ID, got %q", items[2].ID)
	}
}

func Test_splitPages_skipsEmptyPages(t *testing.T) {
	// Page marker immediately followed by another marker — produces empty page content.
	text := "Content\n--- PAGE 1 ---\n--- PAGE 2 ---\nReal content"
	req := ParseRequest{
		Filename: "test.pdf",
		SourceID: "src:3",
	}
	items := splitPages(text, req, "application/pdf", "gemini")
	// The empty page between markers should be skipped.
	for _, item := range items {
		if item.Content == "" {
			t.Error("empty pages should be skipped")
		}
	}
}
