// Package jsontool provides JSON querying, validation, and transformation
// tools backed by gjson (reads) and sjson (writes). These libraries support
// powerful dot-path syntax with array indexing, wildcards, and modifiers —
// far superior to hand-rolled path traversal.
//
// Problem: Agents frequently need to extract values from API responses,
// transform configuration files, or validate JSON payloads. LLMs are
// unreliable at mentally navigating deeply nested JSON structures. This
// package provides precise, deterministic JSON operations.
//
// gjson path syntax examples:
//
//	"name.first"           → nested key access
//	"friends.#"            → array length
//	"friends.0.name"       → first element
//	"friends.#.name"       → all names from array
//	"friends.#(age>40)"    → filter by condition
//	"friends.#(name%\"*ob*\")" → wildcard match
//
// Safety guards:
//   - Output truncated at 32 KB to limit LLM context consumption
//
// Dependencies:
//   - github.com/tidwall/gjson — high-performance JSON query (14k+ ⭐)
//   - github.com/tidwall/sjson — JSON mutation via path syntax (2k+ ⭐)
//   - No external system dependencies
//
// See https://github.com/tidwall/gjson for full syntax.
package jsontool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ────────────────────── Request / Response ──────────────────────

const maxOutputBytes = 32 << 10 // 32 KB — cap tool output to limit LLM context usage

type jsonRequest struct {
	Operation string `json:"operation" jsonschema:"description=The JSON operation. One of: validate, query, format, minify, diff_keys, set, delete.,enum=validate,enum=query,enum=format,enum=minify,enum=diff_keys,enum=set,enum=delete"`
	JSON      string `json:"json" jsonschema:"description=The JSON string to operate on."`
	// Path for 'query', 'set', 'delete': gjson/sjson dot-path syntax.
	// Examples: 'name.first', 'friends.0.age', 'friends.#.name', 'friends.#(age>40)'.
	Path  string `json:"path,omitempty" jsonschema:"description=gjson/sjson path expression. Supports nested keys (a.b.c), array indices (arr.0), array length (arr.#), wildcards (arr.#.name), and filters (arr.#(age>40))."`
	Value string `json:"value,omitempty" jsonschema:"description=Value to set for the 'set' operation. Can be a JSON value (string, number, object, array, boolean, null)."`
	JSON2 string `json:"json2,omitempty" jsonschema:"description=Second JSON string for diff_keys operation."`
}

