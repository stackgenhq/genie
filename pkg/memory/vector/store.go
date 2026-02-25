package vector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/stackgenhq/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/inmemory"
)

//go:generate go tool counterfeiter -generate

//counterfeiter:generate . IStore
type IStore interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
	SearchWithFilter(ctx context.Context, query string, limit int, filter map[string]string) ([]SearchResult, error)
	Add(ctx context.Context, items ...BatchItem) error
	Delete(ctx context.Context, ids ...string) error
	Close(ctx context.Context) error
}

// BatchItem represents a single document to be stored via Add.
type BatchItem struct {
	ID       string
	Text     string
	Metadata map[string]string
}

// Config holds the configuration for the vector store.
// It supports OpenAI, Ollama (via OpenAI-compatible endpoint), HuggingFace
// Text-Embeddings-Inference, Gemini, and a deterministic dummy embedder
// for development and testing.
type Config struct {
	// PersistenceDir is the directory where the vector store snapshot is
	// saved as a JSON file. If empty, the store is ephemeral (in-memory only).
	// Note: PersistenceDir is ignored when using Milvus as the vector store,
	// as Milvus handles persistence internally.
	PersistenceDir    string `yaml:"persistence_dir" toml:"persistence_dir"`
	EmbeddingProvider string `yaml:"embedding_provider" toml:"embedding_provider"` // "openai", "ollama", "huggingface", "gemini", "dummy"
	APIKey            string `yaml:"api_key" toml:"api_key"`
	OllamaURL         string `yaml:"ollama_url" toml:"ollama_url"`
	OllamaModel       string `yaml:"ollama_model" toml:"ollama_model"`
	HuggingFaceURL    string `yaml:"huggingface_url" toml:"huggingface_url"`
	GeminiAPIKey      string `yaml:"gemini_api_key" toml:"gemini_api_key"`
	GeminiModel       string `yaml:"gemini_model" toml:"gemini_model"`
	// VectorStoreProvider specifies the vector store backend to use.
	// Options: "inmemory" (default), "milvus"
	VectorStoreProvider string `yaml:"vector_store_provider" toml:"vector_store_provider"`
	// Milvus configuration (only used when VectorStoreProvider is "milvus")
	Milvus MilvusConfig `yaml:"milvus" toml:"milvus"`
}

// DefaultConfig builds the default vector store configuration by resolving
// API keys and endpoints through the given SecretProvider. Without a
// SecretProvider, callers can pass security.NewEnvProvider() to preserve
// the legacy os.Getenv behavior.
func DefaultConfig(ctx context.Context, sp security.SecretProvider) Config {
	// Helper to resolve a secret, ignoring errors (treat as empty).
	get := func(name string) string {
		v, _ := sp.GetSecret(ctx, name)
		return v
	}

	return Config{
		VectorStoreProvider: "inmemory",
		APIKey:              get("OPENAI_API_KEY"),
		OllamaURL:           get("OLLAMA_URL"),
		OllamaModel:         get("OLLAMA_MODEL"),
		HuggingFaceURL:      get("HUGGINGFACE_URL"),
		GeminiAPIKey:        get("GOOGLE_API_KEY"),
		GeminiModel:         get("GEMINI_EMBED_MODEL"),
		Milvus: MilvusConfig{
			Address:        get("MILVUS_ADDRESS"),
			Username:       get("MILVUS_USERNAME"),
			Password:       get("MILVUS_PASSWORD"),
			DBName:         get("MILVUS_DB_NAME"),
			APIKey:         get("MILVUS_API_KEY"),
			CollectionName: get("MILVUS_COLLECTION_NAME"),
		},
	}
}

// snapshotFile is the filename used to persist the vector store state.
const snapshotFile = "vector_store.json"

