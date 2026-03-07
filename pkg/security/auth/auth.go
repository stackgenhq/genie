// Package auth provides HTTP authentication middleware for the AG-UI server.
// It supports multiple authentication strategies that can be used independently
// or combined:
//
//   - Password: simple shared-secret via header (dev / quick cloud deployments)
//   - JWT/OIDC: OAuth2 bearer tokens validated against trusted issuers (production)
//
// When both are configured, JWT is checked first; password is the fallback.
// When neither is configured but protection is requested, a random password
// is auto-generated and logged so the operator can retrieve it from container logs.
//
// Usage:
//
//	cfg := auth.Config{Password: auth.PasswordConfig{Enabled: true, Value: "s3cret"}}
//	mw := auth.NewMiddleware(cfg)
//	router.Use(mw)
package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// Middleware returns an http.Handler middleware that enforces authentication
// based on the given Config. When no protection is configured, it returns a
// no-op passthrough. The caller does not need to know which strategy is active.
//
// Resolution order per request:
//  1. If OAuth session cookie is valid → pass through (user already logged in)
//  2. If trusted OIDC issuers are configured → validate Authorization: Bearer <jwt>
//  3. If password protection is enabled → validate X-AGUI-Password header or ?password= query
//  4. Otherwise → pass through (no auth)
func Middleware(cfg Config, oauthHandler ...*OAuthHandler) func(http.Handler) http.Handler {
	var oh *OAuthHandler
	if len(oauthHandler) > 0 {
		oh = oauthHandler[0]
	}
	guard := newGuard(cfg, oh)
	if guard == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return guard.middleware
}

// guard holds the resolved authentication state. Built once at startup.
type guard struct {
	// password is the resolved password bytes (may be from config, env, keyring, or auto-generated).
	password []byte
	// jwtValidator handles OIDC/JWT verification against trusted issuers. Nil when not configured.
	jwtValidator *jwtValidator
	// oauthHandler validates OAuth session cookies. Nil when OAuth is not configured.
	oauthHandler *OAuthHandler
}

// newGuard creates a guard from the config. Returns nil when no protection is needed.
func newGuard(cfg Config, oh *OAuthHandler) *guard {
	hasJWT := cfg.JWT.Enabled()
	hasPassword := cfg.Password.Enabled
	hasOAuth := oh != nil

	if !hasJWT && !hasPassword && !hasOAuth {
		return nil
	}

	g := &guard{}

	// Set up OAuth session validation.
	if hasOAuth {
		g.oauthHandler = oh
	}

	// Set up JWT validation if issuers are configured.
	if hasJWT {
		g.jwtValidator = newJWTValidator(cfg.JWT)
	}

	// Resolve password (config → env → keyring → auto-generate).
	if hasPassword {
		g.password = resolvePassword(cfg)
	}

	return g
}

// middleware is the http.Handler middleware that enforces authentication.
func (g *guard) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Check OAuth session cookie first (browser SSO).
		if g.oauthHandler != nil {
			if session := g.oauthHandler.ValidateSession(r); session != nil {
				next.ServeHTTP(w, r)
				return
			}
		}

		// 2. Try JWT (Authorization: Bearer <token>).
		if g.jwtValidator != nil {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimPrefix(authHeader, "Bearer ")
				if err := g.jwtValidator.validate(r.Context(), token); err == nil {
					next.ServeHTTP(w, r)
					return
				}
				// JWT present but invalid → reject immediately.
				writeJSON(w, http.StatusUnauthorized, "invalid_token", "Bearer token validation failed")
				return
			}
			// No Bearer header but JWT is configured. Fall through to password
			// if also configured; otherwise check OAuth or reject.
			if len(g.password) == 0 && g.oauthHandler == nil {
				writeJSON(w, http.StatusUnauthorized, "missing_token", "Authorization: Bearer <token> required")
				return
			}
		}

		// 3. Try password (X-AGUI-Password header or ?password= query param).
		if len(g.password) > 0 {
			provided := r.Header.Get("X-AGUI-Password")
			if provided == "" {
				provided = r.URL.Query().Get("password")
			}
			if provided != "" && subtle.ConstantTimeCompare(g.password, []byte(provided)) == 1 {
				next.ServeHTTP(w, r)
				return
			}

			// If OAuth is configured, send a special response so the UI knows to offer login.
			if g.oauthHandler != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
					"error":         "auth_required",
					"message":       "Authentication required",
					"oauth_enabled": true,
					"login_url":     "/auth/login",
				})
				return
			}

			writeJSON(w, http.StatusUnauthorized, "invalid_password", "Password required to connect")
			return
		}

		// 4. Only OAuth is configured but no session → redirect hint.
		if g.oauthHandler != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"error":         "auth_required",
				"message":       "Authentication required",
				"oauth_enabled": true,
				"login_url":     "/auth/login",
			})
			return
		}

		// Should not reach here (newGuard returns nil for no-auth), but be safe.
		next.ServeHTTP(w, r)
	})
}

// writeJSON writes a JSON error response.
func writeJSON(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message}) //nolint:errcheck
}
