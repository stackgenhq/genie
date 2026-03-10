// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package semanticrouter

import (
	"github.com/stackgenhq/genie/pkg/memory/vector"
)

const defaultThreshold = 0.85

// Config configures the semantic routing engine.
type Config struct {
	// Disabled determines whether semantic routing features are active.
	Disabled bool `yaml:"disabled,omitempty" toml:"disabled,omitempty"`

	// Threshold for semantic similarity matches (0.0 to 1.0).
	// Default is 0.85.
	Threshold float64 `yaml:"threshold,omitempty" toml:"threshold,omitempty"`

	// EnableCaching enables semantic caching for user LLM responses.
	EnableCaching bool `yaml:"enable_caching,omitempty" toml:"enable_caching,omitempty"`

	// VectorStore defines the embedding and storage backend used for
	// the semantic routing and caching. If empty, uses dummy embedder.
	VectorStore vector.Config `yaml:"vector_store,omitempty" toml:"vector_store,omitempty"`

	// Routes allows injecting custom semantic routes or extending builtin ones.
	Routes []Route `yaml:"routes,omitempty" toml:"routes,omitempty"`
}

// DefaultConfig provides sensible defaults.
func DefaultConfig() Config {
	return Config{
		Threshold:     defaultThreshold,
		EnableCaching: true,
	}
}
