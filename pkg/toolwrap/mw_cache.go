// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap

import (
	"context"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/ttlcache"
	"github.com/tidwall/gjson"
)

// semanticCacheTTL is how long a semantic cache entry stays valid.
const semanticCacheTTL = 120 * time.Second

// maxSemanticCacheSize limits the number of entries.
const maxSemanticCacheSize = 128

// semanticCacheMiddleware deduplicates idempotent tool calls by caching
// results keyed on semantic identity fields. The key fields are fully
// configurable — callers supply a map of tool name → JSON field names
// that form the tool's identity. Cached entries expire after
// semanticCacheTTL.
//
// Example: for {"create_recurring_task": ["name"]}, a second call with
// the same "name" value returns the cached result without executing.
type semanticCacheMiddleware struct {
	mu        sync.Mutex
	cache     *ttlcache.TTLMap[any] // TTL-bounded result cache
	keyFields map[string][]string   // tool → JSON fields forming the identity
}

// SemanticCacheMiddleware creates a new semantic dedup middleware.
// keyFields maps tool names to the JSON argument fields that form
// the semantic identity of a call. Only tools in this map are eligible
// for caching; all others pass through.
//
// Example:
//
//	SemanticCacheMiddleware(map[string][]string{
//	    "create_recurring_task": {"name"},
//	})
func SemanticCacheMiddleware(keyFields map[string][]string) Middleware {
	if keyFields == nil {
		keyFields = map[string][]string{}
	}
	return &semanticCacheMiddleware{
		cache:     ttlcache.NewTTLMap[any](maxSemanticCacheSize, semanticCacheTTL),
		keyFields: keyFields,
	}
}

func (m *semanticCacheMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		logr := logger.GetLogger(ctx).With("fn", "SemanticCacheMiddleware", "tool", tc.ToolName)

		semKey, eligible := m.semanticKey(tc.ToolName, tc.Args)
		if eligible {
			m.mu.Lock()
			cached, hit := m.cache.Get(semKey)
			m.mu.Unlock()
			if hit {
				logr.Debug("semantic cache hit — returning cached tool result", "key", semKey)
				return cached, nil
			}
		}

		output, err := next(ctx, tc)

		if err == nil && eligible {
			m.mu.Lock()
			m.cache.Set(semKey, output)
			m.mu.Unlock()
			logr.Debug("semantic cache stored", "key", semKey, "ttl", semanticCacheTTL.String())
		}
		return output, err
	}
}

// semanticKey builds a dedup key from the configured key fields.
func (m *semanticCacheMiddleware) semanticKey(toolName string, jsonArgs []byte) (string, bool) {
	fields, ok := m.keyFields[toolName]
	if !ok {
		return "", false
	}
	key := toolName
	for _, f := range fields {
		val := gjson.GetBytes(jsonArgs, f)
		if !val.Exists() {
			return "", false
		}
		key += ":" + val.String()
	}
	return key, true
}
