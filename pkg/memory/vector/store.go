package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder"
	openaiembed "trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/openai"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/inmemory"
)

// Config holds the configuration for the vector store.
// It supports OpenAI, Ollama (via OpenAI-compatible endpoint), and a
// deterministic dummy embedder for development and testing.
type Config struct {
	// PersistenceDir is the directory where the vector store snapshot is
	// saved as a JSON file. If empty, the store is ephemeral (in-memory only).
	PersistenceDir    string `yaml:"persistence_dir" toml:"persistence_dir"`
	EmbeddingProvider string `yaml:"embedding_provider" toml:"embedding_provider"` // "openai", "ollama", "dummy"
	APIKey            string `yaml:"api_key" toml:"api_key"`
	OllamaURL         string `yaml:"ollama_url" toml:"ollama_url"`
	OllamaModel       string `yaml:"ollama_model" toml:"ollama_model"`
}

// snapshotFile is the filename used to persist the vector store state.
const snapshotFile = "vector_store.json"

// SearchResult represents a single result returned by Store.Search.
// It contains the matched document content, its metadata and
// the cosine similarity score (0.0–1.0, higher is more similar).
type SearchResult struct {
	ID       string         `json:"id"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Score    float64        `json:"score"`
}

// persistedEntry is the on-disk representation of a stored document
// together with its precomputed embedding vector.
type persistedEntry struct {
	Doc       *document.Document `json:"doc"`
	Embedding []float64          `json:"embedding"`
}

// Store wraps a trpc-agent-go in-memory vector store and embedder to
// provide simple add/search operations for agent memory.
// When PersistenceDir is set, the store snapshots its state to disk
// after every Add and restores it on startup.
type Store struct {
	vs         vectorstore.VectorStore
	embedder   embedder.Embedder
	mu         sync.Mutex
	persistDir string
}

// NewStore creates a new vector store backed by trpc-agent-go/knowledge.
// If cfg.PersistenceDir is set, existing data is loaded from disk.
func NewStore(_ context.Context, cfg Config) (*Store, error) {
	emb, err := buildEmbedder(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	vs := inmemory.New()

	s := &Store{
		vs:         vs,
		embedder:   emb,
		persistDir: cfg.PersistenceDir,
	}

	// Restore from disk if a snapshot exists.
	if cfg.PersistenceDir != "" {
		if err := s.loadSnapshot(); err != nil {
			return nil, fmt.Errorf("failed to load snapshot: %w", err)
		}
	}

	return s, nil
}

// Add stores a text with metadata under the given id.
// The text is embedded and stored in the vector store for later retrieval.
// If persistence is configured, the store is snapshotted to disk afterwards.
func (s *Store) Add(ctx context.Context, id string, text string, metadata map[string]string) error {
	embedding, err := s.embedder.GetEmbedding(ctx, text)
	if err != nil {
		return fmt.Errorf("failed to generate embedding: %w", err)
	}

	// Convert map[string]string → map[string]any for the document model.
	meta := make(map[string]any, len(metadata))
	for k, v := range metadata {
		meta[k] = v
	}

	doc := &document.Document{
		ID:       id,
		Content:  text,
		Metadata: meta,
	}

	if err := s.vs.Add(ctx, doc, embedding); err != nil {
		return fmt.Errorf("failed to store document: %w", err)
	}

	// Best-effort persist after every write.
	if s.persistDir != "" {
		if err := s.saveSnapshot(ctx); err != nil {
			return fmt.Errorf("failed to save snapshot: %w", err)
		}
	}

	return nil
}

// Search finds the most semantically similar documents to the query text.
// It returns up to limit results ordered by descending similarity.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	embedding, err := s.embedder.GetEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	res, err := s.vs.Search(ctx, &vectorstore.SearchQuery{
		Vector:     embedding,
		Limit:      limit,
		SearchMode: vectorstore.SearchModeVector,
	})
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	results := make([]SearchResult, 0, len(res.Results))
	for _, r := range res.Results {
		results = append(results, SearchResult{
			ID:       r.Document.ID,
			Content:  r.Document.Content,
			Metadata: r.Document.Metadata,
			Score:    r.Score,
		})
	}
	return results, nil
}

// snapshotPath returns the full path to the snapshot file.
func (s *Store) snapshotPath() string {
	return filepath.Join(s.persistDir, snapshotFile)
}

// saveSnapshot writes all documents and their embeddings to a JSON file.
func (s *Store) saveSnapshot(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	metadata, err := s.vs.GetMetadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to get metadata for snapshot: %w", err)
	}

	entries := make([]persistedEntry, 0, len(metadata))
	for id := range metadata {
		doc, embedding, err := s.vs.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to get document %s: %w", id, err)
		}
		entries = append(entries, persistedEntry{
			Doc:       doc,
			Embedding: embedding,
		})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	if err := os.MkdirAll(s.persistDir, 0755); err != nil {
		return fmt.Errorf("failed to create persistence dir: %w", err)
	}
	if err := os.WriteFile(s.snapshotPath(), data, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot file: %w", err)
	}
	return nil
}

// loadSnapshot restores documents and embeddings from a previously
// saved JSON snapshot file. If no snapshot exists, this is a no-op.
func (s *Store) loadSnapshot() error {
	data, err := os.ReadFile(s.snapshotPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No snapshot yet — fresh start.
		}
		return fmt.Errorf("failed to read snapshot file: %w", err)
	}

	var entries []persistedEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	ctx := context.Background()
	for _, e := range entries {
		if err := s.vs.Add(ctx, e.Doc, e.Embedding); err != nil {
			return fmt.Errorf("failed to restore document %s: %w", e.Doc.ID, err)
		}
	}
	return nil
}

// buildEmbedder constructs the appropriate embedder based on configuration.
func buildEmbedder(cfg Config) (embedder.Embedder, error) {
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

	default:
		// Deterministic, non-semantic embedder for testing/dev.
		return &dummyEmbedder{}, nil
	}
}

// dummyEmbedder implements embedder.Embedder with a deterministic,
// non-semantic embedding function. Suitable only for testing.
type dummyEmbedder struct{}

const dummyDimension = 1536

// GetEmbedding returns a deterministic vector derived from the text bytes.
func (d *dummyEmbedder) GetEmbedding(_ context.Context, text string) ([]float64, error) {
	vec := make([]float64, dummyDimension)
	for i := 0; i < len(text) && i < dummyDimension; i++ {
		vec[i] = float64(text[i]) / 255.0
	}
	return vec, nil
}

// GetEmbeddingWithUsage returns the same as GetEmbedding with nil usage.
func (d *dummyEmbedder) GetEmbeddingWithUsage(ctx context.Context, text string) ([]float64, map[string]any, error) {
	emb, err := d.GetEmbedding(ctx, text)
	return emb, nil, err
}

// GetDimensions returns the fixed dimensionality of the dummy embeddings.
func (d *dummyEmbedder) GetDimensions() int {
	return dummyDimension
}
