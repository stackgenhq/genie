// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package langfuse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Metrics", func() {
	Describe("GetAgentStats", func() {
		var (
			server *httptest.Server
			c      *client
			cfg    Config
		)

		BeforeEach(func() {
			cfg = Config{
				PublicKey: "pk",
				SecretKey: "sk",
				Host:      "http://localhost",
			}
		})

		AfterEach(func() {
			if server != nil {
				server.Close()
			}
		})

		Context("with successful response returning multiple agents", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/api/public/metrics"))
					Expect(r.URL.Query().Get("query")).NotTo(BeEmpty())

					// Verify the query contains expected fields
					var query metricsQuery
					err := json.Unmarshal([]byte(r.URL.Query().Get("query")), &query)
					Expect(err).NotTo(HaveOccurred())
					Expect(query.View).To(Equal("observations"))
					Expect(query.Dimensions).To(HaveLen(1))
					Expect(query.Dimensions[0].Field).To(Equal("traceName"))

					// v1 response format: keys are aggregation_measure, token values as strings
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{
						"data": [
							{
								"traceName": "codeowner.chat",
								"sum_totalCost": 0.315126,
								"sum_inputTokens": "370033",
								"sum_outputTokens": "42710",
								"sum_totalTokens": "412743",
								"count_count": "524"
							},
							{
								"traceName": "devops.chat",
								"sum_totalCost": 0.0123,
								"sum_inputTokens": "5000",
								"sum_outputTokens": "1200",
								"sum_totalTokens": "6200",
								"count_count": "15"
							}
						]
					}`))
				}))
				cfg.Host = server.URL
				c = &client{
					httpClient: server.Client(),
					config:     cfg,
				}
			})

			It("returns stats for all agents", func() {
				stats, err := c.GetAgentStats(context.Background(), GetAgentStatsRequest{
					Duration: 24 * time.Hour,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(stats).To(HaveLen(2))

				Expect(stats[0].AgentName).To(Equal("codeowner.chat"))
				Expect(stats[0].TotalCost).To(BeNumerically("~", 0.315126, 0.0001))
				Expect(stats[0].InputTokens).To(BeNumerically("==", 370033))
				Expect(stats[0].OutputTokens).To(BeNumerically("==", 42710))
				Expect(stats[0].TotalTokens).To(BeNumerically("==", 412743))
				Expect(stats[0].Count).To(BeNumerically("==", 524))

				Expect(stats[1].AgentName).To(Equal("devops.chat"))
				Expect(stats[1].TotalCost).To(BeNumerically("~", 0.0123, 0.0001))
			})
		})

		Context("with agent name filter", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var query metricsQuery
					err := json.Unmarshal([]byte(r.URL.Query().Get("query")), &query)
					Expect(err).NotTo(HaveOccurred())

					// Verify the filter is applied
					Expect(query.Filters).To(HaveLen(1))
					Expect(query.Filters[0].Column).To(Equal("traceName"))
					Expect(query.Filters[0].Operator).To(Equal("="))
					Expect(query.Filters[0].Value).To(Equal("codeowner.chat"))

					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{
						"data": [
							{
								"traceName": "codeowner.chat",
								"sum_totalCost": 0.315126,
								"sum_inputTokens": "370033",
								"sum_outputTokens": "42710",
								"sum_totalTokens": "412743",
								"count_count": "524"
							}
						]
					}`))
				}))
				cfg.Host = server.URL
				c = &client{
					httpClient: server.Client(),
					config:     cfg,
				}
			})

			It("sends the filter to the API", func() {
				stats, err := c.GetAgentStats(context.Background(), GetAgentStatsRequest{
					Duration:  24 * time.Hour,
					AgentName: "codeowner.chat",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(stats).To(HaveLen(1))
				Expect(stats[0].AgentName).To(Equal("codeowner.chat"))
			})
		})

		Context("when cost is null (model not tracked)", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{
						"data": [
							{
								"traceName": "unknown-model",
								"sum_totalCost": null,
								"sum_inputTokens": "12593",
								"sum_outputTokens": "957",
								"sum_totalTokens": "13550",
								"count_count": "75"
							}
						]
					}`))
				}))
				cfg.Host = server.URL
				c = &client{
					httpClient: server.Client(),
					config:     cfg,
				}
			})

			It("returns 0 for null cost", func() {
				stats, err := c.GetAgentStats(context.Background(), GetAgentStatsRequest{
					Duration: 1 * time.Hour,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(stats).To(HaveLen(1))
				Expect(stats[0].TotalCost).To(Equal(float64(0)))
				Expect(stats[0].InputTokens).To(BeNumerically("==", 12593))
			})
		})

		Context("when API returns empty data", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"data": []}`))
				}))
				cfg.Host = server.URL
				c = &client{
					httpClient: server.Client(),
					config:     cfg,
				}
			})

			It("returns an empty slice", func() {
				stats, err := c.GetAgentStats(context.Background(), GetAgentStatsRequest{
					Duration: 1 * time.Hour,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(stats).To(BeEmpty())
			})
		})

		Context("when API returns an error status code", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error": "Invalid query"}`))
				}))
				cfg.Host = server.URL
				c = &client{
					httpClient: server.Client(),
					config:     cfg,
				}
			})

			It("returns error with status code", func() {
				stats, err := c.GetAgentStats(context.Background(), GetAgentStatsRequest{
					Duration: 1 * time.Hour,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unexpected status code 400"))
				Expect(stats).To(BeNil())
			})
		})

		Context("when API returns invalid JSON", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`not valid json`))
				}))
				cfg.Host = server.URL
				c = &client{
					httpClient: server.Client(),
					config:     cfg,
				}
			})

			It("returns error", func() {
				stats, err := c.GetAgentStats(context.Background(), GetAgentStatsRequest{
					Duration: 1 * time.Hour,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to unmarshal"))
				Expect(stats).To(BeNil())
			})
		})
	})

	Describe("parseAgentStats", func() {
		It("parses v1 format with string token values and float cost", func() {
			data := []map[string]any{
				{
					"traceName":        "my-agent",
					"sum_totalCost":    59.081,
					"sum_inputTokens":  "22548075",
					"sum_outputTokens": "2526738",
					"sum_totalTokens":  "25074813",
					"count_count":      "2974",
				},
			}
			stats := parseAgentStats(data)
			Expect(stats).To(HaveLen(1))
			Expect(stats[0].AgentName).To(Equal("my-agent"))
			Expect(stats[0].TotalCost).To(BeNumerically("~", 59.081, 0.001))
			Expect(stats[0].InputTokens).To(BeNumerically("==", 22548075))
			Expect(stats[0].OutputTokens).To(BeNumerically("==", 2526738))
			Expect(stats[0].TotalTokens).To(BeNumerically("==", 25074813))
			Expect(stats[0].Count).To(BeNumerically("==", 2974))
		})

		It("handles null cost gracefully", func() {
			data := []map[string]any{
				{
					"traceName":        "null-cost-agent",
					"sum_totalCost":    nil,
					"sum_inputTokens":  "100",
					"sum_outputTokens": "50",
					"sum_totalTokens":  "150",
					"count_count":      "5",
				},
			}
			stats := parseAgentStats(data)
			Expect(stats).To(HaveLen(1))
			Expect(stats[0].TotalCost).To(Equal(float64(0)))
			Expect(stats[0].InputTokens).To(BeNumerically("==", 100))
		})

		It("handles missing keys gracefully", func() {
			data := []map[string]any{
				{
					"traceName": "partial-agent",
				},
			}
			stats := parseAgentStats(data)
			Expect(stats).To(HaveLen(1))
			Expect(stats[0].AgentName).To(Equal("partial-agent"))
			Expect(stats[0].TotalCost).To(Equal(float64(0)))
			Expect(stats[0].InputTokens).To(Equal(float64(0)))
		})

		It("handles empty data", func() {
			stats := parseAgentStats(nil)
			Expect(stats).To(BeEmpty())
		})
	})

	Describe("numericFromMap", func() {
		It("handles float64 values", func() {
			m := map[string]any{"key": float64(42.5)}
			Expect(numericFromMap(m, "key")).To(Equal(42.5))
		})

		It("handles string values", func() {
			m := map[string]any{"key": "12345"}
			Expect(numericFromMap(m, "key")).To(Equal(float64(12345)))
		})

		It("returns 0 for nil values", func() {
			m := map[string]any{"key": nil}
			Expect(numericFromMap(m, "key")).To(Equal(float64(0)))
		})

		It("returns 0 for missing keys", func() {
			m := map[string]any{}
			Expect(numericFromMap(m, "missing")).To(Equal(float64(0)))
		})

		It("returns 0 for non-parseable strings", func() {
			m := map[string]any{"key": "not-a-number"}
			Expect(numericFromMap(m, "key")).To(Equal(float64(0)))
		})
	})

	Describe("Global GetAgentStats", func() {
		BeforeEach(func() {
			defaultClient = nil
		})

		It("returns nil when no client is configured", func() {
			stats, err := GetAgentStats(context.Background(), GetAgentStatsRequest{
				Duration: 1 * time.Hour,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(stats).To(BeNil())
		})

		It("returns nil when using noopClient", func() {
			defaultClient = &noopClient{}
			stats, err := GetAgentStats(context.Background(), GetAgentStatsRequest{
				Duration: 1 * time.Hour,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(stats).To(BeNil())
		})
	})
})
