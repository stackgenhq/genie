// Package encodetool provides encoding, decoding, and hashing tools for
// agents. LLMs cannot natively perform base64 encoding, URL encoding, or
// cryptographic hashing — this package fills that gap with deterministic,
// correct transformations.
//
// Problem: Agents frequently need to encode API payloads, decode base64
// attachments, URL-encode query parameters, or compute checksums. LLMs
// cannot perform these operations natively and will hallucinate results.
//
// Supported operations:
//   - base64_encode / base64_decode — RFC 4648 standard encoding
//   - url_encode / url_decode — RFC 3986 percent-encoding
//   - sha256 — SHA-256 cryptographic hash (hex output)
//
// Dependencies:
//   - Go stdlib only (crypto/sha256, encoding/base64, net/url)
//   - No external system dependencies
package encodetool

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ────────────────────── Request / Response ──────────────────────

type encodeRequest struct {
	Operation string `json:"operation" jsonschema:"description=The encoding operation. One of: base64_encode, base64_decode, url_encode, url_decode, sha256,enum=base64_encode,enum=base64_decode,enum=url_encode,enum=url_decode,enum=sha256"`
	Input     string `json:"input" jsonschema:"description=The string to encode/decode/hash."`
}

type encodeResponse struct {
	Operation string `json:"operation"`
	Input     string `json:"input"`
	Result    string `json:"result"`
	Message   string `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type encodeTools struct{}

func newEncodeTools() *encodeTools {
	return &encodeTools{}
}

func (e *encodeTools) encodeTool() tool.CallableTool {
	desc := "Encode, decode, or hash strings. Supported operations: base64_encode, base64_decode, url_encode, url_decode, sha256."
	return function.NewFunctionTool(
		e.encode,
		function.WithName("encode_string"),
		function.WithDescription(desc),
	)
}

// ────────────────────── Implementation ──────────────────────

func (e *encodeTools) encode(_ context.Context, req encodeRequest) (encodeResponse, error) {
	op := strings.ToLower(strings.TrimSpace(req.Operation))
	resp := encodeResponse{
		Operation: op,
		Input:     req.Input,
	}

	if req.Input == "" {
		return resp, fmt.Errorf("input is required")
	}

	var err error
	switch op {
	case "base64_encode":
		resp.Result = base64.StdEncoding.EncodeToString([]byte(req.Input))
		resp.Message = "Base64 encoded successfully"

	case "base64_decode":
		decoded, decErr := base64.StdEncoding.DecodeString(req.Input)
		if decErr != nil {
			return resp, fmt.Errorf("base64 decode failed: %w", decErr)
		}
		resp.Result = string(decoded)
		resp.Message = "Base64 decoded successfully"

	case "url_encode":
		resp.Result = url.QueryEscape(req.Input)
		resp.Message = "URL encoded successfully"

	case "url_decode":
		resp.Result, err = url.QueryUnescape(req.Input)
		if err != nil {
			return resp, fmt.Errorf("URL decode failed: %w", err)
		}
		resp.Message = "URL decoded successfully"

	case "sha256":
		hash := sha256.Sum256([]byte(req.Input))
		resp.Result = hex.EncodeToString(hash[:])
		resp.Message = "SHA-256 hash computed"

	case "md5":
		return resp, fmt.Errorf("md5 is disabled by security policy; use sha256 for hashing")

	default:
		return resp, fmt.Errorf("unsupported operation %q: must be one of base64_encode, base64_decode, url_encode, url_decode, sha256", req.Operation)
	}

	return resp, nil
}
