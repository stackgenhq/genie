package auth_test

import (
	"net/http"
	"net/http/httptest"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/security/auth"
	"github.com/stackgenhq/genie/pkg/security/keyring"
)

var _ = Describe("Password Resolution", func() {

	// okHandler is a simple 200 handler we protect with the middleware.
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	doRequest := func(mw func(http.Handler) http.Handler, req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mw(okHandler).ServeHTTP(rec, req)
		return rec
	}

	Context("explicit config value", func() {
		It("accepts correct password", func() {
			mw := auth.Middleware(auth.Config{
				Password: auth.PasswordConfig{Enabled: true, Value: "my-secret"},
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-AGUI-Password", "my-secret")
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusOK))
		})

		It("rejects wrong password", func() {
			mw := auth.Middleware(auth.Config{
				Password: auth.PasswordConfig{Enabled: true, Value: "my-secret"},
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-AGUI-Password", "wrong")
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusUnauthorized))
		})
	})

	Context("AGUI_PASSWORD env var", func() {
		const envPassword = "env-secret-pwd"

		BeforeEach(func() {
			os.Setenv("AGUI_PASSWORD", envPassword)
		})
		AfterEach(func() {
			os.Unsetenv("AGUI_PASSWORD")
		})

		It("uses env var when config value is empty", func() {
			mw := auth.Middleware(auth.Config{Password: auth.PasswordConfig{Enabled: true}})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-AGUI-Password", envPassword)
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusOK))
		})

		It("config value takes priority over env var", func() {
			mw := auth.Middleware(auth.Config{
				Password: auth.PasswordConfig{Enabled: true, Value: "config-pwd"},
			})
			// Config password works
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-AGUI-Password", "config-pwd")
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusOK))

			// Env var does NOT work
			req2 := httptest.NewRequest(http.MethodGet, "/", nil)
			req2.Header.Set("X-AGUI-Password", envPassword)
			Expect(doRequest(mw, req2).Code).To(Equal(http.StatusUnauthorized))
		})
	})

	Context("OS keyring", func() {
		const keyringPwd = "keyring-secret"

		BeforeEach(func() {
			_ = keyring.KeyringSet(keyring.AccountAGUIPassword, []byte(keyringPwd))
		})
		AfterEach(func() {
			_ = keyring.KeyringDelete(keyring.AccountAGUIPassword)
		})

		It("accepts keyring password when config and env are empty", func() {
			mw := auth.Middleware(auth.Config{Password: auth.PasswordConfig{Enabled: true}})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-AGUI-Password", keyringPwd)
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusOK))
		})
	})

	Context("auto-generated password", func() {
		BeforeEach(func() {
			_ = keyring.KeyringDelete(keyring.AccountAGUIPassword)
			os.Unsetenv("AGUI_PASSWORD")
		})

		It("generates a random password and rejects any guess", func() {
			mw := auth.Middleware(auth.Config{Password: auth.PasswordConfig{Enabled: true}})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-AGUI-Password", "random-guess")
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusUnauthorized))
		})

		It("rejects missing password", func() {
			mw := auth.Middleware(auth.Config{Password: auth.PasswordConfig{Enabled: true}})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusUnauthorized))
		})
	})

	Context("query param (removed for security)", func() {
		It("rejects password sent via query param (credentials must use header)", func() {
			mw := auth.Middleware(auth.Config{
				Password: auth.PasswordConfig{Enabled: true, Value: "query-pwd"},
			})
			req := httptest.NewRequest(http.MethodGet, "/?password=query-pwd", nil)
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusUnauthorized))
		})
	})
})
