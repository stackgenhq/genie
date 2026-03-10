// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package doctool provides document parsing tools for agents. It extracts
// text content from common file formats so agents can process file
// attachments received through messenger platforms.
//
// Problem: Users send files (PDFs, Word docs, CSVs, config files) that
// agents need to read and understand. Without this tool, file content
// would be opaque to the agent. This package converts documents into
// plain text that fits within the LLM context window.
//
// Supported formats:
//   - .txt, .md, .json, .yaml, .yml, .xml, .log — read as-is
//   - .csv — parsed into a readable markdown table
//   - .docx — extracted via ZIP + XML parsing (no external dependencies)
//   - .pdf — multi-strategy extraction:
//     1. pdftotext (poppler-utils) — fast, handles forms and text PDFs
//     2. Tesseract OCR — handles scanned/image-based PDFs
//     3. dslipak/pdf (pure Go) — fallback when system tools are unavailable
//
// Safety guards:
//   - File size capped at 10 MB
//   - 30-second parse timeout (prevents hung I/O on network mounts or complex PDFs)
//   - PDF parsing runs in a goroutine with context deadline (dslipak/pdf ignores context)
//   - Output truncated at 32 KB to limit LLM context consumption
//
// Dependencies:
//   - github.com/dslipak/pdf — pure-Go PDF reader (fallback, no system deps)
//   - pdftotext (poppler-utils) — optional, for reliable text PDF extraction
//   - pdftoppm (poppler-utils) — optional, for PDF-to-image conversion (OCR pipeline)
//   - tesseract — optional, for OCR on scanned/image PDFs
//   - Install on macOS: brew install poppler tesseract
//   - Install on Debian: apt-get install poppler-utils tesseract-ocr
package doctool

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/osutils"

	pdfread "github.com/dslipak/pdf"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	maxFileSize    = 10 << 20 // 10 MB
	maxOutputBytes = 32 << 10 // 32 KB — cap tool output to limit LLM context usage
)

// ────────────────────── Request / Response ──────────────────────

type docRequest struct {
	FilePath string `json:"file_path" jsonschema:"description=Absolute or relative path to the document file to parse."`
	Format   string `json:"format,omitempty" jsonschema:"description=Override file format detection. One of: text, csv, docx, pdf. If empty, the format is auto-detected from the file extension."`
}

