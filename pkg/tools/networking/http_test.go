// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package networking

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/httputil"
)

var _ = Describe("HTTPTool", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("NewTool", func() {
		It("should create a tool with correct name", func() {
			t := NewTool()
			Expect(t.Declaration().Name).To(Equal("http_request"))
		})

		It("should create a tool with description", func() {
			t := NewTool()
			Expect(t.Declaration().Description).To(ContainSubstring("HTTP request"))
		})
	})

	Describe("Do", func() {
		It("should perform a GET request", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal("GET"))
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"status":"ok"}`)
			}))
			defer server.Close()

			ht := &httpTool{client: server.Client(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			result, err := ht.Do(ctx, HTTPRequest{
				URL:    server.URL + "/health",
				Method: "GET",
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(ContainSubstring("200"))
			Expect(result).To(ContainSubstring(`{"status":"ok"}`))
		})

		It("should perform a POST request with body", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal("POST"))
				Expect(r.Header.Get("Content-Type")).To(Equal("application/json"))
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, `{"id":"123"}`)
			}))
			defer server.Close()

			ht := &httpTool{client: server.Client(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			result, err := ht.Do(ctx, HTTPRequest{
				URL:    server.URL + "/create",
				Method: "POST",
				Body:   `{"name":"test"}`,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(ContainSubstring("201"))
			Expect(result).To(ContainSubstring(`{"id":"123"}`))
		})

		It("should include custom headers", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Header.Get("Authorization")).To(Equal("Bearer tok123"))
				Expect(r.Header.Get("X-Custom")).To(Equal("value"))
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			ht := &httpTool{client: server.Client(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			_, err := ht.Do(ctx, HTTPRequest{
				URL:    server.URL,
				Method: "GET",
				Headers: map[string]string{
					"Authorization": "Bearer tok123",
					"X-Custom":      "value",
				},
			})

			Expect(err).NotTo(HaveOccurred())
		})

		It("should default method to GET", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal("GET"))
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			ht := &httpTool{client: server.Client(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			_, err := ht.Do(ctx, HTTPRequest{URL: server.URL})

			Expect(err).NotTo(HaveOccurred())
		})

		It("should reject unsupported methods", func() {
			ht := &httpTool{client: httputil.GetClient(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			_, err := ht.Do(ctx, HTTPRequest{URL: "http://example.com", Method: "INVALID"})

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported HTTP method"))
		})

		It("should error when URL is empty", func() {
			ht := &httpTool{client: httputil.GetClient(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			_, err := ht.Do(ctx, HTTPRequest{Method: "GET"})

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("url is required"))
		})

		It("should handle server errors gracefully", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, "internal error")
			}))
			defer server.Close()

			ht := &httpTool{client: server.Client(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			result, err := ht.Do(ctx, HTTPRequest{URL: server.URL})

			Expect(err).NotTo(HaveOccurred()) // HTTP errors are not Go errors
			Expect(result).To(ContainSubstring("500"))
			Expect(result).To(ContainSubstring("internal error"))
		})

		It("should truncate large responses", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				// Write more than maxResponseBytes
				for i := 0; i < 200; i++ {
					fmt.Fprint(w, "x")
				}
			}))
			defer server.Close()

			ht := &httpTool{client: server.Client(), maxResponseBytes: 100, defaultTimeout: 30}
			result, err := ht.Do(ctx, HTTPRequest{URL: server.URL})

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(ContainSubstring("[Response truncated"))
		})

		It("should handle empty response body", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			ht := &httpTool{client: server.Client(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			result, err := ht.Do(ctx, HTTPRequest{URL: server.URL})

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(ContainSubstring("204"))
			Expect(result).To(ContainSubstring("[Empty response body]"))
		})

		It("should cap timeout at maxTimeout", func() {
			ht := &httpTool{client: httputil.GetClient(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			// This just verifies no panic; the timeout capping is internal
			_, err := ht.Do(ctx, HTTPRequest{
				URL:     "http://localhost:1", // will fail but tests the path
				Timeout: 999,
			})
			Expect(err).To(HaveOccurred()) // connection refused
		})

		It("should support PUT method", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal("PUT"))
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			ht := &httpTool{client: server.Client(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			_, err := ht.Do(ctx, HTTPRequest{URL: server.URL, Method: "PUT", Body: `{}`})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should support DELETE method", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal("DELETE"))
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			ht := &httpTool{client: server.Client(), maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			_, err := ht.Do(ctx, HTTPRequest{URL: server.URL, Method: "DELETE"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should include response headers for Location", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://example.com/new")
				w.WriteHeader(http.StatusFound)
			}))
			defer server.Close()

			// Disable redirects so we see the 302
			client := server.Client()
			client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}

			ht := &httpTool{client: client, maxResponseBytes: defaultMaxResponseBytes, defaultTimeout: 30}
			result, err := ht.Do(ctx, HTTPRequest{URL: server.URL})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(ContainSubstring("Location: http://example.com/new"))
		})
	})
})
