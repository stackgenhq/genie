package vector

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder"
	geminiembed "trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/gemini"
	hfembed "trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/huggingface"
	openaiembed "trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/openai"
)

// buildEmbedder constructs the appropriate embedder based on configuration.
// It accepts a context because the Gemini embedder requires one for client
// initialization.
func (cfg Config) buildEmbedder(ctx context.Context) (embedder.Embedder, error) {
	switch cfg.EmbeddingProvider {
	case "openai":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("openai provider requested but no API key found")
		}
		return openaiembed.New(
			openaiembed.WithAPIKey(cfg.APIKey),
			openaiembed.WithModel(openaiembed.ModelTextEmbedding3Small),
		), nil

	case "ollama":
		ollamaURL := cfg.OllamaURL
		if ollamaURL == "" {
			ollamaURL = "http://localhost:11434"
		}
		model := cfg.OllamaModel
		if model == "" {
			model = "nomic-embed-text"
		}
		// Ollama exposes an OpenAI-compatible /v1/embeddings endpoint,
		// so we use the OpenAI embedder with a custom base URL.
		return openaiembed.New(
			openaiembed.WithBaseURL(ollamaURL+"/v1"),
			openaiembed.WithModel(model),
		), nil

	case "huggingface":
		hfURL := cfg.HuggingFaceURL
		if hfURL == "" {
			hfURL = hfembed.DefaultBaseURL
		}
		return hfembed.New(
			hfembed.WithBaseURL(hfURL),
		), nil

	case "gemini":
		apiKey := cfg.GeminiAPIKey
		if apiKey == "" {
			return nil, fmt.Errorf("gemini provider requested but no API key found (set GOOGLE_API_KEY)")
		}
		opts := []geminiembed.Option{
			geminiembed.WithAPIKey(apiKey),
		}
		if cfg.GeminiModel != "" {
			opts = append(opts, geminiembed.WithModel(cfg.GeminiModel))
		}
		return geminiembed.New(ctx, opts...)

	default:
		// Deterministic, non-semantic embedder for testing/dev.
		return &dummyEmbedder{}, nil
	}
}
