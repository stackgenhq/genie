/*
Copyright © 2026 StackGen, Inc.
*/

package security

import (
	"context"
	"fmt"
	"os"
	"sync"

	"gocloud.dev/runtimevar"

	// Import constantvar driver so "constant://" URLs work in tests and
	// local dev without any external dependencies.
	_ "gocloud.dev/runtimevar/constantvar"
)

// Manager is the primary SecretProvider implementation. It resolves secrets
// from gocloud.dev/runtimevar backends (GCP Secret Manager, AWS Secrets
// Manager, etcd, files, etc.) and falls back to os.Getenv for any secret
// name that has no explicit runtimevar URL mapping.
//
// Manager lazily opens runtimevar.Variable instances on first access and
// caches them for subsequent calls. The runtimevar URL itself controls
// which backend and decoder to use (e.g. "?decoder=string").
//
// Without this struct, the application would have no way to dynamically
// fetch secrets from cloud secret stores at runtime — every secret would
// need to be baked into the process environment before startup.
type Manager struct {
	mu      sync.RWMutex
	vars    map[string]*runtimevar.Variable
	secrets map[string]string // name → runtimevar URL
}

// NewManager creates a Manager from the given Config. If Config.Secrets is
// empty, the Manager still works — it just falls back to os.Getenv for
// every lookup.
func NewManager(cfg Config) *Manager {
	return &Manager{
		vars:    make(map[string]*runtimevar.Variable),
		secrets: cfg.Secrets,
	}
}

// GetSecret retrieves the plaintext value of the named secret. It first
// checks whether a runtimevar URL mapping exists for the given name. If so,
// it lazily opens (and caches) the runtimevar.Variable and returns the
// latest snapshot value. If no mapping exists, it falls back to os.Getenv.
//
// Errors are returned when the runtimevar cannot be opened or its value
// cannot be read. A missing env var returns "" with no error — matching
// the existing behavior callers depend on.
func (m *Manager) GetSecret(ctx context.Context, name string) (string, error) {
	varURL, ok := m.secrets[name]
	if !ok {
		// No runtimevar mapping — fall back to environment variable.
		return os.Getenv(name), nil
	}

	v, err := m.getOrOpenVar(ctx, name, varURL)
	if err != nil {
		return "", err
	}

	snapshot, err := v.Latest(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to read secret %q from runtimevar: %w", name, err)
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

// Close releases all open runtimevar.Variable instances. Safe to call
// multiple times.
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
	// Fast path: check with read lock.
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
