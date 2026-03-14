// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/security/auth"
)

// buildJWT creates a minimal unsigned JWT with the given claims for testing (invalid signature).
func buildJWT(header map[string]string, claims map[string]interface{}) string {
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString(headerJSON)
	c := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	return fmt.Sprintf("%s.%s.%s", h, c, sig)
}

// generateTestJWK creates an RSA key and returns its JWK representation and the private key.
func generateTestJWK() (*rsa.PrivateKey, map[string]interface{}) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)

	return priv, map[string]interface{}{
		"kid": "key-1",
		"kty": "RSA",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
	}
}

// signJWT signs the claims using the provided RSA private key.
func signJWT(priv *rsa.PrivateKey, claims map[string]interface{}) string {
	header := map[string]interface{}{
		"alg": "RS256",
		"kid": "key-1",
		"typ": "JWT",
	}
	hJSON, _ := json.Marshal(header)
	cJSON, _ := json.Marshal(claims)

	msg := base64.RawURLEncoding.EncodeToString(hJSON) + "." + base64.RawURLEncoding.EncodeToString(cJSON)

	hashed := crypto.SHA256.New()
	hashed.Write([]byte(msg))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, hashed.Sum(nil))

	return msg + "." + base64.RawURLEncoding.EncodeToString(sig)
}