type jsonResponse struct {
	Operation string `json:"operation"`
	Result    string `json:"result"`
	Valid     *bool  `json:"valid,omitempty"`
	Type      string `json:"type,omitempty"`
	Message   string `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type jsonTools struct{}

func newJSONTools() *jsonTools { return &jsonTools{} }

func (j *jsonTools) jsonTool() tool.CallableTool {
	return function.NewFunctionTool(
		j.jsonOp,
		function.WithName("util_json"),
		function.WithDescription(
			"Work with JSON data using gjson (read) and sjson (write). Supported operations: "+
				"validate (check if JSON is valid), "+
				"query (extract values using gjson path syntax — supports nested keys, arrays, wildcards, filters), "+
				"set (set a value at a path using sjson), "+
				"delete (remove a key at a path using sjson), "+
				"format (pretty-print JSON), "+
				"minify (compress JSON to single line), "+
				"diff_keys (compare top-level keys between two JSON objects). "+
				"Path examples: 'name.first', 'items.0', 'items.#', 'items.#.name', 'items.#(price>10)'.",
		),
	)
}

// ────────────────────── Implementation ──────────────────────

func (j *jsonTools) jsonOp(_ context.Context, req jsonRequest) (jsonResponse, error) {
	op := strings.ToLower(strings.TrimSpace(req.Operation))
	resp := jsonResponse{Operation: op}

	if req.JSON == "" {
		return resp, fmt.Errorf("json input is required")
	}

	switch op {
	case "validate":
		return j.validate(req, resp)
	case "query":
		return j.query(req, resp)
	case "set":
		return j.set(req, resp)
	case "delete":
		return j.deletePath(req, resp)
	case "format":
		return j.format(req, resp)
	case "minify":
		return j.minify(req, resp)
	case "diff_keys":
		return j.diffKeys(req, resp)
	default:
		return resp, fmt.Errorf("unsupported operation %q: must be one of validate, query, set, delete, format, minify, diff_keys", req.Operation)
	}
}

func (j *jsonTools) validate(req jsonRequest, resp jsonResponse) (jsonResponse, error) {
	valid := gjson.Valid(req.JSON)
	resp.Valid = &valid
	if valid {
		resp.Result = "true"
		resp.Message = "JSON is valid"
	} else {
		resp.Result = "false"
		resp.Message = "JSON is invalid"
	}
	return resp, nil
}

func (j *jsonTools) query(req jsonRequest, resp jsonResponse) (jsonResponse, error) {
	if req.Path == "" {
		return resp, fmt.Errorf("path is required for query operation")
	}
	if !gjson.Valid(req.JSON) {
		return resp, fmt.Errorf("invalid JSON input")
	}

	result := gjson.Get(req.JSON, req.Path)
	if !result.Exists() {
		return resp, fmt.Errorf("path %q not found in JSON", req.Path)
	}

	resp.Result = result.String()
	resp.Type = result.Type.String()

	// For complex types, return the raw JSON representation.
	if result.Type == gjson.JSON {
		resp.Result = result.Raw
	}

	if len(resp.Result) > maxOutputBytes {
		resp.Result = resp.Result[:maxOutputBytes] + "\n[...truncated — output exceeded 32 KB limit]"
	}

	resp.Message = fmt.Sprintf("Value at path %q (type: %s)", req.Path, resp.Type)
	return resp, nil
}

func (j *jsonTools) set(req jsonRequest, resp jsonResponse) (jsonResponse, error) {
	if req.Path == "" {
		return resp, fmt.Errorf("path is required for set operation")
	}
	if !gjson.Valid(req.JSON) {
		return resp, fmt.Errorf("invalid JSON input")
	}

	// Determine the Go value to set. sjson accepts raw values.
	var value any
	if err := json.Unmarshal([]byte(req.Value), &value); err != nil {
		// If it doesn't parse as JSON, treat it as a string.
		value = req.Value
	}

	result, err := sjson.Set(req.JSON, req.Path, value)
	if err != nil {
		return resp, fmt.Errorf("failed to set value at path %q: %w", req.Path, err)
	}

	resp.Result = result
	if len(resp.Result) > maxOutputBytes {
		resp.Result = resp.Result[:maxOutputBytes] + "\n[...truncated — output exceeded 32 KB limit]"
	}
	resp.Message = fmt.Sprintf("Set value at path %q", req.Path)
	return resp, nil
}

func (j *jsonTools) deletePath(req jsonRequest, resp jsonResponse) (jsonResponse, error) {
	if req.Path == "" {
		return resp, fmt.Errorf("path is required for delete operation")
	}
	if !gjson.Valid(req.JSON) {
		return resp, fmt.Errorf("invalid JSON input")
	}

	result, err := sjson.Delete(req.JSON, req.Path)
	if err != nil {
		return resp, fmt.Errorf("failed to delete path %q: %w", req.Path, err)
	}

	resp.Result = result
	resp.Message = fmt.Sprintf("Deleted path %q", req.Path)
	return resp, nil
}

func (j *jsonTools) format(req jsonRequest, resp jsonResponse) (jsonResponse, error) {
	var data any
	if err := json.Unmarshal([]byte(req.JSON), &data); err != nil {
		return resp, fmt.Errorf("invalid JSON: %w", err)
	}
	formatted, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return resp, fmt.Errorf("failed to format JSON: %w", err)
	}
	resp.Result = string(formatted)
	if len(resp.Result) > maxOutputBytes {
		resp.Result = resp.Result[:maxOutputBytes] + "\n[...truncated — output exceeded 32 KB limit]"
	}
	resp.Message = "JSON formatted (pretty-printed)"
	return resp, nil
}

func (j *jsonTools) minify(req jsonRequest, resp jsonResponse) (jsonResponse, error) {
	var data any
	if err := json.Unmarshal([]byte(req.JSON), &data); err != nil {
		return resp, fmt.Errorf("invalid JSON: %w", err)
	}
	compact, err := json.Marshal(data)
	if err != nil {
		return resp, fmt.Errorf("failed to minify JSON: %w", err)
	}
	resp.Result = string(compact)
	if len(resp.Result) > maxOutputBytes {
		resp.Result = resp.Result[:maxOutputBytes] + "\n[...truncated — output exceeded 32 KB limit]"
	}
	resp.Message = "JSON minified"
	return resp, nil
}

func (j *jsonTools) diffKeys(req jsonRequest, resp jsonResponse) (jsonResponse, error) {
	if req.JSON2 == "" {
		return resp, fmt.Errorf("json2 is required for diff_keys operation")
	}

	if !gjson.Valid(req.JSON) {
		return resp, fmt.Errorf("json is not valid JSON")
	}
	if !gjson.Valid(req.JSON2) {
		return resp, fmt.Errorf("json2 is not valid JSON")
	}

	// Use gjson to extract top-level keys.
	keys1 := j.extractKeys(req.JSON)
	keys2 := j.extractKeys(req.JSON2)

	set1 := j.toSet(keys1)
	set2 := j.toSet(keys2)

	onlyIn1 := []string{}
	onlyIn2 := []string{}
	common := []string{}

	for k := range set1 {
		if set2[k] {
			common = append(common, k)
		} else {
			onlyIn1 = append(onlyIn1, k)
		}
	}
	for k := range set2 {
		if !set1[k] {
			onlyIn2 = append(onlyIn2, k)
		}
	}

	diff := map[string]any{
		"only_in_first":  onlyIn1,
		"only_in_second": onlyIn2,
		"common":         common,
	}
	b, _ := json.MarshalIndent(diff, "", "  ")
	resp.Result = string(b)
	resp.Message = fmt.Sprintf("Keys: %d only in first, %d only in second, %d common",
		len(onlyIn1), len(onlyIn2), len(common))
	return resp, nil
}

// extractKeys returns the top-level keys of a JSON object using gjson.
func (j *jsonTools) extractKeys(jsonStr string) []string {
	result := gjson.Parse(jsonStr)
	if !result.IsObject() {
		return nil
	}
	var keys []string
	result.ForEach(func(key, _ gjson.Result) bool {
		keys = append(keys, key.String())
		return true
	})
	return keys
}

func (j *jsonTools) toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}
