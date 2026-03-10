// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package skills

// This package provides integration between Genie and the trpc-agent-go skill system.
// Instead of reimplementing the skill repository, we use trpc.group/trpc-go/trpc-agent-go/skill
// directly, which already provides all the functionality we need for skill discovery,
// loading, and management per the agentskills.io specification.
//
// The trpc-agent-go/skill package provides:
// - FSRepository for filesystem-backed skill discovery
// - YAML frontmatter parsing for SKILL.md files
// - Skill name validation
// - Auxiliary document loading
//
// This package adds Genie-specific integration:
// - Configuration loading from GenieConfig
// - Tool creation for agent integration
// - Script execution with workspace isolation