type docResponse struct {
	FilePath  string `json:"file_path"`
	Format    string `json:"format"`
	Content   string `json:"content"`
	PageCount int    `json:"page_count,omitempty"`
	LineCount int    `json:"line_count,omitempty"`
	CharCount int    `json:"char_count"`
	Message   string `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type docTools struct{}

func newDocTools() *docTools { return &docTools{} }

func (d *docTools) docParseTool() tool.CallableTool {
	return function.NewFunctionTool(
		d.parse,
		function.WithName("parse_document"),
		function.WithDescription(
			"Parse and extract text content from a document file. "+
				"Supported formats: PDF (.pdf), Word documents (.docx), "+
				"CSV files (.csv), and text-based files (.txt, .md, .json, .yaml, .xml, .log). "+
				"Returns the extracted text content along with metadata like page count and character count.",
		),
	)
}

// ────────────────────── Implementation ──────────────────────

func (d *docTools) parse(ctx context.Context, req docRequest) (docResponse, error) {
	resp := docResponse{FilePath: req.FilePath}

	if req.FilePath == "" {
		return resp, fmt.Errorf("file_path is required")
	}

	// Guard against hung file I/O (e.g. network-mounted paths).
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Verify file exists and check size.
	info, err := os.Stat(req.FilePath)
	if err != nil {
		return resp, fmt.Errorf("cannot access file %q: %w", req.FilePath, err)
	}
	if info.IsDir() {
		return resp, fmt.Errorf("%q is a directory, not a file", req.FilePath)
	}
	if info.Size() > maxFileSize {
		return resp, fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), maxFileSize)
	}

	// Detect format from extension or override.
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = d.detectFormat(req.FilePath)
	}
	resp.Format = format

	// Check context before starting potentially expensive I/O.
	if err := ctx.Err(); err != nil {
		return resp, fmt.Errorf("parsing cancelled: %w", err)
	}

	switch format {
	case "text":
		return d.parseText(req.FilePath, resp)
	case "csv":
		return d.parseCSV(req.FilePath, resp)
	case "docx":
		return d.parseDOCX(req.FilePath, resp)
	case "pdf":
		return d.parsePDF(ctx, req.FilePath, resp)
	default:
		// Try reading as text for unknown formats.
		return d.parseText(req.FilePath, resp)
	}
}

func (d *docTools) detectFormat(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pdf":
		return "pdf"
	case ".docx":
		return "docx"
	case ".csv":
		return "csv"
	case ".txt", ".md", ".json", ".yaml", ".yml", ".xml",
		".log", ".toml", ".ini", ".cfg", ".conf",
		".go", ".py", ".js", ".ts", ".java", ".rb", ".rs",
		".sh", ".bash", ".zsh", ".html", ".htm", ".css":
		return "text"
	default:
		return "text"
	}
}

// parseText reads a file as plain text.
func (d *docTools) parseText(path string, resp docResponse) (docResponse, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return resp, fmt.Errorf("failed to read file: %w", err)
	}
	content := string(data)
	if len(content) > maxOutputBytes {
		content = content[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
	}
	resp.Content = content
	resp.CharCount = len(content)
	resp.LineCount = strings.Count(content, "\n") + 1
	resp.Message = fmt.Sprintf("Read %d characters, %d lines from %s", resp.CharCount, resp.LineCount, filepath.Base(path))
	return resp, nil
}

// parseCSV reads a CSV file and formats it as a readable table.
func (d *docTools) parseCSV(path string, resp docResponse) (docResponse, error) {
	f, err := os.Open(path)
	if err != nil {
		return resp, fmt.Errorf("failed to open CSV: %w", err)
	}
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return resp, fmt.Errorf("failed to parse CSV: %w", err)
	}

	if len(records) == 0 {
		resp.Content = "(empty CSV file)"
		resp.Message = "Empty CSV file"
		return resp, nil
	}

	var sb strings.Builder

	// Header row.
	if len(records) > 0 {
		sb.WriteString("| " + strings.Join(records[0], " | ") + " |\n")
		sb.WriteString("|" + strings.Repeat(" --- |", len(records[0])) + "\n")
	}

	// Data rows.
	for i := 1; i < len(records); i++ {
		sb.WriteString("| " + strings.Join(records[i], " | ") + " |\n")
	}

	resp.Content = sb.String()
	if len(resp.Content) > maxOutputBytes {
		resp.Content = resp.Content[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
	}
	resp.LineCount = len(records)
	resp.CharCount = len(resp.Content)
	resp.Message = fmt.Sprintf("Parsed CSV: %d rows, %d columns", len(records), len(records[0]))
	return resp, nil
}

// parseDOCX extracts text from a .docx file using ZIP + XML parsing.
// DOCX files are ZIP archives containing XML documents. The main text
// content lives in word/document.xml.
func (d *docTools) parseDOCX(path string, resp docResponse) (docResponse, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return resp, fmt.Errorf("failed to open DOCX: %w", err)
	}
	defer func() { _ = r.Close() }()

	var sb strings.Builder

	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return resp, fmt.Errorf("failed to read document.xml: %w", err)
		}

		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return resp, fmt.Errorf("failed to read document content: %w", err)
		}

		text := d.extractDOCXText(content)
		sb.WriteString(text)
		break
	}

	resp.Content = strings.TrimSpace(sb.String())
	if len(resp.Content) > maxOutputBytes {
		resp.Content = resp.Content[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
	}
	resp.CharCount = len(resp.Content)
	resp.LineCount = strings.Count(resp.Content, "\n") + 1
	resp.Message = fmt.Sprintf("Extracted %d characters from DOCX", resp.CharCount)
	return resp, nil
}

// extractDOCXText walks the XML tree and collects text nodes from
// <w:t> elements, inserting newlines at paragraph boundaries (<w:p>).
func (d *docTools) extractDOCXText(xmlData []byte) string {
	var sb strings.Builder
	decoder := xml.NewDecoder(bytes.NewReader(xmlData))
	inParagraph := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "p" {
				if inParagraph {
					sb.WriteString("\n")
				}
				inParagraph = true
			}
		case xml.CharData:
			sb.Write(t)
		}
	}

	return sb.String()
}

// parsePDF extracts text from a PDF using a multi-strategy approach:
//  1. pdftotext (poppler-utils) — fast, handles forms and text PDFs
//  2. tesseract OCR — handles scanned/image-based PDFs
//  3. dslipak/pdf (pure Go) — fallback when no system tools are available
//
// Each strategy is tried in order. If pdftotext returns empty output
// (common with scanned PDFs), we try OCR before giving up.
func (d *docTools) parsePDF(ctx context.Context, path string, resp docResponse) (docResponse, error) {
	// Strategy 1: pdftotext (fast, reliable for text PDFs and forms).
	content, err := d.pdfToText(ctx, path)
	if err == nil && len(strings.TrimSpace(content)) > 10 {
		resp.Content = strings.TrimSpace(content)
		if len(resp.Content) > maxOutputBytes {
			resp.Content = resp.Content[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
		}
		resp.CharCount = len(resp.Content)
		resp.LineCount = strings.Count(resp.Content, "\n") + 1
		resp.Message = fmt.Sprintf("Extracted %d characters via pdftotext", resp.CharCount)
		return resp, nil
	}

	// Strategy 2: Tesseract OCR (for scanned/image-based PDFs).
	content, err = d.pdfOCR(ctx, path)
	if err == nil && len(strings.TrimSpace(content)) > 10 {
		resp.Content = strings.TrimSpace(content)
		if len(resp.Content) > maxOutputBytes {
			resp.Content = resp.Content[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
		}
		resp.CharCount = len(resp.Content)
		resp.LineCount = strings.Count(resp.Content, "\n") + 1
		resp.Message = fmt.Sprintf("Extracted %d characters via OCR (tesseract)", resp.CharCount)
		return resp, nil
	}

	// Strategy 3: dslipak/pdf (pure Go fallback, may hang on complex PDFs).
	type pdfResult struct {
		content   string
		pageCount int
		err       error
	}

	resultCh := make(chan pdfResult, 1)
	go func() {
		c, pages, e := d.parsePDFDirect(path)
		resultCh <- pdfResult{c, pages, e}
	}()

	select {
	case res := <-resultCh:
		if res.err != nil {
			return resp, res.err
		}

		resp.PageCount = res.pageCount
		resp.Content = strings.TrimSpace(res.content)
		if len(resp.Content) > maxOutputBytes {
			resp.Content = resp.Content[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
		}
		resp.CharCount = len(resp.Content)
		resp.LineCount = strings.Count(resp.Content, "\n") + 1
		resp.Message = fmt.Sprintf("Extracted %d characters from %d pages (pure Go fallback)", resp.CharCount, res.pageCount)
		return resp, nil

	case <-ctx.Done():
		return resp, fmt.Errorf("PDF parsing timed out: %w", ctx.Err())
	}
}

// pdfToText shells out to poppler's pdftotext for reliable text extraction.
// Returns empty string if pdftotext is not installed.
func (d *docTools) pdfToText(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", path, "-")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// pdfOCR uses pdftoppm (poppler) + tesseract for OCR on scanned PDFs.
// Returns empty string if either tool is not installed.
func (d *docTools) pdfOCR(ctx context.Context, path string) (string, error) {
	// Check if both tools exist.
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return "", osutils.ToolNotFoundError("pdftoppm", map[string]string{"brew": "poppler", "apt": "poppler-utils"})
	}
	if _, err := exec.LookPath("tesseract"); err != nil {
		return "", osutils.ToolNotFoundError("tesseract", map[string]string{"apt": "tesseract-ocr", "apk": "tesseract-ocr"})
	}

	// Create temp dir for page images.
	tmpDir, err := os.MkdirTemp("", "doctool-ocr-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Convert PDF pages to PNG images using pdftoppm.
	prefix := filepath.Join(tmpDir, "page")
	cmd := exec.CommandContext(ctx, "pdftoppm", "-png", "-r", "200", path, prefix)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftoppm failed: %w", err)
	}

	// Find generated page images.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	pageNum := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".png") {
			continue
		}
		pageNum++

		// Check context before processing each page.
		if ctx.Err() != nil {
			return sb.String(), fmt.Errorf("OCR cancelled: %w", ctx.Err())
		}

		imgPath := filepath.Join(tmpDir, entry.Name())
		ocrCmd := exec.CommandContext(ctx, "tesseract", imgPath, "stdout", "--psm", "6")
		out, err := ocrCmd.Output()
		if err != nil {
			continue // skip pages that fail OCR
		}

		text := strings.TrimSpace(string(out))
		if text != "" {
			if pageNum > 1 {
				fmt.Fprintf(&sb, "\n\n--- Page %d ---\n\n", pageNum)
			}
			sb.WriteString(text)
		}

		// Stop if we've already exceeded the output limit.
		if sb.Len() > maxOutputBytes {
			break
		}
	}

	return sb.String(), nil
}

// parsePDFDirect is the pure-Go PDF extraction fallback using dslipak/pdf.
// Isolated in a separate method so it can be run in a goroutine with timeout.
func (d *docTools) parsePDFDirect(path string) (string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("failed to open PDF: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, _ := f.Stat()
	reader, err := pdfread.NewReader(f, info.Size())
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse PDF: %w", err)
	}

	var sb strings.Builder
	numPages := reader.NumPage()

	for i := 1; i <= numPages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		if text != "" {
			if i > 1 {
				sb.WriteString("\n\n--- Page " + fmt.Sprintf("%d", i) + " ---\n\n")
			}
			sb.WriteString(text)
		}
	}

	return sb.String(), numPages, nil
}
