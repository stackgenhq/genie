// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

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

	"github.com/stackgenhq/genie/pkg/memory/vector/qdrantstore"
	"github.com/stackgenhq/genie/pkg/orchestrator/orchestratorcontext"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

//go:generate go tool counterfeiter -generate

//counterfeiter:generate . IStore
type IStore interface {
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, error)
	Add(ctx context.Context, req AddRequest) error
	// Upsert replaces existing documents with the same ID, or inserts if not present.
	// Use a stable ID (e.g. source:external_id) to overwrite memory when appropriate.
	Upsert(ctx context.Context, req UpsertRequest) error
	Delete(ctx context.Context, req DeleteRequest) error
	Close(ctx context.Context) error
}

// SearchRequest holds the parameters for a vector store search.
// When Filter is non-nil, only documents whose metadata contains ALL
// specified entries are returned. This replaces the former SearchWithFilter
// method — an unfiltered search simply leaves Filter nil.
type SearchRequest struct {
	Query  string
	Limit  int
	Filter map[string]string
}

// AddRequest holds the items to insert into the vector store.
type AddRequest struct {
	Items []BatchItem
}

// UpsertRequest holds the items to upsert (replace-or-insert) in the vector store.
type UpsertRequest struct {
	Items []BatchItem
}

// DeleteRequest holds the IDs to remove from the vector store.
type DeleteRequest struct {
	IDs []string
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
	// Note: PersistenceDir is ignored when using an external store (Qdrant),
	// as those handle persistence internally.
	PersistenceDir    string `yaml:"persistence_dir,omitempty" toml:"persistence_dir,omitempty"`
	EmbeddingProvider string `yaml:"embedding_provider,omitempty" toml:"embedding_provider,omitempty"` // "openai", "ollama", "huggingface", "gemini"
	APIKey            string `yaml:"api_key,omitempty" toml:"api_key,omitempty"`
	OllamaURL         string `yaml:"ollama_url,omitempty" toml:"ollama_url,omitempty"`
	OllamaModel       string `yaml:"ollama_model,omitempty" toml:"ollama_model,omitempty"`
	HuggingFaceURL    string `yaml:"huggingface_url,omitempty" toml:"huggingface_url,omitempty"`
	GeminiAPIKey      string `yaml:"gemini_api_key,omitempty" toml:"gemini_api_key,omitempty"`
	GeminiModel       string `yaml:"gemini_model,omitempty" toml:"gemini_model,omitempty"`
	// VectorStoreProvider specifies the vector store backend to use.
	// Options: "inmemory" (default), "qdrant"
	VectorStoreProvider string `yaml:"vector_store_provider,omitempty" toml:"vector_store_provider,omitempty"`
	// Qdrant configuration (only used when VectorStoreProvider is "qdrant")
	Qdrant qdrantstore.Config `yaml:"qdrant,omitempty" toml:"qdrant,omitempty"`
	// AllowedMetadataKeys optionally restricts which metadata keys may be used in
	// memory_store and memory_search. If non-empty, only these keys are accepted
	// for metadata (store) and filter (search), enabling product/category buckets.
	AllowedMetadataKeys []string `yaml:"allowed_metadata_keys,omitempty" toml:"allowed_metadata_keys,omitempty"`
}

