package pkgsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Package Search Tool (util_pkg_search)", func() {
	var p *pkgTools

	BeforeEach(func() {
		p = newPkgTools()
	})

	Describe("npm registry", func() {
		It("parses npm search results from mock server", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := map[string]any{
					"objects": []map[string]any{
						{
							"package": map[string]any{
								"name":        "express",
								"version":     "4.18.2",
								"description": "Fast, unopinionated web framework",
								"links":       map[string]any{"npm": "https://www.npmjs.com/package/express"},
							},
						},
						{
							"package": map[string]any{
								"name":        "express-validator",
								"version":     "7.0.1",
								"description": "Express middleware for validation",
								"links":       map[string]any{"npm": "https://www.npmjs.com/package/express-validator"},
							},
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			// Point at mock server instead of real npm registry.
			p.npmBaseURL = server.URL

			resp, err := p.searchNPM(context.Background(), "express", searchResponse{Registry: "npm", Query: "express"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(2))
			Expect(resp.Results[0].Name).To(Equal("express"))
			Expect(resp.Results[0].Version).To(Equal("4.18.2"))
			Expect(resp.Results[1].Name).To(Equal("express-validator"))
		})
	})

	Describe("pypi registry", func() {
		It("parses pypi package info from mock server", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := map[string]any{
					"info": map[string]any{
						"name":        "requests",
						"version":     "2.31.0",
						"summary":     "Python HTTP for Humans.",
						"package_url": "https://pypi.org/project/requests/",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			// Point at mock server instead of real PyPI.
			p.pypiBaseURL = server.URL

			resp, err := p.searchPyPI(context.Background(), "requests", searchResponse{Registry: "pypi", Query: "requests"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(1))
			Expect(resp.Results[0].Name).To(Equal("requests"))
			Expect(resp.Results[0].Version).To(Equal("2.31.0"))
			Expect(resp.Results[0].Description).To(Equal("Python HTTP for Humans."))
		})
	})

	Describe("error cases", func() {
		It("returns error for empty query", func() {
			_, err := p.search(context.Background(), searchRequest{Registry: "npm"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("query is required"))
		})

		It("returns error for unsupported registry", func() {
			_, err := p.search(context.Background(), searchRequest{Registry: "rubygems", Query: "rails"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported registry"))
		})
	})

	Describe("helpers", func() {
		var pt *pkgTools

		BeforeEach(func() {
			pt = newPkgTools()
		})

		DescribeTable("truncateStr",
			func(input string, max int, expected string) {
				Expect(pt.truncateStr(input, max)).To(Equal(expected))
			},
			Entry("short string", "hello", 10, "hello"),
			Entry("exact length", "hello", 5, "hello"),
			Entry("truncated", "hello world", 8, "hello..."),
		)
	})
})

var _ = Describe("Package Search ToolProvider", func() {
	It("returns the expected tool", func() {
		p := NewToolProvider()
		tools := p.GetTools()
		Expect(tools).To(HaveLen(1))
		Expect(tools[0].Declaration().Name).To(Equal("util_pkg_search"))
	})
})
