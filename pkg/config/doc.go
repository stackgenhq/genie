// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package config provides loading and aggregation of Genie configuration.
//
// It solves the problem of unifying all component settings (model, MCP, tools,
// messenger, cron, data sources, security, etc.) from files (.genie.toml,
// .genie.yaml) and environment variables. LoadGenieConfig resolves secret
// placeholders (${VAR}) via a SecretProvider and applies defaults so the rest
// of the application receives a single GenieConfig struct. Without this package,
// each component would read config independently and secret resolution would
// be scattered.
package config
