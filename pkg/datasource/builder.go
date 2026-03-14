// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package datasource

import (
	"context"
	"sync"
)

// ConnectorFactory is a function that builds a DataSource for a given source
// name, using the provided context and ConnectorOptions. It returns nil when
// it cannot build the connector (e.g. missing credentials); the registry
// skips nil connectors silently.
//
// Packages register factories via RegisterConnectorFactory in their init()
// functions, so app.go only needs to blank-import the package to activate it.
type ConnectorFactory func(ctx context.Context, opts ConnectorOptions) DataSource

// ConnectorOptions holds the dependencies a factory may need to build its
// connector. All fields are optional; factories should check for nil / empty
// before use and return nil when required dependencies are missing.
type ConnectorOptions struct {
	// SecretProvider is the application's secret provider, passed as any to
	// avoid importing pkg/security from the datasource package. Each factory
	// type-asserts to security.SecretProvider before use.
	SecretProvider any
	// ExtraInt holds integer extras keyed by name (e.g. "max_depth").
	ExtraInt map[string]int
	// ExtraString holds string extras keyed by name (e.g. "credentials_file", "scm_provider").
	ExtraString map[string]string
}

var (
	factoryMu          sync.RWMutex
	connectorFactories = map[string]ConnectorFactory{}
)

// RegisterConnectorFactory registers a ConnectorFactory for the given source name.
// Call from an init() function in datasource-specific packages (e.g. gmail,
// gdrive) so the factory is picked up automatically when the package is
// imported. Subsequent calls override the previous factory for that source name.
func RegisterConnectorFactory(sourceName string, factory ConnectorFactory) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	connectorFactories[sourceName] = factory
}

// BuildConnectors returns a slice of DataSource connectors for all enabled
// sources in cfg. For each enabled source name:
//   - If a ConnectorFactory is registered (via RegisterConnectorFactory), it is
//     called with ctx and opts to build the connector.
//   - If no factory is registered, the source name is skipped (MCP-backed sources
//     such as jira/confluence/servicenow are expected to be provided by the caller
//     via the extra parameter and passed to a separate sync pipeline).
//
// extra is an optional list of additional DataSource connectors to include (e.g.
// MCP-backed sources built by the caller from mcpresource.NewFromServerName).
// Connectors in extra whose Name() is not in cfg.EnabledSourceNames() are filtered out.
func BuildConnectors(ctx context.Context, cfg *Config, opts ConnectorOptions, extra ...DataSource) map[string]DataSource {
	enabled := make(map[string]struct{})
	for _, name := range cfg.EnabledSourceNames() {
		enabled[name] = struct{}{}
	}

	result := make(map[string]DataSource)

	// Build connectors from registered factories.
	factoryMu.RLock()
	factories := make(map[string]ConnectorFactory, len(connectorFactories))
	for k, v := range connectorFactories {
		factories[k] = v
	}
	factoryMu.RUnlock()

	for name := range enabled {
		factory, ok := factories[name]
		if !ok {
			continue
		}
		conn := factory(ctx, opts)
		if conn != nil {
			result[name] = conn
		}
	}

	// Include caller-provided connectors (e.g. MCP-backed jira/confluence/servicenow).
	for _, conn := range extra {
		if conn == nil {
			continue
		}
		if _, ok := enabled[conn.Name()]; !ok {
			continue // not in enabled sources, skip
		}
		result[conn.Name()] = conn
	}

	return result
}
