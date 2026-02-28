// Package ttlcache provides a TTL-based cache for values that can be refreshed
// on expiry (e.g. OAuth tokens, API metadata).
//
// It solves the problem of caching values with a time-to-live and optional
// refresh: Item wraps a ValueRetriever and returns the cached value until it
// expires, then re-invokes the retriever. Used by Langfuse, MCP, and other
// components that need short-lived cached data. Without this package, each
// would implement its own caching and expiry logic.
package ttlcache
