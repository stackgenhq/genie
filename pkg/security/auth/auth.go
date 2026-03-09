package auth

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security/authcontext"
)

func DemoUser() authcontext.Principal {
	return authcontext.Principal{
		ID:               "demo-user",
		Name:             "Demo User",
		Role:             "demo",
		AuthenticatedVia: "none",
	}
}

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
				ctx := authcontext.WithPrincipal(r.Context(), DemoUser())
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
				ctx = logger.WithArgs(ctx, "principal", p, "request_id", uuid.NewString())
				logger.GetLogger(ctx).Info("user authenticated", "user", p, "ip", getIPAdress(r))
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
func writeJSON(w http.ResponseWriter, status int, code, message, authMethod string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload := map[string]interface{}{
		"error":   code,
		"message": message,
	}
	if authMethod != "" {
		payload["auth_method"] = authMethod
	}
	json.NewEncoder(w).Encode(payload) //nolint:errcheck
}

// getIPAdress extracts the client IP address from an HTTP request.
// It checks proxy headers first (X-Forwarded-For, X-Real-Ip) before falling
// back to the connection's RemoteAddr.
func getIPAdress(r *http.Request) string {
	// X-Forwarded-For may contain a comma-separated list; the first entry
	// is the original client IP.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		if ip != "" {
			return ip
		}
	}

	// X-Real-Ip is set by some reverse proxies (e.g. Nginx).
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to the TCP connection address, stripping the port.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr may already be a bare IP (no port).
		return r.RemoteAddr
	}
	return ip
}
