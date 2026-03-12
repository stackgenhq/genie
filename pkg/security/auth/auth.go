// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/stackgenhq/genie/pkg/identity"
	"github.com/stackgenhq/genie/pkg/logger"
)

// Authenticator defines a pluggable authentication strategy.
// An authenticator is responsible for verifying the request and issuing HTTP
// error responses if the request is unauthorized.
type Authenticator interface {
	// Authenticate inspects the request. Returns a non-nil Sender on success.
	// On failure it must write the appropriate HTTP error to the ResponseWriter
	// and return nil.
	Authenticate(w http.ResponseWriter, r *http.Request) *identity.Sender
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
	log := logger.GetLogger(context.Background())
	if auth == nil {
		// No auth configured → inject a demo sender and pass through.
		var warnOnce sync.Once
		log.Warn("auth: no authentication configured — all requests get DemoSender")
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				warnOnce.Do(func() {
					log.Info("auth: serving first request without authentication",
						"ip", getIPAddress(r), "path", r.URL.Path)
				})
				ctx := identity.WithSender(r.Context(), identity.DemoSender())
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
				ctx := identity.WithSender(r.Context(), *p)
				ctx = logger.WithArgs(ctx, "principal", p, "request_id", uuid.NewString())
				logger.GetLogger(ctx).Info("user authenticated", "user", p, "ip", getIPAddress(r))
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

// writeJSON writes a JSON error response. It does NOT log — use
// writeJSONWithIP when audit/rate-limiting visibility is needed.
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

// writeJSONWithIP writes a JSON error response and logs the auth failure
// with the client's IP address for audit/rate-limiting visibility.
func writeJSONWithIP(w http.ResponseWriter, r *http.Request, status int, code, message, authMethod string) {
	logger.GetLogger(r.Context()).Warn("auth: authentication failed",
		"ip", getIPAddress(r),
		"method", r.Method,
		"path", r.URL.Path,
		"error_code", code,
		"auth_method", authMethod,
		"status", status,
	)
	writeJSON(w, status, code, message, authMethod)
}

// getIPAddress extracts the client IP address from an HTTP request.
// It checks proxy headers first (X-Forwarded-For, X-Real-Ip) before falling
// back to the connection's RemoteAddr.
func getIPAddress(r *http.Request) string {
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
		if ip := strings.TrimSpace(xri); ip != "" {
			return ip
		}
	}

	// Fall back to the TCP connection address, stripping the port.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr may already be a bare IP (no port).
		return r.RemoteAddr
	}
	return ip
}
