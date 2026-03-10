// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package qdrantstore provides the Qdrant vector store backend for Genie's
// semantic memory. It wraps the trpc-agent-go Qdrant driver and exposes a
// Config + New() constructor so the parent vector package can build a Qdrant
// store from user configuration.
//
// Separating this into its own package enables independent unit testing
// without pulling in the full vector store machinery or other backends.
package qdrantstore

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
	qdrantvs "trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/qdrant"
)

// Config holds the configuration for connecting to a Qdrant instance.
// Without this config type, Qdrant connection settings would have to be
// scattered across multiple top-level fields, making configuration less
// readable and harder to extend.
type Config struct {
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

// New creates a Qdrant vector store instance based on the given Config and
// embedder. This function exists to initialize Qdrant with proper
// configuration options. Without it, Qdrant support would not be available
// and users would be limited to in-memory stores only.
func New(ctx context.Context, cfg Config, emb embedder.Embedder) (vectorstore.VectorStore, error) {
	var opts []qdrantvs.Option

	// Set host if provided (the qdrant library defaults to "localhost").
	if cfg.Host != "" {
		opts = append(opts, qdrantvs.WithHost(cfg.Host))
	}

	// Set port if provided (the qdrant library defaults to 6334).
	if cfg.Port > 0 {
		opts = append(opts, qdrantvs.WithPort(cfg.Port))
	}

	// Set API key for Qdrant Cloud authentication.
	if cfg.APIKey != "" {
		opts = append(opts, qdrantvs.WithAPIKey(cfg.APIKey))
	}

	// Enable TLS if configured (required for Qdrant Cloud).
	if cfg.UseTLS {
		opts = append(opts, qdrantvs.WithTLS(true))
	}

	// Set collection name if provided.
	if cfg.CollectionName != "" {
		opts = append(opts, qdrantvs.WithCollectionName(cfg.CollectionName))
	}

	// Set dimension — use configured value, embedder's dimension, or default.
	dimension := cfg.Dimension
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
