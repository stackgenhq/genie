package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// newAPIKeyAuth returns an Authenticator that validates the request against
// a list of static pre-shared keys.
func newAPIKeyAuth(cfg APIKeyConfig) Authenticator {
	// Pre-convert strings to bytes for subtle.ConstantTimeCompare
	var keys [][]byte
	for _, key := range cfg.Keys {
		if key != "" {
			keys = append(keys, []byte(key))
		}
	}
	return &apiKeyAuth{keys: keys}
}

type apiKeyAuth struct {
	keys [][]byte
}

// Authenticate verifies the presence of an API key in the Authorization: Bearer
// header or X-API-Key header.
func (a *apiKeyAuth) Authenticate(w http.ResponseWriter, r *http.Request) bool {
	// Try Bearer token first
	token := ""
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	}

	// Fallback to custom header
	if token == "" {
		token = r.Header.Get("X-API-Key")
	}

	if token == "" {
		writeJSON(w, http.StatusUnauthorized, "missing_api_key", "API Key required (Authorization: Bearer <key> or X-API-Key: <key>)")
		return false
	}

	tokenBytes := []byte(token)
	for _, key := range a.keys {
		if subtle.ConstantTimeCompare(key, tokenBytes) == 1 {
			return true
		}
	}

	writeJSON(w, http.StatusUnauthorized, "invalid_api_key", "Invalid API Key")
	return false
}
