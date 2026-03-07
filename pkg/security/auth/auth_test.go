package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/security/auth"
	"github.com/stackgenhq/genie/pkg/security/keyring"
)

// buildJWT creates a minimal unsigned JWT with the given claims for testing.
func buildJWT(header map[string]string, claims map[string]interface{}) string {
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString(headerJSON)
	c := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	return fmt.Sprintf("%s.%s.%s", h, c, sig)
}

var _ = Describe("Auth Package", func() {

	// okHandler is a simple 200 handler we protect with the middleware.
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Helper to make a request through middleware.
	doRequest := func(mw func(http.Handler) http.Handler, req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mw(okHandler).ServeHTTP(rec, req)
		return rec
	}

	Describe("Config", func() {
		It("has zero value with no protection enabled", func() {
			var cfg auth.Config
			Expect(cfg.PasswordProtected).To(BeFalse())
			Expect(cfg.TrustedIssuers).To(BeNil())
			Expect(cfg.AllowedAudiences).To(BeNil())
			Expect(cfg.Password).To(BeEmpty())
		})
	})

	Describe("Middleware", func() {
		Context("when no auth is configured", func() {
			It("passes through all requests", func() {
				mw := auth.Middleware(auth.Config{})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				rec := doRequest(mw, req)
				Expect(rec.Code).To(Equal(http.StatusOK))
			})
		})

		Context("password protection", func() {
			Context("with explicit password in config", func() {
				var mw func(http.Handler) http.Handler
				const password = "my-secret-password"

				BeforeEach(func() {
					mw = auth.Middleware(auth.Config{
						PasswordProtected: true,
						Password:          password,
					})
				})

				It("rejects requests without the password header", func() {
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusUnauthorized))

					var body map[string]string
					Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
					Expect(body["error"]).To(Equal("invalid_password"))
				})

				It("rejects requests with wrong password", func() {
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("X-AGUI-Password", "wrong")
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusUnauthorized))
				})

				It("accepts requests with correct password", func() {
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("X-AGUI-Password", password)
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusOK))
				})

				It("uses constant-time comparison (no timing leak)", func() {
					// We can't directly test constant-time, but we verify
					// that both wrong short and wrong long passwords are rejected.
					for _, pwd := range []string{"", "x", strings.Repeat("a", 100)} {
						req := httptest.NewRequest(http.MethodGet, "/", nil)
						req.Header.Set("X-AGUI-Password", pwd)
						rec := doRequest(mw, req)
						Expect(rec.Code).To(Equal(http.StatusUnauthorized))
					}
				})
			})

			Context("with AGUI_PASSWORD env var", func() {
				const envPassword = "env-secret-pwd"

				BeforeEach(func() {
					os.Setenv("AGUI_PASSWORD", envPassword)
				})
				AfterEach(func() {
					os.Unsetenv("AGUI_PASSWORD")
				})

				It("uses the env var password when config.Password is empty", func() {
					mw := auth.Middleware(auth.Config{PasswordProtected: true})
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("X-AGUI-Password", envPassword)
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusOK))
				})

				It("config password takes priority over env var", func() {
					mw := auth.Middleware(auth.Config{
						PasswordProtected: true,
						Password:          "config-pwd",
					})
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("X-AGUI-Password", "config-pwd")
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusOK))

					// env var password should NOT work
					req2 := httptest.NewRequest(http.MethodGet, "/", nil)
					req2.Header.Set("X-AGUI-Password", envPassword)
					rec2 := doRequest(mw, req2)
					Expect(rec2.Code).To(Equal(http.StatusUnauthorized))
				})
			})

			Context("with keyring password", func() {
				const keyringPwd = "keyring-secret"

				BeforeEach(func() {
					_ = keyring.KeyringSet(keyring.AccountAGUIPassword, []byte(keyringPwd))
				})
				AfterEach(func() {
					_ = keyring.KeyringDelete(keyring.AccountAGUIPassword)
				})

				It("accepts keyring password when config and env are empty", func() {
					mw := auth.Middleware(auth.Config{PasswordProtected: true})
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("X-AGUI-Password", keyringPwd)
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusOK))
				})
			})

			Context("auto-generated password", func() {
				BeforeEach(func() {
					_ = keyring.KeyringDelete(keyring.AccountAGUIPassword)
					os.Unsetenv("AGUI_PASSWORD")
				})

				It("generates a random password and rejects any guess", func() {
					mw := auth.Middleware(auth.Config{PasswordProtected: true})
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("X-AGUI-Password", "random-guess")
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusUnauthorized))
				})

				It("rejects empty password header", func() {
					mw := auth.Middleware(auth.Config{PasswordProtected: true})
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusUnauthorized))
				})
			})
		})

		Context("JWT/OIDC authentication", func() {
			var jwksServer *httptest.Server
			var oidcServer *httptest.Server

			BeforeEach(func() {
				// Fake JWKS server
				jwksServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]interface{}{
						"keys": []map[string]string{
							{"kid": "key-1", "kty": "RSA", "alg": "RS256", "n": "test-n", "e": "test-e"},
							{"kid": "key-2", "kty": "RSA", "alg": "RS256", "n": "test-n2", "e": "test-e2"},
						},
					})
				}))

				// Fake OIDC discovery server
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
					TrustedIssuers: []string{oidcServer.URL},
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
					TrustedIssuers: []string{oidcServer.URL},
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
					TrustedIssuers: []string{oidcServer.URL},
				})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := doRequest(mw, req)
				Expect(rec.Code).To(Equal(http.StatusUnauthorized))
			})

			It("rejects malformed JWT strings", func() {
				mw := auth.Middleware(auth.Config{
					TrustedIssuers: []string{oidcServer.URL},
				})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Bearer not-a-jwt")
				rec := doRequest(mw, req)
				Expect(rec.Code).To(Equal(http.StatusUnauthorized))
			})

			It("accepts valid token from trusted issuer with matching kid", func() {
				token := buildJWT(
					map[string]string{"alg": "RS256", "kid": "key-1"},
					map[string]interface{}{"iss": oidcServer.URL, "exp": time.Now().Add(time.Hour).Unix()},
				)
				mw := auth.Middleware(auth.Config{
					TrustedIssuers: []string{oidcServer.URL},
				})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := doRequest(mw, req)
				Expect(rec.Code).To(Equal(http.StatusOK))
			})

			It("rejects token with unknown kid", func() {
				token := buildJWT(
					map[string]string{"alg": "RS256", "kid": "unknown-key"},
					map[string]interface{}{"iss": oidcServer.URL, "exp": time.Now().Add(time.Hour).Unix()},
				)
				mw := auth.Middleware(auth.Config{
					TrustedIssuers: []string{oidcServer.URL},
				})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := doRequest(mw, req)
				Expect(rec.Code).To(Equal(http.StatusUnauthorized))
			})

			Context("audience validation", func() {
				It("accepts token with matching audience", func() {
					token := buildJWT(
						map[string]string{"alg": "RS256", "kid": "key-1"},
						map[string]interface{}{
							"iss": oidcServer.URL,
							"exp": time.Now().Add(time.Hour).Unix(),
							"aud": "my-app",
						},
					)
					mw := auth.Middleware(auth.Config{
						TrustedIssuers:   []string{oidcServer.URL},
						AllowedAudiences: []string{"my-app"},
					})
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("Authorization", "Bearer "+token)
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusOK))
				})

				It("rejects token with wrong audience", func() {
					token := buildJWT(
						map[string]string{"alg": "RS256", "kid": "key-1"},
						map[string]interface{}{
							"iss": oidcServer.URL,
							"exp": time.Now().Add(time.Hour).Unix(),
							"aud": "other-app",
						},
					)
					mw := auth.Middleware(auth.Config{
						TrustedIssuers:   []string{oidcServer.URL},
						AllowedAudiences: []string{"my-app"},
					})
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("Authorization", "Bearer "+token)
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusUnauthorized))
				})

				It("accepts token with array audience containing a match", func() {
					token := buildJWT(
						map[string]string{"alg": "RS256", "kid": "key-1"},
						map[string]interface{}{
							"iss": oidcServer.URL,
							"exp": time.Now().Add(time.Hour).Unix(),
							"aud": []string{"other", "my-app"},
						},
					)
					mw := auth.Middleware(auth.Config{
						TrustedIssuers:   []string{oidcServer.URL},
						AllowedAudiences: []string{"my-app"},
					})
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("Authorization", "Bearer "+token)
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusOK))
				})

				It("skips audience check when AllowedAudiences is empty", func() {
					token := buildJWT(
						map[string]string{"alg": "RS256", "kid": "key-1"},
						map[string]interface{}{
							"iss": oidcServer.URL,
							"exp": time.Now().Add(time.Hour).Unix(),
							"aud": "any-audience",
						},
					)
					mw := auth.Middleware(auth.Config{
						TrustedIssuers: []string{oidcServer.URL},
					})
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("Authorization", "Bearer "+token)
					rec := doRequest(mw, req)
					Expect(rec.Code).To(Equal(http.StatusOK))
				})
			})
		})

		Context("combined JWT + password", func() {
			var oidcServer *httptest.Server
			var jwksServer *httptest.Server

			BeforeEach(func() {
				jwksServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]interface{}{
						"keys": []map[string]string{
							{"kid": "key-1", "kty": "RSA", "alg": "RS256", "n": "n", "e": "e"},
						},
					})
				}))
				oidcServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/.well-known/openid-configuration" {
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(map[string]string{"jwks_uri": jwksServer.URL, "issuer": oidcServer.URL})
					}
				}))
			})
			AfterEach(func() {
				oidcServer.Close()
				jwksServer.Close()
			})

			It("accepts JWT when both are configured", func() {
				token := buildJWT(
					map[string]string{"alg": "RS256", "kid": "key-1"},
					map[string]interface{}{"iss": oidcServer.URL, "exp": time.Now().Add(time.Hour).Unix()},
				)
				mw := auth.Middleware(auth.Config{
					TrustedIssuers:    []string{oidcServer.URL},
					PasswordProtected: true,
					Password:          "fallback-pwd",
				})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := doRequest(mw, req)
				Expect(rec.Code).To(Equal(http.StatusOK))
			})

			It("falls back to password when no Bearer header is present", func() {
				mw := auth.Middleware(auth.Config{
					TrustedIssuers:    []string{oidcServer.URL},
					PasswordProtected: true,
					Password:          "fallback-pwd",
				})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("X-AGUI-Password", "fallback-pwd")
				rec := doRequest(mw, req)
				Expect(rec.Code).To(Equal(http.StatusOK))
			})

			It("rejects invalid JWT even when password is also provided", func() {
				token := buildJWT(
					map[string]string{"alg": "RS256", "kid": "key-1"},
					map[string]interface{}{"iss": "https://evil.example.com", "exp": time.Now().Add(time.Hour).Unix()},
				)
				mw := auth.Middleware(auth.Config{
					TrustedIssuers:    []string{oidcServer.URL},
					PasswordProtected: true,
					Password:          "fallback-pwd",
				})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("X-AGUI-Password", "fallback-pwd")
				rec := doRequest(mw, req)
				// JWT is checked first; when Bearer header is present, password is NOT checked.
				Expect(rec.Code).To(Equal(http.StatusUnauthorized))
			})

			It("rejects when neither JWT nor password is provided", func() {
				mw := auth.Middleware(auth.Config{
					TrustedIssuers:    []string{oidcServer.URL},
					PasswordProtected: true,
					Password:          "fallback-pwd",
				})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				rec := doRequest(mw, req)
				Expect(rec.Code).To(Equal(http.StatusUnauthorized))
			})
		})

		Context("response format", func() {
			It("returns JSON error responses", func() {
				mw := auth.Middleware(auth.Config{
					PasswordProtected: true,
					Password:          "secret",
				})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				rec := doRequest(mw, req)

				Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
				var body map[string]string
				Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
				Expect(body).To(HaveKey("error"))
				Expect(body).To(HaveKey("message"))
			})
		})
	})
})
