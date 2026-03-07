package auth

import (
	"encoding/json"
	"net/http"

	"github.com/stackgenhq/genie/pkg/security/authcontext"
)

// Authenticator defines a pluggable authentication strategy.
// An authenticator is responsible for verifying the request and issuing HTTP
// error responses if the request is unauthorized.
type Authenticator interface {
	// Authenticate inspects the request. Returns a non-nil Principal on success.
	// On failure it must write the appropriate HTTP error to the ResponseWriter
	// and return nil.
	Authenticate(w http.ResponseWriter, r *http.Request) *authcontext.Principal
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
		// No auth configured → inject a demo principal and pass through.
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := authcontext.WithPrincipal(r.Context(), authcontext.Principal{
					ID:               "demo-user",
					Name:             "Demo User",
					Role:             "demo",
					AuthenticatedVia: "none",
				})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Bypass authentication for CORS preflight OPTIONS requests.
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			if p := auth.Authenticate(w, r); p != nil {
				ctx := authcontext.WithPrincipal(r.Context(), *p)
				next.ServeHTTP(w, r.WithContext(ctx))
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
