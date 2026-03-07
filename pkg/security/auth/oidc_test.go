package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/security/auth"
)

// oidcCfg is a helper to create a Config with OIDC fields set.
func oidcCfg(clientID, clientSecret string, opts ...func(*auth.Config)) auth.Config {
	cfg := auth.Config{
		OIDC: auth.OIDCConfig{
			IssuerURL:    "https://accounts.google.com",
			ClientID:     clientID,
			ClientSecret: clientSecret,
		},
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

var _ = Describe("OIDCHandler", func() {

	Context("NewOIDCHandler", func() {
		It("returns nil when OIDC is not configured", func() {
			handler := auth.NewOIDCHandler(auth.Config{})
			Expect(handler).To(BeNil())
		})

		It("returns a handler when OIDC is configured", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret"))
			Expect(handler).NotTo(BeNil())
		})
	})

	Context("session cookies", func() {
		var handler *auth.OIDCHandler

		BeforeEach(func() {
			cfg := oidcCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OIDC.CookieSecret = "test-cookie-secret-32-bytes-long!"
			})
			handler = auth.NewOIDCHandler(cfg)
		})

		It("returns nil when no cookie is present", func() {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			Expect(handler.ValidateSession(req)).To(BeNil())
		})

		It("rejects tampered cookies", func() {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(&http.Cookie{Name: "genie_session", Value: "tampered.invalid"})
			Expect(handler.ValidateSession(req)).To(BeNil())
		})

		It("rejects empty cookie value", func() {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(&http.Cookie{Name: "genie_session", Value: ""})
			Expect(handler.ValidateSession(req)).To(BeNil())
		})

		It("rejects cookies signed with different key", func() {
			h2 := auth.NewOIDCHandler(oidcCfg("id", "secret", func(c *auth.Config) {
				c.OIDC.CookieSecret = "different-key-that-is-also-long!!"
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(&http.Cookie{Name: "genie_session", Value: "data.wrong-sig"})
			Expect(h2.ValidateSession(req)).To(BeNil())
		})
	})

	Context("HandleAuthInfo", func() {
		It("returns unauthenticated when no session", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OIDC.CookieSecret = "test-cookie-secret-32-bytes-long!"
			}))
			req := httptest.NewRequest(http.MethodGet, "/auth/info", nil)
			rec := httptest.NewRecorder()
			handler.HandleAuthInfo(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			var body map[string]interface{}
			Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
			Expect(body["authenticated"]).To(BeFalse())
			Expect(body["oauth_enabled"]).To(BeTrue())
		})
	})

	Context("HandleLogout", func() {
		It("clears the session cookie", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
			rec := httptest.NewRecorder()
			handler.HandleLogout(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			cookies := rec.Result().Cookies()
			var found bool
			for _, c := range cookies {
				if c.Name == "genie_session" {
					found = true
					Expect(c.MaxAge).To(Equal(-1))
					Expect(c.Value).To(BeEmpty())
				}
			}
			Expect(found).To(BeTrue())
		})

		It("returns JSON response", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
			rec := httptest.NewRecorder()
			handler.HandleLogout(rec, req)

			var body map[string]string
			Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
			Expect(body["error"]).To(Equal("logged_out"))
		})
	})

	Context("HandleLogin", func() {
		It("fails cleanly when provider discovery is unavailable", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OIDC.IssuerURL = "http://localhost:12345/not-found"
				c.OIDC.AllowedDomains = []string{"example.com"}
				c.OIDC.RedirectURL = "https://genie.example.com/auth/callback"
			}))
			req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
			rec := httptest.NewRecorder()
			handler.HandleLogin(rec, req)

			Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		})

		It("does NOT set hd when multiple domains", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OIDC.AllowedDomains = []string{"a.com", "b.com"}
			}))
			req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
			rec := httptest.NewRecorder()
			handler.HandleLogin(rec, req)
			Expect(rec.Header().Get("Location")).NotTo(ContainSubstring("hd="))
		})

	})

	Context("HandleCallback", func() {
		It("rejects without state cookie", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=abc&code=xyz", nil)
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("state cookie"))
		})

		It("rejects mismatching state", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=wrong&code=xyz", nil)
			req.AddCookie(&http.Cookie{Name: "genie_oauth_state", Value: "correct"})
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("CSRF"))
		})

		It("handles error response", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?error=access_denied&state=abc", nil)
			req.AddCookie(&http.Cookie{Name: "genie_oauth_state", Value: "abc"})
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)
			Expect(rec.Code).To(Equal(http.StatusForbidden))
			Expect(rec.Body.String()).To(ContainSubstring("access_denied"))
		})

		It("rejects without code", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=abc", nil)
			req.AddCookie(&http.Cookie{Name: "genie_oauth_state", Value: "abc"})
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("authorization code"))
		})

		It("fails gracefully on discovery network issue", func() {
			handler := auth.NewOIDCHandler(oidcCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OIDC.IssuerURL = "http://localhost:12345/not-found"
			}))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=s&code=c", nil)
			req.AddCookie(&http.Cookie{Name: "genie_oauth_state", Value: "s"})
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)
			Expect(rec.Code).To(Equal(http.StatusInternalServerError))
			Expect(rec.Body.String()).To(ContainSubstring("Failed to initialize OIDC provider"))
		})
	})
})
