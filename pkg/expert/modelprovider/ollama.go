/*
Copyright © 2026 StackGen, Inc.
*/

package modelprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultOllamaURL is the default base URL for a local Ollama server.
const DefaultOllamaURL = "http://localhost:11434"

// DefaultOllamaModelForSetup is the default model offered during setup. It runs well on
// Mac (Apple Silicon) and is pulled behind the scenes if not already present.
const DefaultOllamaModelForSetup = "llama3.2:3b"

// OllamaReachable reports whether an Ollama server is reachable at the given URL.
// If url is empty, DefaultOllamaURL is used. The check uses a short timeout so
// setup does not block if Ollama is not running.
func OllamaReachable(ctx context.Context, url string) bool {
	if url == "" {
		url = DefaultOllamaURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/tags", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ollamaTagsResponse is the JSON response from GET /api/tags.
type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ListModels returns the names of models available on the Ollama server at url.
// If url is empty, DefaultOllamaURL is used. Returns an error if the request
// fails or the response cannot be parsed.
func ListModels(ctx context.Context, url string) ([]string, error) {
	if url == "" {
		url = DefaultOllamaURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama list models: HTTP %d", resp.StatusCode)
	}
	var out ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}
	seen := make(map[string]struct{})
	names := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		if m.Name != "" {
			if _, ok := seen[m.Name]; !ok {
				seen[m.Name] = struct{}{}
				names = append(names, m.Name)
			}
		}
	}
	return names, nil
}

// PullModel pulls the named model from the Ollama library to the local server at url.
// If url is empty, DefaultOllamaURL is used. The pull runs with stream disabled so it
// blocks until the model is fully pulled. Use a context with a long timeout (e.g. 10+ minutes).
func PullModel(ctx context.Context, url, model string) error {
	if url == "" {
		url = DefaultOllamaURL
	}
	if model == "" {
		return fmt.Errorf("ollama pull: model name is required")
	}
	body, _ := json.Marshal(map[string]any{"name": model, "stream": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama pull: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Pull can take many minutes for large models.
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama pull: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := string(slurp)
		if len(msg) >= 4096 {
			msg = msg + "..."
		}
		return fmt.Errorf("ollama pull: HTTP %d: %s", resp.StatusCode, msg)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
