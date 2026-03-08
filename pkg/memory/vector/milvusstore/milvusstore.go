// Package milvusstore provides the Milvus vector store backend for Genie's
// semantic memory. It wraps the trpc-agent-go Milvus driver and exposes a
// Config + New() constructor so the parent vector package can build a Milvus
// store from user configuration.
//
// Separating this into its own package enables independent unit testing
// without pulling in the full vector store machinery or other backends.
package milvusstore

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

// Config holds the configuration for connecting to a Milvus instance.
// Without this config type, Milvus connection settings would have to be
// scattered across multiple top-level fields, making configuration less
// readable and harder to extend.
type Config struct {
	Address  string `yaml:"milvus_address,omitempty" toml:"milvus_address,omitempty"` // e.g., "localhost:19530"
	Username string `yaml:"milvus_username,omitempty" toml:"milvus_username,omitempty"`
	Password string `yaml:"milvus_password,omitempty" toml:"milvus_password,omitempty"`
	DBName   string `yaml:"milvus_db_name,omitempty" toml:"milvus_db_name,omitempty"`
	APIKey   string `yaml:"milvus_api_key,omitempty" toml:"milvus_api_key,omitempty"`
	// CollectionName is the name of the collection to use/create in Milvus.
	// Defaults to "trpc_agent_documents" if not specified.
	CollectionName string `yaml:"milvus_collection_name,omitempty" toml:"milvus_collection_name,omitempty"`
	// Dimension is the vector dimension. Must match the embedder's dimension.
	// Defaults to 1536 if not specified.
	Dimension int `yaml:"milvus_dimension,omitempty" toml:"milvus_dimension,omitempty,omitzero"`
}

// New creates a Milvus vector store instance based on the given Config and
// embedder. This function exists to initialize Milvus with proper
// configuration options. Without it, Milvus support would not be available
// and users would be limited to in-memory or Qdrant stores only.
func New(ctx context.Context, cfg Config, emb embedder.Embedder) (vectorstore.VectorStore, error) {
	var opts []milvusvs.Option

	// Set address (required)
	if cfg.Address == "" {
		return nil, fmt.Errorf("milvus address is required when using Milvus vector store")
	}
	opts = append(opts, milvusvs.WithAddress(cfg.Address))

	// Set authentication if provided
	if cfg.Username != "" {
		opts = append(opts, milvusvs.WithUsername(cfg.Username))
	}
	if cfg.Password != "" {
		opts = append(opts, milvusvs.WithPassword(cfg.Password))
	}
	if cfg.APIKey != "" {
		opts = append(opts, milvusvs.WithAPIKey(cfg.APIKey))
	}

	// Set database name if provided
	if cfg.DBName != "" {
		opts = append(opts, milvusvs.WithDBName(cfg.DBName))
	}

	// Set collection name if provided
	if cfg.CollectionName != "" {
		opts = append(opts, milvusvs.WithCollectionName(cfg.CollectionName))
	}

	// Set dimension - use embedder's dimension or default
	dimension := cfg.Dimension
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
