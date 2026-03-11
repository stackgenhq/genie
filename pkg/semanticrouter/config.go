// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package semanticrouter

import (
	"time"

	"github.com/stackgenhq/genie/pkg/memory/vector"
	mw "github.com/stackgenhq/genie/pkg/semanticrouter/semanticmiddleware"
)

const defaultThreshold = 0.85

// defaultCacheTTL is how long a semantic cache entry stays valid.
// Operational queries (health checks, pod listings) go stale quickly,
// so this is kept intentionally short.
const defaultCacheTTL = 5 * time.Minute

// Config configures the semantic routing engine.
type Config struct {
	// Disabled determines whether semantic routing features are active.
	Disabled bool `yaml:"disabled,omitempty" toml:"disabled,omitempty"`

	// Threshold for semantic similarity matches (0.0 to 1.0).
	// Default is 0.85.
	Threshold float64 `yaml:"threshold,omitempty" toml:"threshold,omitempty"`

	// EnableCaching enables semantic caching for user LLM responses.
	EnableCaching bool `yaml:"enable_caching,omitempty" toml:"enable_caching,omitempty"`

	// CacheTTL controls how long cached responses remain valid.
	// Expired entries are ignored on read. Default is 5 minutes.
	CacheTTL time.Duration `yaml:"cache_ttl,omitempty" toml:"cache_ttl,omitempty"`

	// VectorStore defines the embedding and storage backend used for
	// the semantic routing and caching. If empty, uses dummy embedder.
	VectorStore vector.Config `yaml:"vector_store,omitempty" toml:"vector_store,omitempty"`

	// Routes allows injecting custom semantic routes or extending builtin ones.
	Routes []Route `yaml:"routes,omitempty" toml:"routes,omitempty"`

	// --- Middleware configs ---

	// L0Regex configures the L0 regex pre-filter middleware that catches
	// conversational follow-ups and corrections before any embedding or LLM call.
	L0Regex mw.L0RegexConfig `yaml:"l0_regex,omitempty" toml:"l0_regex,omitempty"`

	// FollowUpBypass configures the follow-up bypass middleware that ensures
	// messages flagged as follow-ups by L0 skip the expensive L2 LLM call.
	FollowUpBypass mw.FollowUpBypassConfig `yaml:"follow_up_bypass,omitempty" toml:"follow_up_bypass,omitempty"`
}

// DefaultConfig provides sensible defaults.
func DefaultConfig() Config {
	return Config{
		Threshold:     defaultThreshold,
		EnableCaching: true,
		CacheTTL:      defaultCacheTTL,
	}
}
