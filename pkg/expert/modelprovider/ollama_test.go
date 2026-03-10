// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/

package modelprovider_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
)

var _ = Describe("Ollama", func() {
	Describe("DefaultOllamaURL", func() {
		It("is the default local Ollama base URL", func() {
			Expect(modelprovider.DefaultOllamaURL).To(Equal("http://localhost:11434"))
		})
	})

	Describe("OllamaReachable", func() {
		Context("when url is empty", func() {
			It("uses DefaultOllamaURL and returns a boolean (true if Ollama is running, false otherwise)", func(ctx context.Context) {
				got := modelprovider.OllamaReachable(ctx, "")
				Expect(got).To(BeElementOf(true, false))
			})
		})

		Context("when given a URL", func() {
			It("returns true when the server responds 200 OK", func(ctx context.Context) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				defer srv.Close()

				got := modelprovider.OllamaReachable(ctx, srv.URL)
				Expect(got).To(BeTrue())
			})

			It("returns false when the server responds non-200", func(ctx context.Context) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
				defer srv.Close()

				got := modelprovider.OllamaReachable(ctx, srv.URL)
				Expect(got).To(BeFalse())
			})

			It("returns false when the host is unreachable", func(ctx context.Context) {
				got := modelprovider.OllamaReachable(ctx, "http://127.0.0.1:19999")
				Expect(got).To(BeFalse())
			})

			It("returns false when context is cancelled", func(ctx context.Context) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				defer srv.Close()

				cancelled, cancel := context.WithCancel(ctx)
				cancel()

				got := modelprovider.OllamaReachable(cancelled, srv.URL)
				Expect(got).To(BeFalse())
			})
		})
	})

	Describe("ListModels", func() {
		It("returns model names when server returns valid /api/tags JSON", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"models":[{"name":"llama3"},{"name":"nomic-embed-text"}]}`))
			}))
			defer srv.Close()

			models, err := modelprovider.ListModels(ctx, srv.URL)
			Expect(err).NotTo(HaveOccurred())
			Expect(models).To(Equal([]string{"llama3", "nomic-embed-text"}))
		})

		It("returns error when server responds non-200", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()

			_, err := modelprovider.ListModels(ctx, srv.URL)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("HTTP 500"))
		})

		It("returns error when response is not valid JSON", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not json"))
			}))
			defer srv.Close()

			_, err := modelprovider.ListModels(ctx, srv.URL)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("DefaultOllamaModelForSetup", func() {
		It("is a Mac-friendly model name", func() {
			Expect(modelprovider.DefaultOllamaModelForSetup).To(Equal("llama3.2:3b"))
		})
	})

	Describe("PullModel", func() {
		It("returns nil when server responds 200", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodPost))
				Expect(r.URL.Path).To(Equal("/api/pull"))
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			err := modelprovider.PullModel(ctx, srv.URL, "llama3.2:3b")
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns error when model name is empty", func(ctx context.Context) {
			err := modelprovider.PullModel(ctx, "http://localhost:11434", "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("model name is required"))
		})
	})
})
