// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree

import (
	"github.com/stackgenhq/genie/pkg/config"
	"github.com/stackgenhq/genie/pkg/hooks"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
)

// Toggles configures optional predictability and bounding mechanisms.
//
// Feature flags (like DryRun simulation) live in config.FeaturesConfig and
// are user-configurable via genie.toml. This struct holds runtime-injected
// dependencies that cannot be serialized to config files.
type Toggles struct {
	// Features holds the boolean feature flags from config.
	// This replaces the previous inline boolean fields, centralizing
	// feature toggle configuration in config.FeaturesConfig.
	Features config.FeaturesConfig

	// Reflector is the ActionReflector used for RAR loops.
	// When non-nil, agent output is reviewed before committing to the next iteration.
	Reflector ActionReflector `json:"-"`

	// Hooks are lifecycle callbacks invoked at well-defined points during
	// tree execution. Multiple hooks can be composed via hooks.NewChainHook.
	// Hooks replace the previous AuditEmitter field — the AuditHook
	// implementation provides the same audit-logging behavior.
	Hooks hooks.ExecutionHook `json:"-"`

	// FailureReflector generates verbal reflections on agent failures.
	// When set, failed episodes store an actionable summary of what went
	// wrong and what to try differently, instead of discarding the failure.
	FailureReflector memory.FailureReflector `json:"-"`

	// ImportanceScorer assigns 1-10 importance scores to episodes.
	// When set, every stored episode gets an importance score that
	// influences weighted retrieval — high-importance episodes surface
	// even when older. When nil, episodes get a neutral default (0.5).
	ImportanceScorer memory.ImportanceScorer `json:"-"`

	// WisdomStore provides access to consolidated daily wisdom notes.
	// When set, wisdom notes are injected into agent prompts. The store
	// is populated by the EpisodeConsolidator running periodically.
	WisdomStore memory.WisdomStore `json:"-"`
}