var _ = Describe("Auth Middleware", func() {

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	doRequest := func(mw func(http.Handler) http.Handler, req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mw(okHandler).ServeHTTP(rec, req)
		return rec
	}

	Describe("Config", func() {
		It("has zero value with no protection enabled", func() {
			var cfg auth.Config
			Expect(cfg.Password.Enabled).To(BeFalse())
			Expect(cfg.JWT.TrustedIssuers).To(BeNil())
			Expect(cfg.JWT.AllowedAudiences).To(BeNil())
			Expect(cfg.Password.Value).To(BeEmpty())
		})

		It("OIDCConfig.Enabled is false when client ID is missing", func() {
			Expect(auth.OIDCConfig{IssuerURL: "x", ClientSecret: "s"}.Enabled()).To(BeFalse())
		})

		It("OIDCConfig.Enabled is false when client secret is missing", func() {
			Expect(auth.OIDCConfig{IssuerURL: "x", ClientID: "id"}.Enabled()).To(BeFalse())
		})

		It("OIDCConfig.Enabled is false when issuer url is missing", func() {
			Expect(auth.OIDCConfig{ClientID: "id", ClientSecret: "s"}.Enabled()).To(BeFalse())
		})

		It("OIDCConfig.Enabled is true when all are set", func() {
			Expect(auth.OIDCConfig{IssuerURL: "x", ClientID: "id", ClientSecret: "s"}.Enabled()).To(BeTrue())
		})

		It("JWTConfig.Enabled is false when no issuers", func() {
			Expect(auth.JWTConfig{}.Enabled()).To(BeFalse())
		})

		It("JWTConfig.Enabled is true when issuers are set", func() {
			Expect(auth.JWTConfig{TrustedIssuers: []string{"https://accounts.google.com"}}.Enabled()).To(BeTrue())
		})
	})

	Describe("no-op passthrough", func() {
		It("passes through all requests when no auth configured", func() {
			mw := auth.Middleware(auth.Config{})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusOK))
		})

		It("passes through with nil OAuth handler", func() {
			mw := auth.Middleware(auth.Config{}, nil)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusOK))
		})
	})

	Describe("password protection via middleware", func() {
		It("rejects missing password and returns JSON", func() {
			mw := auth.Middleware(auth.Config{
				Password: auth.PasswordConfig{Enabled: true, Value: "secret"},
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := doRequest(mw, req)
			Expect(rec.Code).To(Equal(http.StatusUnauthorized))
			Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))

			var body map[string]string
			Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
			Expect(body["error"]).To(Equal("invalid_password"))
			Expect(body).To(HaveKey("message"))
		})

		It("accepts correct password", func() {
			mw := auth.Middleware(auth.Config{
				Password: auth.PasswordConfig{Enabled: true, Value: "secret"},
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-AGUI-Password", "secret")
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusOK))
		})

		It("uses constant-time comparison", func() {
			mw := auth.Middleware(auth.Config{
				Password: auth.PasswordConfig{Enabled: true, Value: "secret"},
			})
			for _, pwd := range []string{"", "x", strings.Repeat("a", 100)} {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("X-AGUI-Password", pwd)
				Expect(doRequest(mw, req).Code).To(Equal(http.StatusUnauthorized))
			}
		})
	})

	Describe("JWT/OIDC authentication", func() {
		var jwksServer *httptest.Server
		var oidcServer *httptest.Server
		var privateKey *rsa.PrivateKey
		var jwk map[string]interface{}

		BeforeEach(func() {
			privateKey, jwk = generateTestJWK()
			jwksServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"keys": []map[string]interface{}{jwk},
				})
			}))
			oidcServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/openid-configuration" {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]string{
						"jwks_uri": jwksServer.URL,
						"issuer":   oidcServer.URL,
					})
				} else {
					http.NotFound(w, r)
				}
			}))
		})

		AfterEach(func() {
			jwksServer.Close()
			oidcServer.Close()
		})

		It("rejects requests without Authorization header", func() {
			mw := auth.Middleware(auth.Config{
				JWT: auth.JWTConfig{TrustedIssuers: []string{oidcServer.URL}},
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := doRequest(mw, req)
			Expect(rec.Code).To(Equal(http.StatusUnauthorized))

			var body map[string]string
			Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
			Expect(body["error"]).To(Equal("missing_token"))
		})

		It("rejects tokens with untrusted issuer", func() {
			token := buildJWT(
				map[string]string{"alg": "RS256", "kid": "key-1"},
				map[string]interface{}{"iss": "https://evil.example.com", "exp": time.Now().Add(time.Hour).Unix()},
			)
			mw := auth.Middleware(auth.Config{
				JWT: auth.JWTConfig{TrustedIssuers: []string{oidcServer.URL}},
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := doRequest(mw, req)
			Expect(rec.Code).To(Equal(http.StatusUnauthorized))

			var body map[string]string
			Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
			Expect(body["error"]).To(Equal("invalid_token"))
		})

		It("rejects expired tokens", func() {
			token := buildJWT(
				map[string]string{"alg": "RS256", "kid": "key-1"},
				map[string]interface{}{"iss": oidcServer.URL, "exp": time.Now().Add(-time.Hour).Unix()},
			)
			mw := auth.Middleware(auth.Config{
				JWT: auth.JWTConfig{TrustedIssuers: []string{oidcServer.URL}},
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusUnauthorized))
		})

		It("rejects malformed JWT strings", func() {
			mw := auth.Middleware(auth.Config{
				JWT: auth.JWTConfig{TrustedIssuers: []string{oidcServer.URL}},
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer not-a-jwt")
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusUnauthorized))
		})

		It("accepts valid JWTs and injects claims into context", func() {
			token := signJWT(privateKey, map[string]interface{}{
				"iss":        oidcServer.URL,
				"exp":        time.Now().Add(time.Hour).Unix(),
				"iat":        time.Now().Unix(),
				"aud":        "test-client",
				"sub":        "user123",
				"email":      "test@example.com",
				"name":       "Test User",
				"roles":      "admin",
				"department": "engineering",
			})
			mw := auth.Middleware(auth.Config{
				JWT: auth.JWTConfig{TrustedIssuers: []string{oidcServer.URL}},
			})

			var capturedClaims map[string]any

			// We need a custom handler to capture the context
			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedClaims = auth.GetClaims(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)

			rec := httptest.NewRecorder()
			mw(testHandler).ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(capturedClaims).NotTo(BeNil())
			Expect(capturedClaims["roles"]).To(Equal("admin"))
			Expect(capturedClaims["email"]).To(Equal("test@example.com"))
			Expect(capturedClaims["department"]).To(Equal("engineering"))
		})
	})

	Describe("Middleware with OIDC", func() {
		It("returns oauth_enabled in 401 when OIDC + password configured", func() {
			oh := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OIDC.CookieSecret = "test-cookie-secret-32-bytes-long!"
			}))
			mw := auth.Middleware(auth.Config{
				Password: auth.PasswordConfig{Enabled: true, Value: "test-pwd"},
				OIDC:     auth.OIDCConfig{IssuerURL: "https://test", ClientID: "test-id", ClientSecret: "test-secret"},
			}, oh)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := doRequest(mw, req)
			Expect(rec.Code).To(Equal(http.StatusUnauthorized))

			var body map[string]interface{}
			Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
			Expect(body["oauth_enabled"]).To(BeTrue())
			Expect(body["login_url"]).To(Equal("/auth/login"))
		})

		It("returns missing_token when only OIDC configured", func() {
			oh := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OIDC.CookieSecret = "test-cookie-secret-32-bytes-long!"
			}))
			mw := auth.Middleware(auth.Config{
				OIDC: auth.OIDCConfig{IssuerURL: "https://test", ClientID: "test-id", ClientSecret: "test-secret"},
			}, oh)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := doRequest(mw, req)
			Expect(rec.Code).To(Equal(http.StatusUnauthorized))

			var body map[string]interface{}
			Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
			Expect(body["error"]).To(Equal("missing_token"))
			Expect(body["oauth_enabled"]).To(BeTrue())
		})
	})
})
