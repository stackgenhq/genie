package auth

import (
	"encoding/json"
	"net/http"
)

// Authenticator defines a pluggable authentication strategy.
// An authenticator is responsible for verifying the request and issuing HTTP
// error responses if the request is unauthorized.
type Authenticator interface {
	// Authenticate inspects the request. Returns true if authorized.
	// If false, it must write the appropriate HTTP error to the ResponseWriter.
	Authenticate(w http.ResponseWriter, r *http.Request) bool
}

// Middleware returns an http.Handler middleware that enforces authentication
// based on the Config. The user can only choose ONE authentication option.
// Resolution precedence: OIDC > API keys > JWT > Password.
func Middleware(cfg Config, oidcHandler ...*OIDCHandler) func(http.Handler) http.Handler {
	var oh *OIDCHandler
	if len(oidcHandler) > 0 {
		oh = oidcHandler[0]
	}

	auth := resolveAuthenticator(cfg, oh)
	if auth == nil {
		return func(next http.Handler) http.Handler { return next } // no-op passthrough
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Bypass authentication for CORS preflight OPTIONS requests.
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			if auth.Authenticate(w, r) {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// resolveAuthenticator determines which authentication strategy to use.
// It explicitly enforces a single strategy to prevent mixed configurations.
func resolveAuthenticator(cfg Config, oh *OIDCHandler) Authenticator {
	if oh != nil {
		return oh
	}
	if cfg.APIKeys.Enabled() {
		return newAPIKeyAuth(cfg.APIKeys)
	}
	if cfg.JWT.Enabled() {
		return newJWTValidator(cfg.JWT)
	}
	if cfg.Password.Enabled {
		return newPasswordAuth(cfg)
	}
	return nil
}

// writeJSON writes a JSON error response.
func writeJSON(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"error":   code,
		"message": message,
	})
}
