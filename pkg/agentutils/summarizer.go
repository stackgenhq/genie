package agentutils

import (
	"context"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// OutputFormat controls the structure of the summarized output.
// Supported values are JSON, Text, and YAML.
type OutputFormat string

const (
	// OutputFormatJSON instructs the summarizer to produce a JSON-formatted summary.
	OutputFormatJSON OutputFormat = "JSON"

	// OutputFormatText instructs the summarizer to produce a plain-text summary.
	OutputFormatText OutputFormat = "TEXT"

	// OutputFormatMarkdown instructs the summarizer to produce a markdown-formatted summary.
	OutputFormatMarkdown OutputFormat = "MARKDOWN"

	// OutputFormatYAML instructs the summarizer to produce a YAML-formatted summary.
	OutputFormatYAML OutputFormat = "YAML"
)

// SummarizeRequest is the input for both the Summarizer.Summarize method
// and the summarize_content tool.
// Content is the raw text to summarize and RequiredOutputFormat
// determines the structure of the result.
type SummarizeRequest struct {
	Content              string       `json:"content" jsonschema:"description=The content to summarize,required"`
	RequiredOutputFormat OutputFormat `json:"required_output_format" jsonschema:"description=Output format: JSON or TEXT or YAML,required,enum=JSON,enum=TEXT,enum=YAML"`
}

//go:generate go tool counterfeiter -generate

// Summarizer produces context-aware summaries using a lightweight LLM model.
// It exists so that tool outputs and other verbose content can be condensed
// into structured formats (JSON, Text, YAML) before being presented to
// downstream consumers. Without this, callers would need to implement
// their own prompt engineering and LLM orchestration for summarization.
//
//counterfeiter:generate . Summarizer
type Summarizer interface {
	Summarize(ctx context.Context, req SummarizeRequest) (string, error)
}

// summarizer is the concrete implementation of Summarizer backed by an expert.Expert.
type summarizer struct {
	expert expert.Expert
}

// summarizePrompt is the system prompt used for the summarization expert.
// It instructs the model to be a strict summarizer that only outputs the
// requested format with no preamble or explanation.
const summarizePrompt = `You are a precise summarization engine.
Your job is to read the provided content and produce a concise, context-aware summary.

Rules:
1. Output ONLY the summary in the requested format — no preamble, no explanation, no markdown fences.
2. Preserve key facts, identifiers, and relationships from the input.
3. If the requested format is JSON, output valid JSON.
4. If the requested format is YAML, output valid YAML.
5. If the requested format is TEXT, output clean plain text.
6. Never fabricate information that is not in the input.`

// NewSummarizer creates a new Summarizer that uses the frontdesk (fast, cheap)
// model to condense content into the requested output format.
// The returned Summarizer makes single, tool-free LLM calls, keeping
// latency and cost minimal.
// Without this constructor, callers would need to manually wire an
// expert.Expert with the correct system prompt and task type for summarization.
func NewSummarizer(ctx context.Context, modelProvider modelprovider.ModelProvider, auditor audit.Auditor) (Summarizer, error) {
	bio := expert.ExpertBio{
		Personality: summarizePrompt,
		Name:        "summarizer",
		Description: "Summarizes content into structured output formats",
	}

	exp, err := bio.ToExpert(ctx, modelProvider, auditor, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create summarizer expert: %w", err)
	}

	return &summarizer{expert: exp}, nil
}

// NewSummarizerWithExpert creates a Summarizer using a pre-built expert.Expert.
// This is primarily useful in tests where the caller wants to inject a stub
// or fake expert without needing a real model provider.
// Without this constructor, tests would need to set up a full model provider
// and audit infrastructure just to test summarization logic.
func NewSummarizerWithExpert(exp expert.Expert) Summarizer {
	return &summarizer{expert: exp}
}

// Summarize condenses the given content into the requested output format
// using the frontdesk model. It validates inputs, constructs the prompt,
// and makes a single LLM call with no tool iterations.
// Without this method, callers would have to build their own prompt
// formatting and LLM invocation logic for every summarization need.
func (s *summarizer) Summarize(ctx context.Context, req SummarizeRequest) (string, error) {
	logr := logger.GetLogger(ctx).With("fn", "summarizer.Summarize", "format", req.RequiredOutputFormat)

	if strings.TrimSpace(req.Content) == "" {
		return "", fmt.Errorf("content must not be empty")
	}

	if !isValidOutputFormat(req.RequiredOutputFormat) {
		return "", fmt.Errorf("unsupported output format: %q (must be JSON, TEXT, or YAML)", req.RequiredOutputFormat)
	}

	message := fmt.Sprintf(
		"Summarize the following content into %s format.\n\n--- BEGIN CONTENT ---\n%s\n--- END CONTENT ---",
		string(req.RequiredOutputFormat),
		req.Content,
	)

	logr.Info("summarization requested", "content_length", len(req.Content))

	resp, err := s.expert.Do(ctx, expert.Request{
		Message:  message,
		TaskType: modelprovider.TaskSummarizer,
		Mode: expert.ExpertConfig{
			MaxLLMCalls:       1,
			MaxToolIterations: 0,
		},
	})
	if err != nil {
		return "", fmt.Errorf("summarization LLM call failed: %w", err)
	}

	output := extractTextFromChoices(resp.Choices)
	if strings.TrimSpace(output) == "" {
		return "", fmt.Errorf("summarization produced empty output")
	}

	logr.Info("summarization completed", "output_length", len(output))
	return output, nil
}

// isValidOutputFormat checks whether the given format is one of the
// supported output formats (JSON, TEXT, YAML).
func isValidOutputFormat(f OutputFormat) bool {
	switch f {
	case OutputFormatJSON, OutputFormatText, OutputFormatYAML, OutputFormatMarkdown:
		return true
	default:
		return false
	}
}

// extractTextFromChoices returns the text content from the last model choice.
// Earlier versions concatenated ALL choices, which caused duplicate output when
// the model returned multiple choices with identical content (e.g. the resume
// appeared twice). Standard LLM usage only needs the first (best) choice.
func extractTextFromChoices(choices []model.Choice) string {
	if len(choices) == 0 {
		return ""
	}
	return choices[len(choices)-1].Message.Content
}

// summarizeTool wraps a Summarizer as a trpc-agent-go tool so that any
// agent can invoke summarization through the standard tool interface.
type summarizeTool struct {
	summarizer Summarizer
}

// NewSummarizerTool returns a tool.Tool named "summarize_content" that
// wraps the given Summarizer. Other agents can include this tool in their
// tool registry to get on-demand summarization of verbose content.
// Without this, agents would need direct access to the Summarizer interface
// rather than the standard tool-calling mechanism.
func NewSummarizerTool(s Summarizer) tool.Tool {
	st := &summarizeTool{summarizer: s}

	return function.NewFunctionTool(
		st.execute,
		function.WithName("summarize_content"),
		function.WithDescription(
			"Summarize verbose content into a concise, structured format. "+
				"Use this when a tool output or document is too large and needs to be condensed "+
				"before further processing. Supports JSON, TEXT, YAML and MARKDOWN output formats."),
	)
}

func (st *summarizeTool) execute(ctx context.Context, req SummarizeRequest) (string, error) {
	return st.summarizer.Summarize(ctx, req)
}

// SetSkipSummarize instructs the upstream auto-summarize middleware to bypass
// summarization for the current tool call, returning its verbatim output.
func SetSkipSummarize(ctx context.Context) {
	toolcontext.GetSkipSummarizeSetter(ctx)()
}
