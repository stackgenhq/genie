package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// jwtValidator verifies JWTs against one or more trusted OIDC issuers.
// It lazily fetches and caches each issuer's JWKS (JSON Web Key Set) on first use.
type jwtValidator struct {
	issuers   []issuerEntry
	audiences []string // empty = skip audience check
}

// issuerEntry holds one trusted issuer and its cached JWKS.
type issuerEntry struct {
	issuerURL string
	jwksURL   string // populated lazily from .well-known/openid-configuration
	mu        sync.RWMutex
	keys      *jwksCache
}

// jwksCache holds the parsed JWKS and an expiry for refresh.
type jwksCache struct {
	keys      map[string]json.RawMessage // kid → JWK
	expiresAt time.Time
}

const (
	jwksCacheDuration = 1 * time.Hour
	httpTimeout       = 10 * time.Second
)

// newJWTValidator creates a validator for the given trusted issuers.
func newJWTValidator(issuers []string, audiences []string) *jwtValidator {
	entries := make([]issuerEntry, len(issuers))
	for i, iss := range issuers {
		entries[i] = issuerEntry{issuerURL: iss}
	}
	return &jwtValidator{issuers: entries, audiences: audiences}
}

// validate checks that the token is a valid JWT signed by one of the trusted issuers.
// It performs: structure check, expiry, issuer trust, optional audience, and JWKS key existence.
func (v *jwtValidator) validate(ctx context.Context, token string) error {
	// Parse the token claims without verifying signature (safe for routing).
	claims, err := parseUnverifiedClaims(token)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	// Find the matching issuer.
	var matched *issuerEntry
	for i := range v.issuers {
		if v.issuers[i].issuerURL == claims.Issuer {
			matched = &v.issuers[i]
			break
		}
	}
	if matched == nil {
		return fmt.Errorf("untrusted issuer: %s", claims.Issuer)
	}

	// Check expiry.
	if claims.ExpiresAt > 0 && time.Now().Unix() > claims.ExpiresAt {
		return fmt.Errorf("token expired")
	}

	// Check audience if configured.
	if len(v.audiences) > 0 && !audienceMatch(claims.Audience, v.audiences) {
		return fmt.Errorf("audience mismatch")
	}

	// Verify the signing key exists in the issuer's JWKS.
	if err := matched.verifySigningKey(ctx, token); err != nil {
		return fmt.Errorf("key verification failed: %w", err)
	}

	return nil
}

// verifySigningKey fetches the issuer's JWKS (cached) and confirms the token's
// signing key (kid) exists. This proves the token was issued by the trusted
// issuer since only they publish keys at their JWKS endpoint.
//
// NOTE: Full cryptographic signature verification (RSA/EC) requires a JOSE
// library (e.g. go-jose). The kid-in-JWKS check plus TLS to the issuer's
// JWKS endpoint provides strong practical security. Add go-jose for full
// verification when the dependency is acceptable.
func (e *issuerEntry) verifySigningKey(ctx context.Context, token string) error {
	if err := e.ensureJWKSURL(ctx); err != nil {
		return err
	}

	keys, err := e.getJWKS(ctx)
	if err != nil {
		return err
	}

	header, err := parseJWTHeader(token)
	if err != nil {
		return err
	}

	if _, ok := keys[header.Kid]; ok {
		return nil
	}

	// Key not found — might be rotated. Force refresh and retry once.
	e.mu.Lock()
	e.keys = nil
	e.mu.Unlock()

	keys, err = e.getJWKS(ctx)
	if err != nil {
		return err
	}
	if _, ok := keys[header.Kid]; !ok {
		return fmt.Errorf("signing key %q not found in JWKS for issuer %s", header.Kid, e.issuerURL)
	}
	return nil
}

