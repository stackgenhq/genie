package vector

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
	qdrantvs "trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/qdrant"
)

// Qdrant support is integrated into the vector store via the Config struct
// in store.go. To use Qdrant as the vector store backend:
//
//  1. Set VectorStoreProvider to "qdrant" in your configuration
//  2. Configure Qdrant connection settings:
//     - Host (required): e.g., "localhost"
//     - Port (optional): gRPC port, defaults to 6334
//     - APIKey (optional): for Qdrant Cloud authentication
//     - UseTLS (optional): for secure connections (required for Qdrant Cloud)
//     - CollectionName (optional): defaults to "trpc_agent_documents"
//     - Dimension (optional): defaults to embedder dimension or 1536
//
// Example configuration:
//
//	[vector_memory]
//	vector_store_provider = "qdrant"
//	embedding_provider = "openai"
//	api_key = "${OPENAI_API_KEY}"
//
//	[vector_memory.qdrant]
//	host = "localhost"
//	port = 6334
//	collection_name = "genie_documents"
//
// The Qdrant implementation is provided by trpc-agent-go/knowledge/vectorstore/qdrant
// and supports all standard vector store operations (Add, Search, Delete, etc.)
// with automatic persistence handled by Qdrant itself.

// QdrantConfig holds the configuration for connecting to a Qdrant instance.
// Without this config type, Qdrant connection settings would have to be
// scattered across the top-level Config or passed as individual fields,
// making configuration less readable and harder to extend.
type QdrantConfig struct {
	// Host is the Qdrant server hostname. Defaults to "localhost" if not specified.
	Host string `yaml:"host,omitempty" toml:"host,omitempty"`
	// Port is the Qdrant gRPC port. Defaults to 6334 if not specified.
	Port int `yaml:"port,omitempty" toml:"port,omitempty,omitzero"`
	// APIKey is the API key for Qdrant Cloud authentication.
	APIKey string `yaml:"api_key,omitempty" toml:"api_key,omitempty"`
	// UseTLS enables TLS for secure connections (required for Qdrant Cloud).
	UseTLS bool `yaml:"use_tls,omitempty" toml:"use_tls,omitempty,omitzero"`
	// CollectionName is the name of the collection to use/create in Qdrant.
	// Defaults to "trpc_agent_documents" if not specified.
	CollectionName string `yaml:"collection_name,omitempty" toml:"collection_name,omitempty"`
	// Dimension is the vector dimension. Must match the embedder's dimension.
	// Defaults to 1536 if not specified.
	Dimension int `yaml:"dimension,omitempty" toml:"dimension,omitempty,omitzero"`
}

// buildQdrantStore creates a Qdrant vector store instance based on the configuration.
// This function exists to initialize Qdrant with proper configuration options.
// Without this function, Qdrant support would not be available and users would be
// limited to in-memory stores only.
func (cfg Config) buildQdrantStore(ctx context.Context, emb embedder.Embedder) (vectorstore.VectorStore, error) {
	var opts []qdrantvs.Option

	// Set host if provided (the qdrant library defaults to "localhost").
	if cfg.Qdrant.Host != "" {
		opts = append(opts, qdrantvs.WithHost(cfg.Qdrant.Host))
	}

	// Set port if provided (the qdrant library defaults to 6334).
	if cfg.Qdrant.Port > 0 {
		opts = append(opts, qdrantvs.WithPort(cfg.Qdrant.Port))
	}

	// Set API key for Qdrant Cloud authentication.
	if cfg.Qdrant.APIKey != "" {
		opts = append(opts, qdrantvs.WithAPIKey(cfg.Qdrant.APIKey))
	}

	// Enable TLS if configured (required for Qdrant Cloud).
	if cfg.Qdrant.UseTLS {
		opts = append(opts, qdrantvs.WithTLS(true))
	}

	// Set collection name if provided.
	if cfg.Qdrant.CollectionName != "" {
		opts = append(opts, qdrantvs.WithCollectionName(cfg.Qdrant.CollectionName))
	}

	// Set dimension — use configured value, embedder's dimension, or default.
	dimension := cfg.Qdrant.Dimension
	if dimension == 0 {
		dimension = emb.GetDimensions()
		if dimension == 0 {
			dimension = 1536 // fallback default
		}
	}
	opts = append(opts, qdrantvs.WithDimension(dimension))

	// Create Qdrant vector store.
	vs, err := qdrantvs.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Qdrant vector store: %w", err)
	}

	return vs, nil
}
