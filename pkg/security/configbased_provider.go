// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/

package security

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/ttlcache"
	"github.com/tidwall/gjson"
	"gocloud.dev/runtimevar"
	"golang.org/x/sync/errgroup"

	// Import filevar driver so "file://" URLs work for local JSON/secret files.
	_ "gocloud.dev/runtimevar/filevar"
	// Import constantvar driver so "constant://" URLs work in tests and
	// local dev without any external dependencies.
	_ "gocloud.dev/runtimevar/constantvar"
	// Import AWS Secrets Manager driver so "awssecretsmanager://" URLs work
	// in the config.
	_ "gocloud.dev/runtimevar/awssecretsmanager"
	// Import GCP Secret Manager driver so "gcpsecretmanager://" URLs work
	// in the config.
	_ "gocloud.dev/runtimevar/gcpsecretmanager"
	// Import keyringvar so "keyring://" URLs work for secrets stored in the
	// system keyring (e.g. keyring://genie/TELEGRAM_BOT_TOKEN).
	_ "github.com/stackgenhq/genie/pkg/security/keyring"
)

const (
	// runtimevarTimeout caps how long resolveFromRuntimevar waits for a
	// backend (AWS SM, GCP SM, etc.) to return the first snapshot. Without
	// a timeout, missing credentials or network issues cause the app to
	// hang indefinitely at startup.
	runtimevarTimeout = 15 * time.Second

	// rawCacheTTL controls how long the resolved secret map is kept in
	// memory before the background retriever re-fetches all secrets from
	// their runtimevar backends. Multiple secret names that share the same
	// underlying runtimevar URL (e.g. different &path= into one AWS
	// secret) are deduplicated in a single retriever pass.
	rawCacheTTL = 5 * time.Minute
)

// remoteSecretMap is a name → raw-resolved-value map returned by the
// background retriever and cached by ttlcache.Item.
type remoteSecretMap map[string]string

// Manager is the primary SecretProvider implementation. It resolves secrets
// from gocloud.dev/runtimevar backends (GCP Secret Manager, AWS Secrets
// Manager, etcd, files, etc.) and falls back to os.Getenv for any secret
// name that has no explicit runtimevar URL mapping.
//
// On construction, Manager eagerly resolves every configured secret in a
// background goroutine and caches the raw values in a ttlcache.Item that
// is refreshed every rawCacheTTL/2. GetSecret reads from this in-memory
// cache and applies gjson path extraction on the fly, making individual
// lookups essentially free after the first fetch.
//
// Without this struct, the application would have no way to dynamically
// fetch secrets from cloud secret stores at runtime — every secret would
// need to be baked into the process environment before startup.
type Manager struct {
	mu             sync.RWMutex
	vars           map[string]*runtimevar.Variable
	secretURLs     map[string]string
	rawCache       *ttlcache.Item[remoteSecretMap]
	onSecretLookup func(ctx context.Context, req GetSecretRequest)
}

// ManagerOption configures a Manager at construction time.
type ManagerOption func(*Manager)

// WithSecretLookupAudit sets a callback invoked whenever a secret is successfully
// looked up (GetSecret returns a value). Use it to audit secret access; the
// callback receives the logical secret name only, never the value.
func WithSecretLookupAudit(fn func(ctx context.Context, req GetSecretRequest)) ManagerOption {
	return func(m *Manager) {
		m.onSecretLookup = fn
	}
}

// NewManager creates a Manager from the given Config and optional options.
// When secrets are configured, a background goroutine eagerly resolves all
// runtimevar URLs and keeps them fresh via ttlcache.Item.KeepItFresh. If
// Config.Secrets is empty, the Manager still works — it just falls back to
// os.Getenv for every lookup.
func NewManager(ctx context.Context, cfg Config, opts ...ManagerOption) *Manager {
	manager := &Manager{
		vars:       make(map[string]*runtimevar.Variable),
		secretURLs: cfg.Secrets,
	}
	manager.rawCache = ttlcache.NewItem(manager.resolveAllSecrets, rawCacheTTL)
	for _, opt := range opts {
		opt(manager)
	}

	if len(cfg.Secrets) > 0 {
		go func() {
			manager.rawCache.GetValue(ctx) //nolint:errcheck
		}()
	}

	return manager
}

