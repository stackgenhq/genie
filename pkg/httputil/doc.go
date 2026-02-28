// Package httputil provides HTTP client and transport utilities used across Genie.
//
// It solves the problem of centralizing TLS configuration (NIST 2030 minimums:
// TLS 1.2+, strong ciphers) so that all outbound HTTP calls use a consistent,
// secure default. SetDefaultTLSConfig is called at startup from config; GetClient
// and NewRoundTripper then use it for MCP, web search, OAuth, and other HTTP
// clients. Without this package, each integration would configure TLS separately
// and security policy would be harder to enforce.
package httputil