// DefaultConfig builds the default vector store configuration by resolving
// API keys and endpoints through the given SecretProvider. Without a
// SecretProvider, callers can pass security.NewEnvProvider() to preserve
// the legacy os.Getenv behavior.
func DefaultConfig(ctx context.Context, sp security.SecretProvider) Config {
	// Helper to resolve a secret, ignoring errors (treat as empty).
	get := func(name string) string {
		v, _ := sp.GetSecret(ctx, security.GetSecretRequest{
			Name:   name,
			Reason: toolcontext.GetJustification(ctx),
		})
		return v
	}

	return Config{
		VectorStoreProvider: "inmemory",
		EmbeddingProvider:   "dummy",
		APIKey:              get("OPENAI_API_KEY"),
		OllamaURL:           get("OLLAMA_URL"),
		OllamaModel:         get("OLLAMA_MODEL"),
		HuggingFaceURL:      get("HUGGINGFACE_URL"),
		GeminiAPIKey:        get("GOOGLE_API_KEY"),
		GeminiModel:         get("GEMINI_EMBED_MODEL"),
		Qdrant: qdrantstore.Config{
			Host:           get("QDRANT_HOST"),
			APIKey:         get("QDRANT_API_KEY"),
			CollectionName: get("QDRANT_COLLECTION_NAME"),
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
// When using Qdrant, persistence is handled by the external store itself.
type Store struct {
	vs               vectorstore.VectorStore
	embedder         embedder.Embedder
	mu               sync.Mutex
	persistDir       string
	useExternalStore bool // true if using Qdrant (skip snapshots)
}

// NewStore creates a new vector store backed by trpc-agent-go/knowledge.
// If cfg.PersistenceDir is set and using in-memory store, existing data is loaded from disk.
// If using Qdrant, persistence is handled by the external store itself.
func (cfg Config) NewStore(ctx context.Context) (*Store, error) {
	emb, err := cfg.buildEmbedder(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	var vs vectorstore.VectorStore
	useExternalStore := false

	// Determine which vector store to use (case-insensitive)
	vectorStoreProvider := strings.ToLower(strings.TrimSpace(cfg.VectorStoreProvider))
	if vectorStoreProvider == "" {
		vectorStoreProvider = "inmemory" // default
	}

	switch vectorStoreProvider {
	case "qdrant":
		vs, err = qdrantstore.New(ctx, qdrantstore.Config(cfg.Qdrant), emb)
		if err != nil {
			return nil, fmt.Errorf("failed to create Qdrant store: %w", err)
		}
		useExternalStore = true
	case "inmemory":
		vs = inmemory.New()
	default:
		return nil, fmt.Errorf("invalid vector_store_provider: %q (valid options: inmemory, qdrant)", cfg.VectorStoreProvider)
	}

	s := &Store{
		vs:               vs,
		embedder:         emb,
		persistDir:       cfg.PersistenceDir,
		useExternalStore: useExternalStore,
	}

	// Restore from disk if a snapshot exists (only for in-memory store).
	if !useExternalStore && cfg.PersistenceDir != "" {
		if err := s.loadSnapshot(ctx); err != nil {
			return nil, fmt.Errorf("failed to load snapshot: %w", err)
		}
	}

	return s, nil
}

// embeddedDoc holds an embedded document ready for vector store insertion.
// Pre-allocating these in a fixed-size slice lets us parallelise embedding
// generation while keeping the downstream vector store insert sequential
// (the underlying store may not be concurrency-safe).
type embeddedDoc struct {
	doc       *document.Document
	embedding []float64
}

// Add stores one or more documents in the vector store. When multiple items
// are provided, their embeddings are generated concurrently via errgroup,
// reducing wall-clock latency from N×round-trip to max(round-trip).
// A single disk snapshot is taken at the end.
func (s *Store) Add(ctx context.Context, req AddRequest) error {
	// Create a parent span so individual per-item embedding spans (created by
	// the upstream embedder) are nested under this operation rather than
	// appearing as orphaned root-level traces in Langfuse.
	ctx, span := trace.Tracer.Start(ctx, "vectorstore.add")
	span.SetAttributes(
		attribute.Int("vectorstore.batch_size", len(req.Items)),
		attribute.String("vectorstore.agent", orchestratorcontext.AgentNameFromContext(ctx)),
	)
	defer span.End()

	// Fast path: single item avoids errgroup overhead.
	if len(req.Items) == 1 {
		return s.addSingle(ctx, req.Items[0])
	}

	// Parallel path: generate embeddings concurrently.
	results := make([]embeddedDoc, len(req.Items))
	g, gctx := errgroup.WithContext(ctx)

	for i, item := range req.Items {
		i, item := i, item // capture loop variables
		g.Go(func() error {
			embedding, err := s.embedder.GetEmbedding(gctx, item.Text)
			if err != nil {
				return fmt.Errorf("failed to generate embedding for %s: %w", item.ID, err)
			}

			meta := make(map[string]any, len(item.Metadata))
			for k, v := range item.Metadata {
				meta[k] = v
			}

			results[i] = embeddedDoc{
				doc: &document.Document{
					ID:       item.ID,
					Content:  item.Text,
					Metadata: meta,
				},
				embedding: embedding,
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	// Sequential insert — the underlying vector store may not be concurrency-safe.
	for _, r := range results {
		if err := s.vs.Add(ctx, r.doc, r.embedding); err != nil {
			return fmt.Errorf("failed to store document %s: %w", r.doc.ID, err)
		}
	}

	// Single snapshot after all documents are added (only for in-memory store).
	if !s.useExternalStore {
		if err := s.saveSnapshot(ctx); err != nil {
			return fmt.Errorf("failed to save snapshot: %w", err)
		}
	}
	return nil
}

// addSingle stores a single document without errgroup overhead.
func (s *Store) addSingle(ctx context.Context, item BatchItem) error {
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

	if !s.useExternalStore {
		if err := s.saveSnapshot(ctx); err != nil {
			return fmt.Errorf("failed to save snapshot: %w", err)
		}
	}
	return nil
}

// Upsert replaces documents with the same ID (delete then add). Use a stable ID
// (e.g. source:external_id) so that re-ingestion overwrites rather than duplicates.
func (s *Store) Upsert(ctx context.Context, req UpsertRequest) error {
	ids := make([]string, 0, len(req.Items))
	for _, item := range req.Items {
		ids = append(ids, item.ID)
	}
	// Delete is best-effort for missing IDs; continue to Add.
	_ = s.Delete(ctx, DeleteRequest{IDs: ids})
	return s.Add(ctx, AddRequest{Items: req.Items})
}

// Search finds semantically similar documents, optionally filtered
// by metadata key-value pairs. Only documents whose metadata contains ALL
// specified filter entries are returned. Pass nil Filter for unfiltered search.
// This enables source-based memory isolation (e.g. per-sender, per-channel).
func (s *Store) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	// Create a parent span so the embedding call inside search is nested
	// rather than appearing as an orphaned root trace in Langfuse.
	ctx, span := trace.Tracer.Start(ctx, "vectorstore.search")
	span.SetAttributes(
		attribute.Int("vectorstore.limit", req.Limit),
		attribute.Int("vectorstore.filter_count", len(req.Filter)),
		attribute.String("vectorstore.agent", orchestratorcontext.AgentNameFromContext(ctx)),
	)
	defer span.End()

	searchQuery := &vectorstore.SearchQuery{
		Limit: req.Limit,
	}

	// Apply metadata filter if provided.
	if len(req.Filter) > 0 {
		metaFilter := make(map[string]any, len(req.Filter))
		for k, v := range req.Filter {
			metaFilter[k] = v
		}
		searchQuery.Filter = &vectorstore.SearchFilter{
			Metadata: metaFilter,
		}
	}

	// When query is empty and filters are present, use filter-only mode
	// to skip embedding entirely.
	if req.Query == "" && len(req.Filter) > 0 {
		searchQuery.SearchMode = vectorstore.SearchModeFilter
	} else if req.Query == "" {
		// No query and no filter — nothing meaningful to search for.
		return nil, fmt.Errorf("Search requires a non-empty query or at least one filter")
	} else {
		embedding, err := s.embedder.GetEmbedding(ctx, req.Query)
		if err != nil {
			return nil, fmt.Errorf("failed to generate query embedding: %w", err)
		}
		searchQuery.Vector = embedding
		searchQuery.SearchMode = vectorstore.SearchModeVector
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
func (s *Store) Delete(ctx context.Context, req DeleteRequest) error {
	var errs []error
	for _, id := range req.IDs {
		if err := s.vs.Delete(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete %s: %w", id, err))
		}
	}

	// Single snapshot after all deletes (only for in-memory store).
	if !s.useExternalStore {
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
// For Qdrant stores, it closes the client connection.
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
func (s *Store) loadSnapshot(ctx context.Context) error {
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

	for _, e := range entries {
		if err := s.vs.Add(ctx, e.Doc, e.Embedding); err != nil {
			return fmt.Errorf("failed to restore document %s: %w", e.Doc.ID, err)
		}
	}
	return nil
}
