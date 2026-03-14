// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package webfetch

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Web Fetch Tool (util_web_fetch)", func() {
	var (
		f      *fetchTools
		server *httptest.Server
	)

	BeforeEach(func() {
		f = newFetchTools()
	})

	AfterEach(func() {
		if server != nil {
			server.Close()
		}
	})

	Describe("GET requests", func() {
		It("fetches plain text content", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte("Hello, World!"))
			}))

			resp, err := f.fetch(context.Background(), fetchRequest{URL: server.URL})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			Expect(resp.Content).To(Equal("Hello, World!"))
			Expect(resp.ContentType).To(ContainSubstring("text/plain"))
		})

		It("fetches HTML and strips tags", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<html><head><title>Test Page</title></head><body><h1>Hello</h1><p>World</p></body></html>`))
			}))

			resp, err := f.fetch(context.Background(), fetchRequest{URL: server.URL})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Title).To(Equal("Test Page"))
			Expect(resp.Content).To(ContainSubstring("Hello"))
			Expect(resp.Content).To(ContainSubstring("World"))
			Expect(resp.Content).NotTo(ContainSubstring("<h1>"))
		})

		It("strips script and style tags from HTML", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<html><head><style>body{color:red}</style></head><body><script>alert('x')</script><p>Clean</p></body></html>`))
			}))

			resp, err := f.fetch(context.Background(), fetchRequest{URL: server.URL})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Content).To(ContainSubstring("Clean"))
			Expect(resp.Content).NotTo(ContainSubstring("alert"))
			Expect(resp.Content).NotTo(ContainSubstring("color:red"))
		})

		It("fetches JSON content", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			}))

			resp, err := f.fetch(context.Background(), fetchRequest{URL: server.URL})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Content).To(Equal(`{"status":"ok"}`))
		})

		It("reports non-200 status codes", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("not found"))
			}))

			resp, err := f.fetch(context.Background(), fetchRequest{URL: server.URL})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(404))
		})
	})

	Describe("HEAD requests", func() {
		It("returns status without body", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusOK)
			}))

			resp, err := f.fetch(context.Background(), fetchRequest{URL: server.URL, Method: "HEAD"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			Expect(resp.Content).To(BeEmpty())
		})
	})

	Describe("POST requests", func() {
		It("sends body and receives response", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal("POST"))
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte("received"))
			}))

			resp, err := f.fetch(context.Background(), fetchRequest{
				URL:    server.URL,
				Method: "POST",
				Body:   `{"key":"value"}`,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Content).To(Equal("received"))
		})
	})

	Describe("custom headers", func() {
		It("sends custom headers", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Header.Get("Authorization")).To(Equal("Bearer test-token"))
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte("authenticated"))
			}))

			resp, err := f.fetch(context.Background(), fetchRequest{
				URL:     server.URL,
				Headers: map[string]string{"Authorization": "Bearer test-token"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Content).To(Equal("authenticated"))
		})
	})

	It("returns error for empty URL", func() {
		_, err := f.fetch(context.Background(), fetchRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("url is required"))
	})

	It("returns error for unreachable URL", func() {
		_, err := f.fetch(context.Background(), fetchRequest{URL: "http://localhost:1"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("request failed"))
	})

	It("defaults method to GET", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("GET"))
			_, _ = w.Write([]byte("ok"))
		}))

		_, err := f.fetch(context.Background(), fetchRequest{URL: server.URL})
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("HTML helpers", func() {
	var f *fetchTools

	BeforeEach(func() {
		f = newFetchTools()
	})

	Describe("extractTitle", func() {
		DescribeTable("extracts title from HTML",
			func(html, expected string) {
				Expect(f.extractTitle(html)).To(Equal(expected))
			},
			Entry("standard title", `<html><head><title>My Page</title></head></html>`, "My Page"),
			Entry("no title", `<html><body>No title here</body></html>`, ""),
			Entry("empty title", `<html><head><title></title></head></html>`, ""),
		)
	})
})

var _ = Describe("Web Fetch ToolProvider", func() {
	It("returns the expected tool", func() {
		p := NewToolProvider()
		tools := p.GetTools(context.Background())
		Expect(tools).To(HaveLen(1))
		Expect(tools[0].Declaration().Name).To(Equal("web_fetch"))
	})
})
