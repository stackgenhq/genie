package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/security/auth"
)

// oauthCfg is a helper to create a Config with OAuth fields set.
func oauthCfg(clientID, clientSecret string, opts ...func(*auth.Config)) auth.Config {
	cfg := auth.Config{
		OAuth: auth.OAuthConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
		},
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

var _ = Describe("OAuthHandler", func() {

	Context("NewOAuthHandler", func() {
		It("returns nil when OAuth is not configured", func() {
			handler := auth.NewOAuthHandler(auth.Config{})
			Expect(handler).To(BeNil())
		})

		It("returns a handler when OAuth is configured", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			Expect(handler).NotTo(BeNil())
		})
	})

	Context("session cookies", func() {
		var handler *auth.OAuthHandler

		BeforeEach(func() {
			cfg := oauthCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OAuth.CookieSecret = "test-cookie-secret-32-bytes-long!"
			})
			handler = auth.NewOAuthHandler(cfg)
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
			h2 := auth.NewOAuthHandler(oauthCfg("id", "secret", func(c *auth.Config) {
				c.OAuth.CookieSecret = "different-key-that-is-also-long!!"
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(&http.Cookie{Name: "genie_session", Value: "data.wrong-sig"})
			Expect(h2.ValidateSession(req)).To(BeNil())
		})
	})

	Context("HandleAuthInfo", func() {
		It("returns unauthenticated when no session", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OAuth.CookieSecret = "test-cookie-secret-32-bytes-long!"
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
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
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
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
			rec := httptest.NewRecorder()
			handler.HandleLogout(rec, req)

			var body map[string]string
			Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
			Expect(body["error"]).To(Equal("logged_out"))
		})
	})

	Context("HandleLogin", func() {
		It("redirects to Google OAuth with CSRF cookie", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OAuth.AllowedDomains = []string{"example.com"}
				c.OAuth.RedirectURL = "https://genie.example.com/auth/callback"
			}))
			req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
			rec := httptest.NewRecorder()
			handler.HandleLogin(rec, req)

			Expect(rec.Code).To(Equal(http.StatusFound))
			location := rec.Header().Get("Location")
			Expect(location).To(ContainSubstring("accounts.google.com"))
			Expect(location).To(ContainSubstring("client_id=test-id"))
			Expect(location).To(ContainSubstring("hd=example.com"))
			Expect(location).To(ContainSubstring("scope=openid"))

			cookies := rec.Result().Cookies()
			var stateCookie *http.Cookie
			for _, c := range cookies {
				if c.Name == "genie_oauth_state" {
					stateCookie = c
				}
			}
			Expect(stateCookie).NotTo(BeNil())
			Expect(stateCookie.HttpOnly).To(BeTrue())
			Expect(stateCookie.Value).NotTo(BeEmpty())
		})

		It("does NOT set hd when multiple domains", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret", func(c *auth.Config) {
				c.OAuth.AllowedDomains = []string{"a.com", "b.com"}
			}))
			req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
			rec := httptest.NewRecorder()
			handler.HandleLogin(rec, req)
			Expect(rec.Header().Get("Location")).NotTo(ContainSubstring("hd="))
		})

		It("does NOT set hd when no domains", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
			rec := httptest.NewRecorder()
			handler.HandleLogin(rec, req)
			Expect(rec.Header().Get("Location")).NotTo(ContainSubstring("hd="))
		})

		It("auto-detects redirect URL from Host", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
			req.Host = "genie.mycompany.com"
			rec := httptest.NewRecorder()
			handler.HandleLogin(rec, req)
			location := rec.Header().Get("Location")
			Expect(location).To(ContainSubstring("genie.mycompany.com"))
		})

		It("uses HTTPS when X-Forwarded-Proto is set", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
			req.Host = "genie.mycompany.com"
			req.Header.Set("X-Forwarded-Proto", "https")
			rec := httptest.NewRecorder()
			handler.HandleLogin(rec, req)
			location := rec.Header().Get("Location")
			Expect(location).To(ContainSubstring("redirect_uri=https"))
		})

		It("sets CSRF state cookie as Secure when behind HTTPS proxy", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
			req.Host = "genie.mycompany.com"
			req.Header.Set("X-Forwarded-Proto", "https")
			rec := httptest.NewRecorder()
			handler.HandleLogin(rec, req)

			var stateCookie *http.Cookie
			for _, c := range rec.Result().Cookies() {
				if c.Name == "genie_oauth_state" {
					stateCookie = c
				}
			}
			Expect(stateCookie).NotTo(BeNil())
			Expect(stateCookie.Secure).To(BeTrue())
		})
	})

	Context("HandleCallback", func() {
		It("rejects without state cookie", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=abc&code=xyz", nil)
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("state cookie"))
		})

		It("rejects mismatching state", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=wrong&code=xyz", nil)
			req.AddCookie(&http.Cookie{Name: "genie_oauth_state", Value: "correct"})
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("CSRF"))
		})

		It("handles Google error response", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?error=access_denied&state=abc", nil)
			req.AddCookie(&http.Cookie{Name: "genie_oauth_state", Value: "abc"})
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)
			Expect(rec.Code).To(Equal(http.StatusForbidden))
			Expect(rec.Body.String()).To(ContainSubstring("access_denied"))
		})

		It("rejects without code", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=abc", nil)
			req.AddCookie(&http.Cookie{Name: "genie_oauth_state", Value: "abc"})
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("authorization code"))
		})

		It("fails gracefully on token exchange", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=s&code=c", nil)
			req.AddCookie(&http.Cookie{Name: "genie_oauth_state", Value: "s"})
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)
			Expect(rec.Code).To(Equal(http.StatusInternalServerError))
			Expect(rec.Body.String()).To(ContainSubstring("Token exchange failed"))
		})

		It("clears the state cookie on callback", func() {
			handler := auth.NewOAuthHandler(oauthCfg("test-id", "test-secret"))
			req := httptest.NewRequest(http.MethodGet, "/auth/callback?error=access_denied&state=abc", nil)
			req.AddCookie(&http.Cookie{Name: "genie_oauth_state", Value: "abc"})
			rec := httptest.NewRecorder()
			handler.HandleCallback(rec, req)

			var cleared bool
			for _, c := range rec.Result().Cookies() {
				if c.Name == "genie_oauth_state" && c.MaxAge == -1 {
					cleared = true
				}
			}
			Expect(cleared).To(BeTrue())
		})
	})
})
