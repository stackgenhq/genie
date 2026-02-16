package langfuse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/logger"
	"golang.org/x/sync/errgroup"
)

type langfusePromptsResponse struct {
	Data []struct {
		Name          string    `json:"name"`
		Type          string    `json:"type"`
		Versions      []int     `json:"versions"`
		Labels        []string  `json:"labels"`
		Tags          []string  `json:"tags"`
		LastUpdatedAt time.Time `json:"lastUpdatedAt"`
		LastConfig    any       `json:"lastConfig"`
	} `json:"data"`
	Meta struct {
		Page       int `json:"page"`
		Limit      int `json:"limit"`
		TotalItems int `json:"totalItems"`
		TotalPages int `json:"totalPages"`
	} `json:"meta"`
}

type remotePrompts map[string]string

func (r remotePrompts) get(name, defaultPrompt string) string {
	if val, ok := r[name]; ok {
		return val
	}
	return defaultPrompt
}

type promptResponse struct {
	Prompt string `json:"prompt"`
}

func (c *client) getAllPromptNames(ctx context.Context) ([]string, error) {
	url := fmt.Sprintf("%s/api/public/v2/prompts", c.config.langfuseHost())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result langfusePromptsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	promptNames := []string{}
	for _, p := range result.Data {
		promptNames = append(promptNames, p.Name)
	}
	return promptNames, nil
}

func (c *client) getAllPrompts(ctx context.Context) (remotePrompts, error) {
	logr := logger.GetLogger(ctx).With("fn", "getAllPrompts")
	logr.Info("getting all prompts")

	promptNames, err := c.getAllPromptNames(ctx)
	if err != nil {
		logr.Warn("could not get all prompt names", "error", err)
		return nil, err
	}
	prompts := make(remotePrompts)
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(5)

	for _, name := range promptNames {
		g.Go(func() error {
			// Fetch the latest version of each prompt using GET /api/public/v2/prompts/{promptName}
			promptContent, err := c.getPromptByName(gCtx, name)
			if err != nil {
				logr.Warn("could not get prompt by name", "name", name, "error", err)
				return nil
			}
			if promptContent != "" {
				mu.Lock()
				prompts[name] = promptContent
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		logr.Warn("could not get all prompts", "error", err)
		return nil, err
	}

	return prompts, nil
}

// getPromptByName fetches the latest version of a prompt by its name
func (c *client) getPromptByName(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("%s/api/public/v2/prompts/%s", c.config.langfuseHost(), name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading the response body: %w", err)
	}

	var result promptResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	return result.Prompt, nil
}

func (c *client) GetPrompt(ctx context.Context, name string, defaultPrompt string) string {
	if c.config.PublicKey == "" || c.config.SecretKey == "" || c.config.Host == "" || !c.config.EnablePrompts {
		return defaultPrompt
	}
	prompts, err := c.promptsCache.GetValue(ctx)
	if err != nil {
		logger.GetLogger(ctx).Warn("could not get prompts from cache", "error", err)
		return defaultPrompt
	}
	return prompts.get(name, defaultPrompt)
}