// resolveAllSecrets is the ttlcache.Item retriever. It groups all
// configured secrets by their clean runtimevar URL (stripping &path=),
// resolves each unique URL exactly once in parallel via errgroup, and maps
// the raw values back to each secret name.
//
// URLs that fail to resolve are logged and skipped — all secret names
// sharing that URL will be absent from the returned map, causing GetSecret
// to return an error for those names.
func (m *Manager) resolveAllSecrets(ctx context.Context) (remoteSecretMap, error) {
	logr := logger.GetLogger(ctx).With("fn", "Manager.resolveAllSecrets")

	urlToNames := make(map[string][]string, len(m.secretURLs))
	for name, varURL := range m.secretURLs {
		cleanURL, _ := stripPathParam(varURL)
		urlToNames[cleanURL] = append(urlToNames[cleanURL], name)
	}

	var mu sync.Mutex
	rawByCleanURL := make(map[string]string, len(urlToNames))

	g, gctx := errgroup.WithContext(ctx)
	for cleanURL, names := range urlToNames {
		cleanURL := cleanURL
		names := names
		g.Go(func() error {
			raw, err := m.resolveFromRuntimevar(gctx, names[0], cleanURL)
			if err != nil {
				logr.Warn("Failed to resolve secret URL", "names", names, "error", err)
				return nil
			}
			mu.Lock()
			rawByCleanURL[cleanURL] = raw
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("failed to resolve remote secrets: %w", err)
	}

	result := make(remoteSecretMap, len(m.secretURLs))
	for name, varURL := range m.secretURLs {
		cleanURL, _ := stripPathParam(varURL)
		raw, ok := rawByCleanURL[cleanURL]
		if !ok {
			continue
		}
		result[name] = raw
	}

	logr.Info("Resolved remote secrets", "resolved", len(result), "configured", len(m.secretURLs), "uniqueURLs", len(urlToNames))
	return result, nil
}

// isConfigError returns true when the error indicates invalid configuration
// (e.g. unsupported scheme, invalid URL) rather than a runtime failure (e.g. network, timeout).
// Config errors are not fallback candidates for os.Getenv.
func isConfigError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unsupported") || strings.Contains(s, "unknown scheme") ||
		strings.Contains(s, "invalid url") || strings.Contains(s, "nosuchscheme")
}

// GetSecret retrieves the plaintext value of the named secret. It first
// checks whether a runtimevar URL mapping exists for the given name. If so,
// it reads the pre-resolved raw value from the in-memory cache and applies
// gjson path extraction when the URL contains "&path=<gjson-path>". If no
// mapping exists, it falls back to os.Getenv with optional gjson extraction
// via "?path=<gjson-path>" in the secret name.
//
// Errors are returned when the cache cannot be populated, or a requested
// gjson path does not exist in the JSON. A missing env var returns "" with
// no error — matching the existing behavior callers depend on.
func (m *Manager) GetSecret(ctx context.Context, req GetSecretRequest) (string, error) {
	logr := logger.GetLogger(ctx).With("fn", "Manager.GetSecret", "name", req.Name)
	var nameJSONPath string
	req.Name, nameJSONPath = req.parseSecretName()

	varURL, hasURL := m.secretURLs[req.Name]
	if !hasURL {
		rawValue := os.Getenv(req.Name)
		if nameJSONPath == "" {
			if rawValue != "" {
				m.auditSecretLookup(ctx, req)
			}
			return rawValue, nil
		}
		val, err := extractJSONPath(req.Name, rawValue, nameJSONPath)
		if err == nil && val != "" {
			m.auditSecretLookup(ctx, req)
		}
		return val, err
	}

	resolved, err := m.rawCache.GetValue(ctx)
	if err != nil {
		// Fall back to env only for runtime failures (e.g. network, timeout), not config errors (e.g. unsupported scheme).
		if !isConfigError(err) {
			logr.Warn("Runtimevar resolution failed, falling back to env var", "error", err)
			rawValue := os.Getenv(req.Name)
			if nameJSONPath == "" {
				if rawValue != "" {
					m.auditSecretLookup(ctx, req)
				}
				return rawValue, nil
			}
			val, err := extractJSONPath(req.Name, rawValue, nameJSONPath)
			if err == nil && val != "" {
				m.auditSecretLookup(ctx, req)
			}
			return val, err
		}
		return "", err
	}

	rawValue, ok := resolved[req.Name]
	if !ok {
		return "", fmt.Errorf("failed to resolve secret %q from runtimevar (configured URL could not be fetched — check credentials and network)", req.Name)
	}
	if rawValue == "" {
		logr.Warn("Secret empty from runtimevar, falling back to env var", "req.Name", req.Name)
		rawValue = os.Getenv(req.Name)
		if nameJSONPath == "" {
			if rawValue != "" {
				m.auditSecretLookup(ctx, req)
			}
			return rawValue, nil
		}
		val, err := extractJSONPath(req.Name, rawValue, nameJSONPath)
		if err == nil && val != "" {
			m.auditSecretLookup(ctx, req)
		}
		return val, err
	}

	_, urlJSONPath := stripPathParam(varURL)
	jsonPath := urlJSONPath
	if jsonPath == "" {
		jsonPath = nameJSONPath
	}
	if jsonPath == "" {
		m.auditSecretLookup(ctx, req)
		return rawValue, nil
	}

	val, err := extractJSONPath(req.Name, rawValue, jsonPath)
	if err == nil {
		m.auditSecretLookup(ctx, req)
	}
	return val, err
}

// auditSecretLookup invokes the optional onSecretLookup callback. Call only when
// a secret value is successfully returned (never with the value; name is the logical key).
// A panicking callback is recovered and logged so secret lookups still succeed.
func (m *Manager) auditSecretLookup(ctx context.Context, req GetSecretRequest) {
	if m.onSecretLookup == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.GetLogger(ctx).Warn("secret lookup audit callback panicked", "panic", r, "secret_name", req.Name)
		}
	}()
	m.onSecretLookup(ctx, req)
}

