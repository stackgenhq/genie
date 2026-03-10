// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package websearch_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/websearch"
)

var _ = Describe("Wikipedia Search Tool", func() {
	Context("NewWikipediaTool", func() {
		It("should create a tool with the correct name", func() {
			t := websearch.NewWikipediaTool()
			Expect(t.Declaration().Name).To(Equal("wikipedia_search"))
		})
	})

	Context("Search", func() {
		var (
			server *httptest.Server
			tool   func(ctx context.Context, input []byte) (any, error)
		)

		AfterEach(func() {
			if server != nil {
				server.Close()
			}
		})

		It("should parse results from valid JSON", func(ctx context.Context) {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodGet))
				Expect(r.URL.Query().Get("action")).To(Equal("query"))
				Expect(r.URL.Query().Get("srsearch")).To(Equal("test query"))
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{
					"query": {
						"search": [
							{
								"title": "Test Title 1",
								"snippet": "Test snippet with <span class=\"searchmatch\">bold</span> text &amp; decoding.",
								"pageid": 1234
							},
							{
								"title": "Test Title 2",
								"snippet": "Another snippet without html.",
								"pageid": 5678
							}
						]
					}
				}`)
			}))

			t := websearch.NewWikipediaTool(
				websearch.WithWikipediaEndpoint(server.URL),
				websearch.WithWikipediaHTTPClient(server.Client()),
			)
			tool = t.Call

			res, err := tool(ctx, []byte(`{"query":"test query"}`))
			Expect(err).NotTo(HaveOccurred())

			text := res.(string)
			Expect(text).To(ContainSubstring("1. Test Title 1"))
			Expect(text).To(ContainSubstring("https://en.wikipedia.org/?curid=1234"))
			Expect(text).To(ContainSubstring("Test snippet with bold text & decoding."))
			Expect(text).To(ContainSubstring("2. Test Title 2"))
			Expect(text).To(ContainSubstring("https://en.wikipedia.org/?curid=5678"))
			Expect(text).To(ContainSubstring("Another snippet without html."))
		})

		It("should return no-results message for empty search array", func(ctx context.Context) {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{
					"query": {
						"search": []
					}
				}`)
			}))

			t := websearch.NewWikipediaTool(
				websearch.WithWikipediaEndpoint(server.URL),
				websearch.WithWikipediaHTTPClient(server.Client()),
			)

			res, err := t.Call(ctx, []byte(`{"query":"xyznonexistent123"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("No results found"))
		})

		It("should return error on empty query", func(ctx context.Context) {
			t := websearch.NewWikipediaTool()
			_, err := t.Call(ctx, []byte(`{"query":"   "}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("empty search query provided"))
		})

		It("should fail after max retries on persistent HTTP 500", func(ctx context.Context) {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			t := websearch.NewWikipediaTool(
				websearch.WithWikipediaEndpoint(server.URL),
				websearch.WithWikipediaHTTPClient(server.Client()),
			)

			_, err := t.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("giving up"))
		})
	})
})
