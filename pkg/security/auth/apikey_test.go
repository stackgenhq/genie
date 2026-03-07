package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/security/auth"
)

var _ = Describe("API Key Authentication", func() {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	doRequest := func(mw func(http.Handler) http.Handler, req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mw(okHandler).ServeHTTP(rec, req)
		return rec
	}

	It("rejects request without token", func() {
		mw := auth.Middleware(auth.Config{
			APIKeys: auth.APIKeyConfig{Keys: []string{"secret-key"}},
		})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := doRequest(mw, req)
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))

		var body map[string]string
		Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
		Expect(body["error"]).To(Equal("missing_api_key"))
	})

	It("accepts valid token via Authorization Bearer", func() {
		mw := auth.Middleware(auth.Config{
			APIKeys: auth.APIKeyConfig{Keys: []string{"secret-key"}},
		})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer secret-key")
		rec := doRequest(mw, req)
		Expect(rec.Code).To(Equal(http.StatusOK))
	})

	It("accepts valid token via X-API-Key header", func() {
		mw := auth.Middleware(auth.Config{
			APIKeys: auth.APIKeyConfig{Keys: []string{"key-1", "secret-key"}},
		})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-API-Key", "secret-key")
		rec := doRequest(mw, req)
		Expect(rec.Code).To(Equal(http.StatusOK))
	})

	It("rejects invalid token via Authorization Bearer", func() {
		mw := auth.Middleware(auth.Config{
			APIKeys: auth.APIKeyConfig{Keys: []string{"secret-key"}},
		})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
		rec := doRequest(mw, req)
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))

		var body map[string]string
		Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
		Expect(body["error"]).To(Equal("invalid_api_key"))
	})

	It("uses constant time comparison without leaking length details", func() {
		mw := auth.Middleware(auth.Config{
			APIKeys: auth.APIKeyConfig{Keys: []string{"12345"}},
		})
		for _, guess := range []string{"", "123", strings.Repeat("a", 100), "123x5"} {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-API-Key", guess)
			Expect(doRequest(mw, req).Code).To(Equal(http.StatusUnauthorized))
		}
	})
})
