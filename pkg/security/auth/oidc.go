// Package auth – oidc.go implements the generic OIDC browser login flow
// using golang.org/x/oauth2 and coreos/go-oidc.
//
//	GET  /auth/login    → redirects to the Provider consent screen
//	GET  /auth/callback → exchanges code for ID token, validates, creates session cookie
//	POST /auth/logout   → clears session cookie
//
// The session is a signed cookie containing {email, domain, exp}. No server-side
// session store is required — the cookie is self-contained and HMAC-SHA256 signed.
package auth

import (
	"context"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/stackgenhq/genie/pkg/security/authcontext"
	"golang.org/x/oauth2"
)

const (
	// sessionCookieName is the name of the session cookie.
	sessionCookieName = "genie_session"

	// sessionMaxAge is how long the session cookie is valid (24 hours).
	sessionMaxAge = 24 * time.Hour

	// stateCookieName stores the CSRF state for the OAuth flow.
	stateCookieName = "genie_oauth_state"

	// cookieSecretEnv is the env var for the cookie signing secret.
	cookieSecretEnv = "AGUI_COOKIE_SECRET"
)

// OIDCHandler manages the OIDC login flow and session cookies.
type OIDCHandler struct {
	issuerURL      string
	clientID       string
	clientSecret   string
	redirectURL    string // may be empty → auto-detected from request
	allowedDomains []string
	cookieKey      []byte // HMAC-SHA256 key for signing session cookies

	// OIDC provider for verifying ID tokens (lazy init).
	providerOnce sync.Once
	provider     *oidc.Provider
}

// sessionPayload is the JSON structure stored inside the signed session cookie.
type sessionPayload struct {
	Email     string `json:"email"`
	Domain    string `json:"hd,omitempty"`
	ExpiresAt int64  `json:"exp"`
}

// NewOIDCHandler creates an OIDCHandler from the given Config.
// Returns nil if OIDC is not configured.
func NewOIDCHandler(cfg Config) *OIDCHandler {
	if !cfg.OIDC.Enabled() {
		return nil
	}
	return &OIDCHandler{
		issuerURL:      cfg.OIDC.IssuerURL,
		clientID:       cfg.OIDC.ClientID,
		clientSecret:   cfg.OIDC.ClientSecret,
		redirectURL:    cfg.OIDC.RedirectURL,
		allowedDomains: cfg.OIDC.AllowedDomains,
		cookieKey:      resolveCookieSecret(cfg.OIDC.CookieSecret),
	}
}

// Authenticate implements the Authenticator interface.
func (h *OIDCHandler) Authenticate(w http.ResponseWriter, r *http.Request) *authcontext.Principal {
	if session := h.ValidateSession(r); session != nil {
		return &authcontext.Principal{
			ID:               session.Email,
			Name:             session.Email,
			Role:             "user",
			AuthenticatedVia: "oidc",
		}
	}
	// Return a special JSON payload the UI uses to offer login
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"error":         "missing_token",
		"message":       "OIDC login required. Use login_url to start the browser-based login flow; subsequent requests must include the session cookie.",
		"oauth_enabled": true,
		"login_url":     "/auth/login",
		"auth_method":   "oidc",
	})
	return nil
}

// resolveCookieSecret resolves the cookie signing key from config, env, or auto-generates one.
func resolveCookieSecret(configured string) []byte {
	if configured != "" {
		return []byte(configured)
	}
	if env := os.Getenv(cookieSecretEnv); env != "" {
		return []byte(env)
	}
	// Auto-generate an ephemeral key (sessions won't survive restarts).
	buf := make([]byte, 32)
	if _, err := cryptorand.Read(buf); err != nil {
		panic(fmt.Sprintf("auth/oidc: failed to generate cookie secret: %v", err))
	}
	return buf
}

// getProvider returns the OIDC provider (lazily initialized).
func (h *OIDCHandler) getProvider(ctx context.Context) (*oidc.Provider, error) {
	var initErr error
	h.providerOnce.Do(func() {
		h.provider, initErr = oidc.NewProvider(ctx, h.issuerURL)
	})
	if initErr != nil {
		return nil, fmt.Errorf("OIDC provider init: %w", initErr)
	}
	if h.provider == nil {
		return nil, fmt.Errorf("OIDC provider init: provider is nil")
	}
	return h.provider, nil
}

// oauth2Config builds the oauth2.Config for a given request.
func (h *OIDCHandler) oauth2Config(ctx context.Context, r *http.Request) (*oauth2.Config, error) {
	provider, err := h.getProvider(ctx)
	if err != nil {
		return nil, err
	}
	return &oauth2.Config{
		ClientID:     h.clientID,
		ClientSecret: h.clientSecret,
		RedirectURL:  h.getRedirectURL(r),
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}, nil
}

