// Package regextool provides regular expression tools for agents. LLMs are
// notoriously unreliable at generating and reasoning about regex patterns —
// this tool lets them test, match, extract, and replace using Go's regexp
// engine for precise results.
//
// Problem: Agents need to parse structured text (log files, configs, code)
// and LLMs cannot reliably execute regex operations mentally. This package
// provides a deterministic regex engine the agent can call.
//
// Safety guards:
//   - Input capped at 1 MB to prevent excessive processing
//   - Pattern length capped at 10,000 chars
//   - Output truncated at 32 KB to limit LLM context consumption
//   - Go's RE2 engine guarantees linear-time matching (no catastrophic backtracking)
//
// Dependencies:
//   - Go stdlib only (regexp uses RE2 — guaranteed linear-time, no backtracking)
//   - No external system dependencies
package regextool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ────────────────────── Request / Response ──────────────────────

const (
	maxOutputBytes = 32 << 10 // 32 KB — cap tool output to limit LLM context usage
	maxInputBytes  = 1 << 20  // 1 MB — cap input to prevent excessive processing
)

type regexRequest struct {
	Operation   string `json:"operation" jsonschema:"description=The regex operation. One of: match, find_all, replace, split, extract_groups.,enum=match,enum=find_all,enum=replace,enum=split,enum=extract_groups"`
	Pattern     string `json:"pattern" jsonschema:"description=The regular expression pattern (Go RE2 syntax)."`
	Input       string `json:"input" jsonschema:"description=The input string to apply the pattern to."`
	Replacement string `json:"replacement,omitempty" jsonschema:"description=Replacement string for replace operation. Supports $1, $2 etc. for group references."`
}

type regexResponse struct {
	Operation string `json:"operation"`
	Pattern   string `json:"pattern"`
	Result    string `json:"result"`
	Count     int    `json:"count,omitempty"`
	Message   string `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type regexTools struct{}

func newRegexTools() *regexTools { return &regexTools{} }

func (r *regexTools) regexTool() tool.CallableTool {
	return function.NewFunctionTool(
		r.regex,
		function.WithName("assist_with_regular_expressions"),
		function.WithDescription(
			"Apply regular expressions to text. Supported operations: "+
				"match (test if pattern matches), "+
				"find_all (find all matches), "+
				"replace (replace matches with a replacement string — supports $1, $2 group references), "+
				"split (split string by pattern), "+
				"extract_groups (extract named/numbered capture groups from first match).",
		),
	)
}

// ────────────────────── Implementation ──────────────────────

func (r *regexTools) regex(_ context.Context, req regexRequest) (regexResponse, error) {
	op := strings.ToLower(strings.TrimSpace(req.Operation))
	resp := regexResponse{
		Operation: op,
		Pattern:   req.Pattern,
	}

	if req.Pattern == "" {
		return resp, fmt.Errorf("pattern is required")
	}
	if req.Input == "" {
		return resp, fmt.Errorf("input is required")
	}
	if len(req.Input) > maxInputBytes {
		return resp, fmt.Errorf("input too large (%d bytes, max %d)", len(req.Input), maxInputBytes)
	}
	if len(req.Pattern) > 10000 {
		return resp, fmt.Errorf("pattern too long (%d chars, max 10000)", len(req.Pattern))
	}

	re, err := regexp.Compile(req.Pattern)
	if err != nil {
		return resp, fmt.Errorf("invalid regex pattern %q: %w", req.Pattern, err)
	}

	switch op {
	case "match":
		return r.match(re, req, resp)
	case "find_all":
		return r.findAll(re, req, resp)
	case "replace":
		return r.replace(re, req, resp)
	case "split":
		return r.split(re, req, resp)
	case "extract_groups":
		return r.extractGroups(re, req, resp)
	default:
		return resp, fmt.Errorf("unsupported operation %q: must be one of match, find_all, replace, split, extract_groups", req.Operation)
	}
}

func (r *regexTools) match(re *regexp.Regexp, req regexRequest, resp regexResponse) (regexResponse, error) {
	matched := re.MatchString(req.Input)
	if matched {
		resp.Result = "true"
		resp.Message = "Pattern matches the input"
	} else {
		resp.Result = "false"
		resp.Message = "Pattern does not match the input"
	}
	return resp, nil
}

func (r *regexTools) findAll(re *regexp.Regexp, req regexRequest, resp regexResponse) (regexResponse, error) {
	matches := re.FindAllString(req.Input, -1)
	resp.Count = len(matches)
	b, _ := json.Marshal(matches)
	resp.Result = string(b)
	if len(resp.Result) > maxOutputBytes {
		resp.Result = resp.Result[:maxOutputBytes] + "...[truncated]"
	}
	resp.Message = fmt.Sprintf("Found %d match(es)", len(matches))
	return resp, nil
}

func (r *regexTools) replace(re *regexp.Regexp, req regexRequest, resp regexResponse) (regexResponse, error) {
	resp.Result = re.ReplaceAllString(req.Input, req.Replacement)
	if len(resp.Result) > maxOutputBytes {
		resp.Result = resp.Result[:maxOutputBytes] + "...[truncated]"
	}
	resp.Message = "Replacement applied"
	return resp, nil
}

func (r *regexTools) split(re *regexp.Regexp, req regexRequest, resp regexResponse) (regexResponse, error) {
	parts := re.Split(req.Input, -1)
	resp.Count = len(parts)
	b, _ := json.Marshal(parts)
	resp.Result = string(b)
	if len(resp.Result) > maxOutputBytes {
		resp.Result = resp.Result[:maxOutputBytes] + "...[truncated]"
	}
	resp.Message = fmt.Sprintf("Split into %d part(s)", len(parts))
	return resp, nil
}

func (r *regexTools) extractGroups(re *regexp.Regexp, req regexRequest, resp regexResponse) (regexResponse, error) {
	match := re.FindStringSubmatch(req.Input)
	if match == nil {
		resp.Result = "null"
		resp.Message = "No match found"
		return resp, nil
	}

	names := re.SubexpNames()
	groups := make(map[string]string)
	for i, name := range names {
		if i == 0 {
			groups["full_match"] = match[0]
			continue
		}
		key := name
		if key == "" {
			key = fmt.Sprintf("group_%d", i)
		}
		if i < len(match) {
			groups[key] = match[i]
		}
	}

	resp.Count = len(match) - 1 // exclude full match
	b, _ := json.Marshal(groups)
	resp.Result = string(b)
	resp.Message = fmt.Sprintf("Extracted %d group(s)", resp.Count)
	return resp, nil
}
