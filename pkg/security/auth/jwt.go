package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
)

// jwtValidator verifies JWTs against one or more trusted OIDC issuers,
// using the coreos/go-oidc library for proper cryptographic verification
// (RSA, EC signature validation via JWKS auto-discovery).
type jwtValidator struct {
	trustedIssuers []string
	audiences      []string // empty = skip audience check

	mu        sync.RWMutex
	verifiers map[string]*oidc.IDTokenVerifier // issuerURL → verifier (lazy)
}

// newJWTValidator creates a validator for the given trusted issuers.
// OIDC provider discovery (JWKS, keys) is done lazily on first token
// validation to avoid blocking server startup on network calls.
func newJWTValidator(cfg JWTConfig) *jwtValidator {
	return &jwtValidator{
		trustedIssuers: cfg.TrustedIssuers,
		audiences:      cfg.AllowedAudiences,
		verifiers:      make(map[string]*oidc.IDTokenVerifier),
	}
}

// Authenticate implements the Authenticator interface.
func (v *jwtValidator) Authenticate(w http.ResponseWriter, r *http.Request) bool {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeJSON(w, http.StatusUnauthorized, "missing_token", "Authorization: Bearer <token> required")
		return false
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if err := v.validate(r.Context(), token); err == nil {
		return true
	}
	writeJSON(w, http.StatusUnauthorized, "invalid_token", "Bearer token validation failed")
	return false
}

// validate checks that the token is a valid JWT signed by one of the trusted issuers.
// Uses go-oidc for full cryptographic signature verification via JWKS.
func (v *jwtValidator) validate(ctx context.Context, token string) error {
	// Peek at the unverified claims to route to the correct verifier.
	issuer, err := peekIssuer(token)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	// Check the issuer is trusted.
	if !v.isTrusted(issuer) {
		return fmt.Errorf("untrusted issuer: %s", issuer)
	}

	// Get or create the verifier for this issuer.
	verifier, err := v.getVerifier(ctx, issuer)
	if err != nil {
		return err
	}

	// Verify the token cryptographically: signature, expiry, issuer match.
	idToken, err := verifier.Verify(ctx, token)
	if err != nil {
		return fmt.Errorf("token verification failed: %w", err)
	}

	// Check audience if configured.
	if len(v.audiences) > 0 && !audienceMatch(idToken.Audience, v.audiences) {
		return fmt.Errorf("audience mismatch: got %v, want one of %v", idToken.Audience, v.audiences)
	}

	return nil
}

// isTrusted checks if the issuer is in the trusted list.
func (v *jwtValidator) isTrusted(issuer string) bool {
	for _, iss := range v.trustedIssuers {
		if iss == issuer {
			return true
		}
	}
	return false
}

// getVerifier returns a cached verifier for the issuer, or creates one
// by performing OIDC discovery (fetching .well-known/openid-configuration).
func (v *jwtValidator) getVerifier(ctx context.Context, issuer string) (*oidc.IDTokenVerifier, error) {
	v.mu.RLock()
	if ver, ok := v.verifiers[issuer]; ok {
		v.mu.RUnlock()
		return ver, nil
	}
	v.mu.RUnlock()

	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check after acquiring write lock.
	if ver, ok := v.verifiers[issuer]; ok {
		return ver, nil
	}

	// Discover the OIDC provider.
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery for %s: %w", issuer, err)
	}

	// Build verifier; we handle audience ourselves for multi-audience support.
	verifier := provider.Verifier(&oidc.Config{
		SkipClientIDCheck: true,
	})

	v.verifiers[issuer] = verifier
	return verifier, nil
}

// peekIssuer extracts the "iss" claim from a JWT without verifying its signature.
// This is safe for routing the token to the correct verifier.
func peekIssuer(token string) (string, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("malformed JWT: expected 3 dot-separated parts")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decoding JWT payload: %w", err)
	}
	var claims struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parsing JWT claims: %w", err)
	}
	if claims.Issuer == "" {
		return "", fmt.Errorf("no issuer (iss) in JWT")
	}
	return claims.Issuer, nil
}

// audienceMatch checks if any token audience matches any allowed audience.
func audienceMatch(tokenAud []string, allowed []string) bool {
	for _, ta := range tokenAud {
		for _, aa := range allowed {
			if ta == aa {
				return true
			}
		}
	}
	return false
}