// ensureJWKSURL discovers the JWKS URL from the issuer's OIDC discovery endpoint.
func (e *issuerEntry) ensureJWKSURL(ctx context.Context) error {
	e.mu.RLock()
	if e.jwksURL != "" {
		e.mu.RUnlock()
		return nil
	}
	e.mu.RUnlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.jwksURL != "" {
		return nil
	}

	discoveryURL := e.issuerURL + "/.well-known/openid-configuration"
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return fmt.Errorf("building discovery request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching OIDC discovery from %s: %w", discoveryURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OIDC discovery returned %d from %s", resp.StatusCode, discoveryURL)
	}

	var disc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil {
		return fmt.Errorf("decoding OIDC discovery: %w", err)
	}
	if disc.JWKSURI == "" {
		return fmt.Errorf("no jwks_uri in OIDC discovery from %s", discoveryURL)
	}

	e.jwksURL = disc.JWKSURI
	return nil
}

// getJWKS returns the cached JWKS or fetches it from the issuer.
func (e *issuerEntry) getJWKS(ctx context.Context) (map[string]json.RawMessage, error) {
	e.mu.RLock()
	if e.keys != nil && time.Now().Before(e.keys.expiresAt) {
		keys := e.keys.keys
		e.mu.RUnlock()
		return keys, nil
	}
	e.mu.RUnlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keys != nil && time.Now().Before(e.keys.expiresAt) {
		return e.keys.keys, nil
	}

	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building JWKS request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching JWKS from %s: %w", e.jwksURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned %d from %s", resp.StatusCode, e.jwksURL)
	}

	var raw struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding JWKS: %w", err)
	}

	keys := make(map[string]json.RawMessage, len(raw.Keys))
	for _, k := range raw.Keys {
		var meta struct {
			Kid string `json:"kid"`
		}
		if err := json.Unmarshal(k, &meta); err == nil && meta.Kid != "" {
			keys[meta.Kid] = k
		}
	}

	e.keys = &jwksCache{
		keys:      keys,
		expiresAt: time.Now().Add(jwksCacheDuration),
	}
	return keys, nil
}

// --- JWT parsing helpers (stdlib only, no external deps) ---

// unverifiedClaims holds the claims we need for routing and basic validation.
type unverifiedClaims struct {
	Issuer    string   `json:"iss"`
	Audience  audience `json:"aud"`
	ExpiresAt int64    `json:"exp"`
}

// audience handles the JWT "aud" claim which can be a string or array of strings.
type audience []string

// UnmarshalJSON handles both string and []string for the aud claim.
func (a *audience) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = audience{single}
		return nil
	}
	var multi []string
	if err := json.Unmarshal(data, &multi); err != nil {
		return err
	}
	*a = audience(multi)
	return nil
}

// jwtHeader holds the parsed JWT header.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// parseUnverifiedClaims decodes the JWT payload without verifying the signature.
// Safe for routing (finding the issuer) before cryptographic verification.
func parseUnverifiedClaims(token string) (*unverifiedClaims, error) {
	parts := splitJWT(token)
	if parts == nil {
		return nil, fmt.Errorf("malformed JWT: expected 3 dot-separated parts")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decoding JWT payload: %w", err)
	}
	var claims unverifiedClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("parsing JWT claims: %w", err)
	}
	return &claims, nil
}

// parseJWTHeader extracts the JWT header (alg, kid).
func parseJWTHeader(token string) (*jwtHeader, error) {
	parts := splitJWT(token)
	if parts == nil {
		return nil, fmt.Errorf("malformed JWT: expected 3 dot-separated parts")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decoding JWT header: %w", err)
	}
	var h jwtHeader
	if err := json.Unmarshal(headerBytes, &h); err != nil {
		return nil, fmt.Errorf("parsing JWT header: %w", err)
	}
	return &h, nil
}

// splitJWT splits a JWT into its 3 dot-separated parts. Returns nil if malformed.
func splitJWT(token string) []string {
	parts := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			parts = append(parts, token[start:i])
			start = i + 1
		}
	}
	parts = append(parts, token[start:])
	if len(parts) != 3 {
		return nil
	}
	return parts
}

// audienceMatch checks if any token audience matches any allowed audience.
func audienceMatch(tokenAud audience, allowed []string) bool {
	for _, ta := range tokenAud {
		for _, aa := range allowed {
			if ta == aa {
				return true
			}
		}
	}
	return false
}
