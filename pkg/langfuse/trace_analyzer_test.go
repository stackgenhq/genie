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

var _ = Describe("TraceAnalyzer", func() {
	Describe("NewTraceAnalyzer", func() {
		It("returns nil when config is missing credentials", func() {
			cfg := Config{}
			Expect(cfg.NewTraceAnalyzer()).To(BeNil())
		})

		It("returns nil when public key is empty", func() {
			cfg := Config{SecretKey: "sk", Host: "http://localhost"}
			Expect(cfg.NewTraceAnalyzer()).To(BeNil())
		})

		It("returns a valid analyzer when config is complete", func() {
			cfg := Config{PublicKey: "pk", SecretKey: "sk", Host: "http://localhost"}
			Expect(cfg.NewTraceAnalyzer()).NotTo(BeNil())
		})
	})

	Describe("Analyze (API-based)", func() {
		var (
			server   *httptest.Server
			analyzer *TraceAnalyzer
		)

		AfterEach(func() {
			if server != nil {
				server.Close()
			}
		})

		Context("with a server returning traces and observations", func() {
			BeforeEach(func() {
				sessionID := "session-123"

				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.URL.Path == "/api/public/traces":
						Expect(r.URL.Query().Get("userId")).To(Equal("arul@stackgen.com"))
						Expect(r.URL.Query().Get("name")).To(Equal("devops-copilot"))

						traces := tracesResponse{
							Data: []Trace{
								{ID: "t1", Timestamp: parseTime("2026-03-11T13:26:00Z"), Name: "devops-copilot", UserID: "arul@stackgen.com", Input: "Check k8s cluster health", SessionID: &sessionID},
							},
						}
						w.WriteHeader(http.StatusOK)
						data, _ := json.Marshal(traces)
						_, _ = w.Write(data)

					case r.URL.Path == "/api/public/observations":
						Expect(r.URL.Query().Get("traceId")).To(Equal("t1"))

						obs := observationsResponse{
							Data: []Observation{
								{ID: "obs1", TraceID: "t1", Type: "SPAN", Name: "agent-run", StartTime: parseTime("2026-03-11T13:26:00Z")},
								{ID: "obs2", TraceID: "t1", Type: "GENERATION", Name: "gpt-4", ParentObservationID: strPtr("obs1"), StartTime: parseTime("2026-03-11T13:26:01Z"), Model: "gpt-4", Usage: &ObsUsage{Input: 500, Output: 100, Total: 600}},
								{ID: "obs3", TraceID: "t1", Type: "SPAN", Name: "k8s_get_pods", ParentObservationID: strPtr("obs1"), StartTime: parseTime("2026-03-11T13:26:02Z"), Input: "kubectl get pods"},
								{ID: "obs4", TraceID: "t1", Type: "SPAN", Name: "k8s_cluster_health", ParentObservationID: strPtr("obs1"), StartTime: parseTime("2026-03-11T13:26:03Z"), Input: "cluster info"},
							},
						}
						w.WriteHeader(http.StatusOK)
						data, _ := json.Marshal(obs)
						_, _ = w.Write(data)
					}
				}))

				cfg := Config{PublicKey: "pk", SecretKey: "sk", Host: server.URL}
				analyzer = &TraceAnalyzer{config: cfg, httpClient: server.Client()}
			})

			It("fetches traces and breaks down execution", func() {
				result, err := analyzer.Analyze(context.Background(), AnalyzeTracesRequest{
					UserID:    "arul@stackgen.com",
					AgentName: "devops-copilot",
					Duration:  24 * time.Hour,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.TracesAnalyzed).To(Equal(1))
				Expect(result.TraceDetails).To(HaveLen(1))

				detail := result.TraceDetails[0]
				Expect(detail.TraceID).To(Equal("t1"))
				Expect(detail.AgentName).To(Equal("devops-copilot"))
				Expect(detail.UserID).To(Equal("arul@stackgen.com"))
				Expect(detail.Input).To(Equal("Check k8s cluster health"))
				Expect(detail.LLMCalls).To(Equal(1))
				// agent-run has a child GENERATION, so it's a sub-agent.
				// k8s_get_pods and k8s_cluster_health are under that sub-agent.
				Expect(detail.SubAgents).To(HaveLen(1))
				Expect(detail.SubAgents[0].Name).To(Equal("agent-run"))
				Expect(detail.SubAgents[0].LLMCalls).To(Equal(1))
				Expect(detail.SubAgents[0].ToolCalls).To(Equal(2)) // k8s_get_pods + k8s_cluster_health
				Expect(detail.InputTokens).To(Equal(500))
				Expect(detail.OutputTokens).To(Equal(100))
			})

			It("aggregates totals across traces", func() {
				result, err := analyzer.Analyze(context.Background(), AnalyzeTracesRequest{
					UserID:    "arul@stackgen.com",
					AgentName: "devops-copilot",
					Duration:  24 * time.Hour,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.TotalLLMCalls).To(Equal(1))
				Expect(result.TotalSubAgents).To(Equal(1))
				Expect(result.TotalInputTokens).To(Equal(500))
				Expect(result.TotalOutputTokens).To(Equal(100))
			})
		})

		Context("when a span has child generations (sub-agent)", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.URL.Path == "/api/public/traces":
						resp := tracesResponse{
							Data: []Trace{
								{ID: "t1", Timestamp: parseTime("2026-03-11T13:00:00Z"), Name: "main-agent", UserID: "user@test.com", Input: "Run analysis"},
							},
						}
						w.WriteHeader(http.StatusOK)
						data, _ := json.Marshal(resp)
						_, _ = w.Write(data)

					case r.URL.Path == "/api/public/observations":
						resp := observationsResponse{
							Data: []Observation{
								{ID: "root", TraceID: "t1", Type: "SPAN", Name: "main-run", StartTime: parseTime("2026-03-11T13:00:00Z")},
								// Sub-agent span with child generation
								{ID: "sub1", TraceID: "t1", Type: "SPAN", Name: "research-subagent", ParentObservationID: strPtr("root"), StartTime: parseTime("2026-03-11T13:00:01Z"), Input: "research topic", Output: "findings"},
								{ID: "gen1", TraceID: "t1", Type: "GENERATION", Name: "llm-call-1", ParentObservationID: strPtr("sub1"), StartTime: parseTime("2026-03-11T13:00:02Z"), Usage: &ObsUsage{Input: 200, Output: 50, Total: 250}},
								{ID: "gen2", TraceID: "t1", Type: "GENERATION", Name: "llm-call-2", ParentObservationID: strPtr("sub1"), StartTime: parseTime("2026-03-11T13:00:03Z"), Usage: &ObsUsage{Input: 300, Output: 80, Total: 380}},
								// Tool call under sub-agent
								{ID: "tool1", TraceID: "t1", Type: "SPAN", Name: "web_search", ParentObservationID: strPtr("sub1"), StartTime: parseTime("2026-03-11T13:00:04Z")},
								// Top-level tool call
								{ID: "tool2", TraceID: "t1", Type: "SPAN", Name: "slack_post", ParentObservationID: strPtr("root"), StartTime: parseTime("2026-03-11T13:00:05Z")},
							},
						}
						w.WriteHeader(http.StatusOK)
						data, _ := json.Marshal(resp)
						_, _ = w.Write(data)
					}
				}))

				cfg := Config{PublicKey: "pk", SecretKey: "sk", Host: server.URL}
				analyzer = &TraceAnalyzer{config: cfg, httpClient: server.Client()}
			})

			It("identifies sub-agents with their LLM and tool counts", func() {
				result, err := analyzer.Analyze(context.Background(), AnalyzeTracesRequest{
					Duration: 24 * time.Hour,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.TraceDetails).To(HaveLen(1))

				detail := result.TraceDetails[0]
				Expect(detail.SubAgents).To(HaveLen(1))
				Expect(detail.SubAgents[0].Name).To(Equal("research-subagent"))
				Expect(detail.SubAgents[0].LLMCalls).To(Equal(2))
				Expect(detail.SubAgents[0].ToolCalls).To(Equal(1)) // web_search

				// Total LLM calls = 2 (both under sub-agent)
				Expect(detail.LLMCalls).To(Equal(2))
				// Top-level tool calls only = slack_post (sub-agent itself is not a tool call)
				Expect(detail.ToolCalls).To(HaveLen(1))
				Expect(detail.ToolCalls[0].Name).To(Equal("slack_post"))
			})

			It("captures sub-agent input and output", func() {
				result, err := analyzer.Analyze(context.Background(), AnalyzeTracesRequest{
					Duration: 24 * time.Hour,
				})
				Expect(err).NotTo(HaveOccurred())

				sa := result.TraceDetails[0].SubAgents[0]
				Expect(sa.Input).To(ContainSubstring("research topic"))
				Expect(sa.Output).To(ContainSubstring("findings"))
			})
		})

		Context("with vector store operations", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.URL.Path == "/api/public/traces":
						resp := tracesResponse{
							Data: []Trace{
								{ID: "t1", Timestamp: parseTime("2026-03-11T13:00:00Z"), Name: "agent", UserID: "user@test.com", Input: "Store data"},
							},
						}
						w.WriteHeader(http.StatusOK)
						data, _ := json.Marshal(resp)
						_, _ = w.Write(data)
					case r.URL.Path == "/api/public/observations":
						resp := observationsResponse{
							Data: []Observation{
								{ID: "root", TraceID: "t1", Type: "SPAN", Name: "agent-root", StartTime: parseTime("2026-03-11T13:00:00Z")},
								{ID: "obs1", TraceID: "t1", Type: "SPAN", Name: "addMemory", ParentObservationID: strPtr("root"), StartTime: parseTime("2026-03-11T13:00:01Z")},
								{ID: "obs2", TraceID: "t1", Type: "SPAN", Name: "vector_store", ParentObservationID: strPtr("root"), StartTime: parseTime("2026-03-11T13:00:02Z")},
								{ID: "obs3", TraceID: "t1", Type: "SPAN", Name: "regular_tool", ParentObservationID: strPtr("root"), StartTime: parseTime("2026-03-11T13:00:03Z")},
							},
						}
						w.WriteHeader(http.StatusOK)
						data, _ := json.Marshal(resp)
						_, _ = w.Write(data)
					}
				}))

				cfg := Config{PublicKey: "pk", SecretKey: "sk", Host: server.URL}
				analyzer = &TraceAnalyzer{config: cfg, httpClient: server.Client()}
			})

			It("counts vector store operations", func() {
				result, err := analyzer.Analyze(context.Background(), AnalyzeTracesRequest{
					Duration: 24 * time.Hour,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.TraceDetails[0].VectorStoreOps).To(Equal(2)) // addMemory + vector_store
				Expect(result.TotalVectorStoreOps).To(Equal(2))
			})
		})

		Context("with session and tags filters", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/public/traces" {
						Expect(r.URL.Query().Get("sessionId")).To(Equal("sess-abc"))
						Expect(r.URL.Query()["tags"]).To(ContainElements("agui"))
					}
					resp := tracesResponse{Data: []Trace{}}
					w.WriteHeader(http.StatusOK)
					data, _ := json.Marshal(resp)
					_, _ = w.Write(data)
				}))

				cfg := Config{PublicKey: "pk", SecretKey: "sk", Host: server.URL}
				analyzer = &TraceAnalyzer{config: cfg, httpClient: server.Client()}
			})

			It("passes filters to the API", func() {
				_, err := analyzer.Analyze(context.Background(), AnalyzeTracesRequest{
					SessionID: "sess-abc",
					Tags:      []string{"agui"},
					Duration:  1 * time.Hour,
				})
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when API returns an error", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error": "server error"}`))
				}))
				cfg := Config{PublicKey: "pk", SecretKey: "sk", Host: server.URL}
				analyzer = &TraceAnalyzer{config: cfg, httpClient: server.Client()}
			})

			It("returns error", func() {
				_, err := analyzer.Analyze(context.Background(), AnalyzeTracesRequest{
					Duration: 1 * time.Hour,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unexpected status code 500"))
			})
		})

		It("returns error on nil analyzer", func() {
			var nilAnalyzer *TraceAnalyzer
			_, err := nilAnalyzer.Analyze(context.Background(), AnalyzeTracesRequest{
				Duration: 1 * time.Hour,
			})
			Expect(err).To(HaveOccurred())
		})

		It("returns error when duration is zero", func() {
			cfg := Config{PublicKey: "pk", SecretKey: "sk", Host: "http://localhost"}
			a := cfg.NewTraceAnalyzer()
			_, err := a.Analyze(context.Background(), AnalyzeTracesRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duration must be positive"))
		})
	})

	Describe("FormatReport", func() {
		It("generates a readable report", func() {
			result := TraceAnalysisResult{
				TracesAnalyzed:      2,
				TotalLLMCalls:       5,
				TotalToolCalls:      3,
				TotalSubAgents:      1,
				TotalVectorStoreOps: 2,
				TotalInputTokens:    1000,
				TotalOutputTokens:   200,
				TraceDetails: []TraceDetail{
					{
						TraceID:   "t1",
						AgentName: "devops-copilot",
						UserID:    "alice@corp.com",
						Input:     "List pods",
						LLMCalls:  3,
						ToolCalls: []ToolCallDetail{
							{Name: "k8s_get_pods", ParentName: "agent-run"},
						},
						SubAgents: []SubAgentDetail{
							{Name: "research-sub", LLMCalls: 2, ToolCalls: 1},
						},
						InputTokens:  500,
						OutputTokens: 100,
					},
				},
			}
			report := result.FormatReport()
			Expect(report).To(ContainSubstring("Traces analyzed | 2"))
			Expect(report).To(ContainSubstring("Total LLM calls | 5"))
			Expect(report).To(ContainSubstring("Total tool calls | 3"))
			Expect(report).To(ContainSubstring("k8s_get_pods"))
			Expect(report).To(ContainSubstring("research-sub"))
			Expect(report).To(ContainSubstring("alice@corp.com"))
		})
	})

	Describe("Helper functions", func() {
		Describe("isVectorStoreOp", func() {
			It("detects known vector store tools", func() {
				Expect(isVectorStoreOp("addMemory")).To(BeTrue())
				Expect(isVectorStoreOp("add_memory")).To(BeTrue())
				Expect(isVectorStoreOp("vector_store")).To(BeTrue())
				Expect(isVectorStoreOp("qdrant_upsert")).To(BeTrue())
			})

			It("detects by keyword pattern", func() {
				Expect(isVectorStoreOp("save_to_vector_db")).To(BeTrue())
				Expect(isVectorStoreOp("embedding_store")).To(BeTrue())
				Expect(isVectorStoreOp("memory_add_entry")).To(BeTrue())
			})

			It("does not flag regular tools", func() {
				Expect(isVectorStoreOp("k8s_get_pods")).To(BeFalse())
				Expect(isVectorStoreOp("slack_post")).To(BeFalse())
				Expect(isVectorStoreOp("web_search")).To(BeFalse())
			})
		})

		Describe("truncateStr", func() {
			It("truncates long strings", func() {
				Expect(truncateStr("hello world", 5)).To(Equal("hello..."))
			})

			It("does not truncate short strings", func() {
				Expect(truncateStr("hi", 10)).To(Equal("hi"))
			})
		})

		Describe("truncateAny", func() {
			It("handles nil", func() {
				Expect(truncateAny(nil, 10)).To(Equal(""))
			})

			It("handles strings", func() {
				Expect(truncateAny("hello world", 5)).To(Equal("hello..."))
			})

			It("handles maps (JSON)", func() {
				m := map[string]string{"key": "value"}
				result := truncateAny(m, 100)
				Expect(result).To(ContainSubstring("key"))
			})
		})

		Describe("buildTraceDetail", func() {
			It("handles traces with no observations", func() {
				t := Trace{ID: "t1", Name: "agent", UserID: "user", Input: "hello"}
				detail := buildTraceDetail(t, nil)
				Expect(detail.TraceID).To(Equal("t1"))
				Expect(detail.LLMCalls).To(Equal(0))
				Expect(detail.ToolCalls).To(BeEmpty())
				Expect(detail.SubAgents).To(BeEmpty())
			})

			It("computes duration from observations", func() {
				t := Trace{ID: "t1", Name: "agent", UserID: "user", Input: "hello"}
				endTime := parseTime("2026-03-11T13:00:10Z")
				obs := []Observation{
					{ID: "o1", TraceID: "t1", Type: "SPAN", Name: "tool1", StartTime: parseTime("2026-03-11T13:00:00Z")},
					{ID: "o2", TraceID: "t1", Type: "SPAN", Name: "tool2", StartTime: parseTime("2026-03-11T13:00:05Z"), EndTime: &endTime},
				}
				detail := buildTraceDetail(t, obs)
				Expect(detail.Duration).To(Equal(10 * time.Second))
			})

			It("accumulates cost from generation observations", func() {
				t := Trace{ID: "t1", Name: "agent", UserID: "user", Input: "hello"}
				obs := []Observation{
					{ID: "g1", TraceID: "t1", Type: "GENERATION", Name: "gen1", StartTime: parseTime("2026-03-11T13:00:00Z"), Usage: &ObsUsage{Input: 100, Output: 50, Total: 150, Cost: 0.001}},
					{ID: "g2", TraceID: "t1", Type: "GENERATION", Name: "gen2", StartTime: parseTime("2026-03-11T13:00:01Z"), Usage: &ObsUsage{Input: 200, Output: 80, Total: 280, Cost: 0.002}},
				}
				detail := buildTraceDetail(t, obs)
				Expect(detail.LLMCalls).To(Equal(2))
				Expect(detail.InputTokens).To(Equal(300))
				Expect(detail.OutputTokens).To(Equal(130))
				Expect(detail.TotalCost).To(BeNumerically("~", 0.003, 0.0001))
			})
		})
	})
})

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func strPtr(s string) *string {
	return &s
}