// parseSecretName splits a secret name into its base name and an optional
// gjson path from the "path" query parameter. For example,
// "MY_SECRET?path=db.host" returns ("MY_SECRET", "db.host"). Names without
// a query string return the name unchanged and an empty path. This is used
// for the env-var fallback path where there is no runtimevar URL to embed
// the path in.
func (req GetSecretRequest) parseSecretName() (baspath, jsonPath string) {
	idx := strings.IndexByte(req.Name, '?')
	if idx < 0 {
		return req.Name, ""
	}

	query := req.Name[idx+1:]
	req.Name = req.Name[:idx]

	for _, part := range strings.Split(query, "&") {
		if strings.HasPrefix(part, "path=") {
			jsonPath = strings.TrimPrefix(part, "path=")
			break
		}
	}

	return req.Name, jsonPath
}

// stripPathParam extracts and removes the "path" query parameter from a
// runtimevar URL. The returned cleanURL can be passed directly to
// runtimevar.OpenVariable. A string-based approach is used instead of
// net/url to avoid re-encoding the URL, which could alter values that
// runtimevar backends expect verbatim (e.g. unescaped JSON in constantvar).
func stripPathParam(rawURL string) (cleanURL, jsonPath string) {
	if idx := strings.Index(rawURL, "&path="); idx >= 0 {
		valueStart := idx + len("&path=")
		rest := rawURL[valueStart:]
		if ampIdx := strings.IndexByte(rest, '&'); ampIdx >= 0 {
			return rawURL[:idx] + rest[ampIdx:], rest[:ampIdx]
		}
		return rawURL[:idx], rest
	}

	if idx := strings.Index(rawURL, "?path="); idx >= 0 {
		valueStart := idx + len("?path=")
		rest := rawURL[valueStart:]
		if ampIdx := strings.IndexByte(rest, '&'); ampIdx >= 0 {
			return rawURL[:idx] + "?" + rest[ampIdx+1:], rest[:ampIdx]
		}
		return rawURL[:idx], rest
	}

	return rawURL, ""
}

// resolveFromRuntimevar fetches the raw string value from a runtimevar URL.
// The variable is lazily opened and cached for subsequent calls. Without
// this method, resolveAllSecrets would have to inline the open-cache-read
// logic, making the deduplication harder to follow.
func (m *Manager) resolveFromRuntimevar(ctx context.Context, name, varURL string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, runtimevarTimeout)
	defer cancel()

	v, err := m.getOrOpenVar(timeoutCtx, name, varURL)
	if err != nil {
		return "", err
	}

	snapshot, err := v.Latest(timeoutCtx)
	if err != nil {
		return "", fmt.Errorf("failed to read secret %q from runtimevar %q (check credentials and network): %w", name, varURL, err)
	}

	switch val := snapshot.Value.(type) {
	case string:
		return val, nil
	case []byte:
		return string(val), nil
	default:
		return fmt.Sprintf("%v", val), nil
	}
}

// extractJSONPath uses gjson to pull a value from rawJSON at the given path.
// Returns an error when the path does not match any element in the JSON.
// Without this function, callers would have to manually parse JSON secrets
// even when they only need a single field from a multi-value secret.
func extractJSONPath(secretName, rawJSON, jsonPath string) (string, error) {
	result := gjson.Get(rawJSON, jsonPath)
	if !result.Exists() {
		return "", fmt.Errorf("path %q not found in secret %q", jsonPath, secretName)
	}
	return result.String(), nil
}

// Close cancels the background refresh goroutine and releases all open
// runtimevar.Variable instances. Safe to call multiple times. After Close,
// GetSecret still works — it re-resolves secrets on demand via the
// ttlcache.Item retriever.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for name, v := range m.vars {
		if err := v.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close variable %q: %w", name, err)
		}
		delete(m.vars, name)
	}
	return firstErr
}

// getOrOpenVar returns a cached runtimevar.Variable or opens a new one.
// The runtimevar URL controls the backend, decoder, and any decryption
// settings (e.g. via query parameters like "?decoder=string").
func (m *Manager) getOrOpenVar(ctx context.Context, name, varURL string) (*runtimevar.Variable, error) {
	m.mu.RLock()
	v, ok := m.vars[name]
	m.mu.RUnlock()
	if ok {
		return v, nil
	}

	// Slow path: acquire write lock and open.
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock.
	if v, ok = m.vars[name]; ok {
		return v, nil
	}

	newVar, err := runtimevar.OpenVariable(ctx, varURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open runtimevar for secret %q at %q: %w", name, varURL, err)
	}

	m.vars[name] = newVar
	return newVar, nil
}