// SearchResult represents a single result returned by Store.Search.
// It contains the matched document content, its metadata and
// the cosine similarity score (0.0–1.0, higher is more similar).
type SearchResult struct {
	ID       string            `json:"id"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Score    float64           `json:"score"`
}

type SearchResults []SearchResult

func (s SearchResult) String() string {
	// create LLM friendly search result that includes type and Content
	return fmt.Sprintf("Type: %s\nContent: %s\nMetadata: %v", s.Metadata["type"], s.Content, s.Metadata)
}

// persistedEntry is the on-disk representation of a stored document
// together with its precomputed embedding vector.
type persistedEntry struct {
	Doc       *document.Document `json:"doc"`
	Embedding []float64          `json:"embedding"`
}

// Store wraps a trpc-agent-go vector store and embedder to
// provide simple add/search operations for agent memory.
// When PersistenceDir is set and using in-memory store, the store snapshots
// its state to disk after every Add and restores it on startup.
// When using Milvus, persistence is handled by Milvus itself.
type Store struct {
	vs         vectorstore.VectorStore
	embedder   embedder.Embedder
	mu         sync.Mutex
	persistDir string
	useMilvus  bool // true if using Milvus (skip snapshots)
}

// NewStore creates a new vector store backed by trpc-agent-go/knowledge.
// If cfg.PersistenceDir is set and using in-memory store, existing data is loaded from disk.
// If using Milvus, persistence is handled by Milvus itself.
func (cfg Config) NewStore(ctx context.Context) (*Store, error) {
	emb, err := cfg.buildEmbedder(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	var vs vectorstore.VectorStore
	useMilvus := false

	// Determine which vector store to use (case-insensitive)
	vectorStoreProvider := strings.ToLower(strings.TrimSpace(cfg.VectorStoreProvider))
	if vectorStoreProvider == "" {
		vectorStoreProvider = "inmemory" // default
	}

	switch vectorStoreProvider {
	case "milvus":
		vs, err = cfg.buildMilvusStore(ctx, emb)
		if err != nil {
			return nil, fmt.Errorf("failed to create Milvus store: %w", err)
		}
		useMilvus = true
	case "inmemory":
		vs = inmemory.New()
	default:
		return nil, fmt.Errorf("invalid vector_store_provider: %q (valid options: inmemory, milvus)", cfg.VectorStoreProvider)
	}

	s := &Store{
		vs:         vs,
		embedder:   emb,
		persistDir: cfg.PersistenceDir,
		useMilvus:  useMilvus,
	}

	// Restore from disk if a snapshot exists (only for in-memory store).
	if !useMilvus && cfg.PersistenceDir != "" {
		if err := s.loadSnapshot(); err != nil {
			return nil, fmt.Errorf("failed to load snapshot: %w", err)
		}
	}

	return s, nil
}

// Add stores one or more documents in the vector store. Each item is
// embedded and stored, and a single disk snapshot is taken at the end.
// Using variadic args makes this efficient for both single inserts and
// bulk ingestion (e.g. runbook loading).
func (s *Store) Add(ctx context.Context, items ...BatchItem) error {
	for _, item := range items {
		embedding, err := s.embedder.GetEmbedding(ctx, item.Text)
		if err != nil {
			return fmt.Errorf("failed to generate embedding for %s: %w", item.ID, err)
		}

		meta := make(map[string]any, len(item.Metadata))
		for k, v := range item.Metadata {
			meta[k] = v
		}

		doc := &document.Document{
			ID:       item.ID,
			Content:  item.Text,
			Metadata: meta,
		}

		if err := s.vs.Add(ctx, doc, embedding); err != nil {
			return fmt.Errorf("failed to store document %s: %w", item.ID, err)
		}
	}

	// Single snapshot after all documents are added (only for in-memory store).
	if !s.useMilvus {
		if err := s.saveSnapshot(ctx); err != nil {
			return fmt.Errorf("failed to save snapshot: %w", err)
		}
	}
	return nil
}

// Search finds the most semantically similar documents to the query text.
// It returns up to limit results ordered by descending similarity.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return s.SearchWithFilter(ctx, query, limit, nil)
}

// SearchWithFilter finds semantically similar documents, optionally filtered
// by metadata key-value pairs. Only documents whose metadata contains ALL
// specified filter entries are returned. Pass nil for unfiltered search.
// This enables source-based memory isolation (e.g. per-sender, per-channel).
func (s *Store) SearchWithFilter(ctx context.Context, query string, limit int, filter map[string]string) ([]SearchResult, error) {
	embedding, err := s.embedder.GetEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	searchQuery := &vectorstore.SearchQuery{
		Vector:     embedding,
		Limit:      limit,
		SearchMode: vectorstore.SearchModeVector,
	}

	// Apply metadata filter if provided.
	if len(filter) > 0 {
		metaFilter := make(map[string]any, len(filter))
		for k, v := range filter {
			metaFilter[k] = v
		}
		searchQuery.Filter = &vectorstore.SearchFilter{
			Metadata: metaFilter,
		}
	}

	res, err := s.vs.Search(ctx, searchQuery)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	results := make([]SearchResult, 0, len(res.Results))
	for _, r := range res.Results {
		// Convert map[string]any → map[string]string (we only store strings via Add).
		meta := make(map[string]string, len(r.Document.Metadata))
		for k, v := range r.Document.Metadata {
			meta[k] = fmt.Sprintf("%v", v)
		}
		results = append(results, SearchResult{
			ID:       r.Document.ID,
			Content:  r.Document.Content,
			Metadata: meta,
			Score:    r.Score,
		})
	}
	return results, nil
}

// Delete removes one or more documents by their IDs from the vector store.
// A single snapshot is taken at the end. Errors from individual deletes
// are collected but do not stop processing of remaining items.
func (s *Store) Delete(ctx context.Context, ids ...string) error {
	var errs []error
	for _, id := range ids {
		if err := s.vs.Delete(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete %s: %w", id, err))
		}
	}

	// Single snapshot after all deletes (only for in-memory store).
	if !s.useMilvus {
		if err := s.saveSnapshot(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to save snapshot after delete: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Close flushes any pending state to disk (if persistence is configured).
// For Milvus stores, it closes the Milvus client connection.
// It is safe to call multiple times.
func (s *Store) Close(ctx context.Context) error {
	if s.vs != nil {
		return s.vs.Close()
	}
	return s.saveSnapshot(ctx)
}

// snapshotPath returns the full path to the snapshot file.
func (s *Store) snapshotPath() string {
	return filepath.Join(s.persistDir, snapshotFile)
}

// saveSnapshot writes all documents and their embeddings to a JSON file.
func (s *Store) saveSnapshot(ctx context.Context) error {
	if s.persistDir == "" {
		return nil
	}
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
