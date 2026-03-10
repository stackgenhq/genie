// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package ocrtool provides optical character recognition (OCR) tools for
// agents. It extracts text from images (screenshots, photos, scanned
// documents, whiteboard captures) using the Tesseract OCR engine.
//
// Problem: Agents receive image attachments containing text (screenshots
// of error messages, photos of whiteboards, scanned receipts) that they
// cannot read natively. This tool converts images to machine-readable
// text so agents can process, search, and reason about visual content.
//
// Supported image formats:
//   - PNG, JPEG/JPG, TIFF, BMP, WebP, PNM
//
// Safety guards:
//   - File size capped at 20 MB to prevent memory exhaustion
//   - 60-second processing timeout
//   - Output truncated at 32 KB to limit LLM context consumption
//
// Dependencies:
//   - tesseract CLI — the industry-standard OCR engine (Apache 2.0)
//   - Install on macOS: brew install tesseract
//   - Install on Debian: apt-get install tesseract-ocr
//   - Additional languages: brew install tesseract-lang
package ocrtool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/osutils"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	maxOutputBytes = 32 << 10         // 32 KB
	maxFileSize    = 20 << 20         // 20 MB
	ocrTimeout     = 60 * time.Second // per-image timeout
)

// supportedExtensions lists image formats tesseract can process.
var supportedExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true,
	".tiff": true, ".tif": true, ".bmp": true,
	".webp": true, ".pnm": true,
}

// ────────────────────── Request / Response ──────────────────────

type ocrRequest struct {
	ImagePath string `json:"image_path" jsonschema:"description=Path to the image file to OCR."`
	Language  string `json:"language,omitempty" jsonschema:"description=Tesseract language code (e.g. eng, fra, deu, jpn). Defaults to eng."`
	PSM       int    `json:"psm,omitempty" jsonschema:"description=Page segmentation mode. 3=fully automatic (default), 6=assume uniform block of text, 11=sparse text, 13=raw line."`
}

// validate checks required fields, file existence, size, and format.
func (r ocrRequest) validate() error {
	if r.ImagePath == "" {
		return fmt.Errorf("image_path is required")
	}

	info, err := os.Stat(r.ImagePath)
	if err != nil {
		return fmt.Errorf("image not found: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not an image file")
	}
	if info.Size() > maxFileSize {
		return fmt.Errorf("image too large (%d bytes, max %d)", info.Size(), maxFileSize)
	}

	ext := strings.ToLower(filepath.Ext(r.ImagePath))
	if !supportedExtensions[ext] {
		return fmt.Errorf("unsupported image format %q: supported formats are PNG, JPEG, TIFF/TIF, BMP, WebP, and PNM", ext)
	}
	return nil
}

// lang returns the resolved language code (defaults to "eng").
func (r ocrRequest) lang() string {
	if l := strings.TrimSpace(r.Language); l != "" {
		return l
	}
	return "eng"
}

// psm returns the resolved page segmentation mode (defaults to 3).
func (r ocrRequest) psm() int {
	if r.PSM != 0 {
		return r.PSM
	}
	return 3
}

type ocrResponse struct {
	ImagePath string `json:"image_path"`
	Text      string `json:"text"`
	CharCount int    `json:"char_count"`
	WordCount int    `json:"word_count"`
	Language  string `json:"language"`
	Message   string `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type ocrTools struct{}

func canBootstrap() error {
	return osutils.ValidateToolAvailability("tesseract", map[string]string{"brew": "tesseract", "apt": "tesseract-ocr"})
}

func newOCRTools() *ocrTools { return &ocrTools{} }

func (o *ocrTools) ocrTool() tool.CallableTool {
	return function.NewFunctionTool(
		o.extractText,
		function.WithName("ocr_extract_text"),
		function.WithDescription(
			"Extract text from an image using OCR (optical character recognition). "+
				"Supports PNG, JPEG, TIFF/TIF, BMP, WebP, and PNM images. "+
				"Use this to read text from screenshots, photos of documents, "+
				"whiteboard captures, or any image containing text. "+
				"Requires tesseract to be installed on the system.",
		),
	)
}

// ────────────────────── Implementation ──────────────────────

func (o *ocrTools) extractText(ctx context.Context, req ocrRequest) (ocrResponse, error) {
	resp := ocrResponse{
		ImagePath: req.ImagePath,
		Language:  req.lang(),
	}

	if err := req.validate(); err != nil {
		return resp, err
	}

	if err := canBootstrap(); err != nil {
		return resp, err
	}

	ctx, cancel := context.WithTimeout(ctx, ocrTimeout)
	defer cancel()

	args := []string{req.ImagePath, "stdout", "-l", resp.Language, "--psm", fmt.Sprintf("%d", req.psm())}
	cmd := exec.CommandContext(ctx, "tesseract", args...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return resp, fmt.Errorf("OCR timed out after %v: %w", ocrTimeout, ctx.Err())
		}
		return resp, fmt.Errorf("tesseract failed: %w", err)
	}

	text := strings.TrimSpace(string(out))
	if len(text) > maxOutputBytes {
		text = text[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
	}

	resp.Text = text
	resp.CharCount = len(text)
	resp.WordCount = len(strings.Fields(text))
	resp.Message = fmt.Sprintf("Extracted %d characters (%d words) from %s", resp.CharCount, resp.WordCount, filepath.Base(req.ImagePath))
	return resp, nil
}
