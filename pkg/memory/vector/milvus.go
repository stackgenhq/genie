package vector

import (
	"context"
	"fmt"
	"time"

	"github.com/milvus-io/milvus/client/v2/entity"
	"google.golang.org/grpc"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
	milvusvs "trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/milvus"
)

// Milvus support is integrated into the vector store via the Config struct
// in store.go. To use Milvus as the vector store backend:
//
//  1. Set VectorStoreProvider to "milvus" in your configuration
//  2. Configure Milvus connection settings:
//     - MilvusAddress (required): e.g., "localhost:19530"
//     - MilvusUsername, MilvusPassword, MilvusAPIKey (optional): for authentication
//     - MilvusDBName (optional): database name
//     - MilvusCollectionName (optional): defaults to "trpc_agent_documents"
//     - MilvusDimension (optional): defaults to embedder dimension or 1536
//
// Example configuration:
//
//	[vector_memory]
//	vector_store_provider = "milvus"
//	embedding_provider = "openai"
//	api_key = "${OPENAI_API_KEY}"
//
//	[vector_memory.milvus]
//	milvus_address = "localhost:19530"
//	milvus_db_name = "genie"
//	milvus_collection_name = "genie_documents"
//
// The Milvus implementation is provided by trpc-agent-go/knowledge/vectorstore/milvus
// and supports all standard vector store operations (Add, Search, Delete, etc.)
// with automatic persistence handled by Milvus itself.

// buildMilvusStore creates a Milvus vector store instance based on the configuration.
// This function exists to initialize Milvus with proper configuration options.
// Without this function, Milvus support would not be available and users would be
// limited to in-memory stores only.

type MilvusConfig struct {
	Address  string `yaml:"milvus_address" toml:"milvus_address"` // e.g., "localhost:19530"
	Username string `yaml:"milvus_username" toml:"milvus_username"`
	Password string `yaml:"milvus_password" toml:"milvus_password"`
	DBName   string `yaml:"milvus_db_name" toml:"milvus_db_name"`
	APIKey   string `yaml:"milvus_api_key" toml:"milvus_api_key"`
	// MilvusCollectionName is the name of the collection to use/create in Milvus.
	// Defaults to "trpc_agent_documents" if not specified.
	CollectionName string `yaml:"milvus_collection_name" toml:"milvus_collection_name"`
	// MilvusDimension is the vector dimension. Must match the embedder's dimension.
	// Defaults to 1536 if not specified.
	Dimension int `yaml:"milvus_dimension" toml:"milvus_dimension"`
}

func (cfg Config) buildMilvusStore(ctx context.Context, emb embedder.Embedder) (vectorstore.VectorStore, error) {
	opts := []milvusvs.Option{}

	// Set address (required)
	if cfg.Milvus.Address == "" {
		return nil, fmt.Errorf("milvus address is required when using Milvus vector store")
	}
	opts = append(opts, milvusvs.WithAddress(cfg.Milvus.Address))

	// Set authentication if provided
	if cfg.Milvus.Username != "" {
		opts = append(opts, milvusvs.WithUsername(cfg.Milvus.Username))
	}
	if cfg.Milvus.Password != "" {
		opts = append(opts, milvusvs.WithPassword(cfg.Milvus.Password))
	}
	if cfg.Milvus.APIKey != "" {
		opts = append(opts, milvusvs.WithAPIKey(cfg.Milvus.APIKey))
	}

	// Set database name if provided
	if cfg.Milvus.DBName != "" {
		opts = append(opts, milvusvs.WithDBName(cfg.Milvus.DBName))
	}

	// Set collection name if provided
	if cfg.Milvus.CollectionName != "" {
		opts = append(opts, milvusvs.WithCollectionName(cfg.Milvus.CollectionName))
	}

	// Set dimension - use embedder's dimension or default
	dimension := cfg.Milvus.Dimension
	if dimension == 0 {
		dimension = emb.GetDimensions()
		if dimension == 0 {
			dimension = 1536 // fallback default
		}
	}
	opts = append(opts, milvusvs.WithDimension(dimension))

	// Set connection timeout via connect params (grpc.WithTimeout is deprecated).
	opts = append(opts, milvusvs.WithDialOptions(grpc.WithConnectParams(grpc.ConnectParams{
		MinConnectTimeout: 5 * time.Second,
	})))

	// Set metric type (default to IP for inner product similarity)
	opts = append(opts, milvusvs.WithMetricType(entity.IP))

	// Create Milvus vector store
	vs, err := milvusvs.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Milvus vector store: %w", err)
	}

	return vs, nil
}