// HandleLogin redirects the user to the provider's consent screen.
func (h *OIDCHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	csrf := generateState()

	// Store CSRF state in a short-lived cookie for protection.
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    csrf,
		Path:     "/auth",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})

	// Encode CSRF and return_to into the OAuth state parameter.
	returnTo := r.URL.Query().Get("return_to")
	statePayload := map[string]string{"csrf": csrf}
	if returnTo != "" {
		statePayload["return_to"] = returnTo
	}
	stateBytes, err := json.Marshal(statePayload)
	if err != nil {
		http.Error(w, "Failed to encode state: "+err.Error(), http.StatusInternalServerError)
		return
	}
	oauthState := base64.RawURLEncoding.EncodeToString(stateBytes)

	cfg, err := h.oauth2Config(r.Context(), r)
	if err != nil {
		http.Error(w, "Failed to initialize OIDC provider: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build the auth URL with optional domain hint (often supported by Google/Okta via "hd" or "domain_hint").
	opts := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOnline,
	}

	// Some providers use `prompt=select_account` to ensure UI shows up reliably.
	// This is generic enough, but is typically safe to include.
	if strings.Contains(h.issuerURL, "google.com") {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "select_account"))
	}

	if len(h.allowedDomains) == 1 {
		// "hd" is specifically Google Workspace, but standard enough to pass harmlessly to others.
		opts = append(opts, oauth2.SetAuthURLParam("hd", h.allowedDomains[0]))
	}

	http.Redirect(w, r, cfg.AuthCodeURL(oauthState, opts...), http.StatusFound)
}

// HandleCallback processes the OAuth callback from the provider.
func (h *OIDCHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	// Verify CSRF state.
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" {
		http.Error(w, "Missing OAuth state cookie. Please try logging in again.", http.StatusBadRequest)
		return
	}
	// Decode returned OAuth state parameter.
	encodedState := r.URL.Query().Get("state")
	stateBytes, err := base64.RawURLEncoding.DecodeString(encodedState)
	if err != nil {
		http.Error(w, "Invalid OAuth state format. Please try logging in again.", http.StatusBadRequest)
		return
	}
	var statePayload map[string]string
	if err := json.Unmarshal(stateBytes, &statePayload); err != nil {
		http.Error(w, "Invalid OAuth state payload. Please try logging in again.", http.StatusBadRequest)
		return
	}

	if statePayload["csrf"] != stateCookie.Value {
		http.Error(w, "Invalid OAuth state (possible CSRF). Please try logging in again.", http.StatusBadRequest)
		return
	}

	// Clear the state cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
	})

	returnTo := "/ui/chat.html"
	if rUrl, ok := statePayload["return_to"]; ok && rUrl != "" {
		returnTo = sanitizeReturnTo(rUrl)
	}

	// Check for errors from provider.
	if errCode := r.URL.Query().Get("error"); errCode != "" {
		http.Error(w, "OAuth error: "+errCode, http.StatusForbidden)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	cfg, err := h.oauth2Config(r.Context(), r)
	if err != nil {
		http.Error(w, "Failed to initialize OIDC provider: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Exchange the authorization code for tokens using golang.org/x/oauth2.
	token, err := cfg.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "Token exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Extract the ID token from the oauth2 token response.
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, "No ID token in token response", http.StatusInternalServerError)
		return
	}

	// Verify the ID token cryptographically using go-oidc.
	provider, err := h.getProvider(r.Context())
	if err != nil {
		http.Error(w, "OIDC provider unavailable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: h.clientID})
	idToken, err := verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "ID token verification failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Extract claims from the verified token.
	var claims struct {
		Email  string `json:"email"`
		Domain string `json:"hd"` // Google Workspace hosted domain, or generic equivalent
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "Failed to parse ID token claims: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if claims.Email == "" {
		http.Error(w, "No email in ID token", http.StatusInternalServerError)
		return
	}

	// Verify domain restriction (only when allowed_domains is configured).
	// When no allowed domains are set, any authenticated user is accepted.
	if len(h.allowedDomains) > 0 {
		if !isDomainAllowed(claims.Domain, h.allowedDomains) && !isDomainAllowed(claims.Email, h.allowedDomains) {
			domainError := claims.Domain
			if domainError == "" {
				parts := strings.Split(claims.Email, "@")
				if len(parts) == 2 {
					domainError = parts[1]
				}
			}
			http.Error(w, fmt.Sprintf("Access denied: domain %q is not in the allowed list", domainError), http.StatusForbidden)
			return
		}
	}

	// Create session cookie.
	session := sessionPayload{
		Email:     claims.Email,
		Domain:    claims.Domain,
		ExpiresAt: time.Now().Add(sessionMaxAge).Unix(),
	}
	cookieValue, err := h.signSession(session)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    cookieValue,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to the original URL or chat UI.
	http.Redirect(w, r, returnTo, http.StatusFound)
}

// HandleLogout clears the session cookie.
func (h *OIDCHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
	})
	writeJSON(w, http.StatusOK, "logged_out", "Session cleared", "oidc")
}

// ValidateSession checks if the request has a valid session cookie.
// Returns the session payload if valid, nil otherwise.
func (h *OIDCHandler) ValidateSession(r *http.Request) *sessionPayload {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	session, err := h.verifySession(cookie.Value)
	if err != nil {
		return nil
	}
	if time.Now().Unix() > session.ExpiresAt {
		return nil
	}
	return session
}

// HandleAuthInfo returns the current user's session info (for the UI to display).
func (h *OIDCHandler) HandleAuthInfo(w http.ResponseWriter, r *http.Request) {
	session := h.ValidateSession(r)
	if session == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"authenticated": false,
			"oauth_enabled": true,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"authenticated": true,
		"oauth_enabled": true,
		"email":         session.Email,
		"domain":        session.Domain,
	})
}

// ── Cookie signing/verification ──

// signSession creates an HMAC-signed cookie value from the session payload.
// Format: base64(json) + "." + base64(hmac-sha256)
func (h *OIDCHandler) signSession(s sessionPayload) (string, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	sig := h.hmacSign([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// verifySession verifies and decodes an HMAC-signed cookie value.
func (h *OIDCHandler) verifySession(value string) (*sessionPayload, error) {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed session cookie")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid signature encoding")
	}
	expected := h.hmacSign([]byte(parts[0]))
	if !hmac.Equal(sig, expected) {
		return nil, fmt.Errorf("invalid signature")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid payload encoding")
	}
	var s sessionPayload
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	return &s, nil
}

func (h *OIDCHandler) hmacSign(data []byte) []byte {
	mac := hmac.New(sha256.New, h.cookieKey)
	mac.Write(data) //nolint:errcheck
	return mac.Sum(nil)
}

// ── Helpers ──

// generateState creates a random CSRF state string.
func generateState() string {
	buf := make([]byte, 16)
	if _, err := cryptorand.Read(buf); err != nil {
		panic(fmt.Sprintf("auth/oidc: failed to generate state: %v", err))
	}
	return hex.EncodeToString(buf)
}

// getRedirectURL returns the callback URL. Uses configured value or auto-detects from the request.
func (h *OIDCHandler) getRedirectURL(r *http.Request) string {
	if h.redirectURL != "" {
		return h.redirectURL
	}
	scheme := "http"
	if isSecureRequest(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/auth/callback"
}

// isSecureRequest returns true if the request came over TLS or via a reverse proxy with HTTPS.
func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// isDomainAllowed checks if the user's OIDC domain or email is in the allow list.
func isDomainAllowed(val string, allowed []string) bool {
	if val == "" {
		return false
	}
	for _, d := range allowed {
		if strings.EqualFold(val, d) {
			return true
		}
		// Also match suffix for email-based domains (e.g. user@stackgen.com matches stackgen.com)
		if strings.HasSuffix(strings.ToLower(val), "@"+strings.ToLower(d)) {
			return true
		}
	}
	return false
}

// sanitizeReturnTo validates and sanitizes the return_to redirect path to prevent
// open redirect vulnerabilities. It decodes URL-encoded characters, rejects backslashes
// and control characters, and ensures the result is a safe local path.
// Without this function, an attacker could use URL-encoded sequences (e.g. /%5C%5Cevil.com)
// to bypass prefix-only checks and redirect users to malicious domains.
func sanitizeReturnTo(raw string) string {
	const fallback = "/ui/chat.html"
	if raw == "" {
		return fallback
	}

	// Decode percent-encoded characters to catch encoded bypass attacks.
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return fallback
	}

	// Reject any backslashes (some user-agents normalize \ to /).
	if strings.ContainsAny(decoded, "\\") {
		return fallback
	}

	// Reject control characters (ASCII 0-31, 127).
	for _, c := range decoded {
		if c < 0x20 || c == 0x7f {
			return fallback
		}
	}

	// Must start with "/" and the second character must NOT be "/" or "\" to
	// prevent protocol-relative redirects (e.g. "//evil.com" or "/\evil.com").
	if !strings.HasPrefix(decoded, "/") || (len(decoded) >= 2 && (decoded[1] == '/' || decoded[1] == '\\')) {
		return fallback
	}

	return decoded
}
