package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Metrics Tool", func() {
	var (
		m      *metricsTools
		server *httptest.Server
	)

	BeforeEach(func() {
		m = newMetricsTools()
	})

	AfterEach(func() {
		if server != nil {
			server.Close()
		}
	})

	Describe("operation dispatch", func() {
		It("rejects unsupported operation", func() {
			_, err := m.query(context.Background(), metricsRequest{
				Operation: "hack_prometheus",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported operation"))
		})
	})

	Describe("instant_query", func() {
		It("requires query parameter", func() {
			_, err := m.query(context.Background(), metricsRequest{
				Operation:     "instant_query",
				PrometheusURL: "http://localhost:9090",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("query is required"))
		})

		It("queries prometheus and returns results", func() {
			promResponse := map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"resultType": "vector",
					"result": []map[string]interface{}{
						{
							"metric": map[string]string{"__name__": "up", "job": "prometheus"},
							"value":  []interface{}{1708000000.0, "1"},
						},
					},
				},
			}
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/api/v1/query"))
				Expect(r.URL.Query().Get("query")).To(Equal("up"))
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(promResponse)
			}))

			resp, err := m.query(context.Background(), metricsRequest{
				Operation:     "instant_query",
				Query:         "up",
				PrometheusURL: server.URL,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Status).To(Equal("success"))
			Expect(resp.Result).To(ContainSubstring("prometheus"))
		})
	})

	Describe("range_query", func() {
		It("requires query parameter", func() {
			_, err := m.query(context.Background(), metricsRequest{
				Operation: "range_query",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("query is required"))
		})

		It("sends range query with start/end/step", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/api/v1/query_range"))
				Expect(r.URL.Query().Get("query")).To(Equal("rate(http_requests_total[5m])"))
				Expect(r.URL.Query().Get("step")).To(Equal("30s"))
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
			}))

			resp, err := m.query(context.Background(), metricsRequest{
				Operation:     "range_query",
				Query:         "rate(http_requests_total[5m])",
				Start:         "2025-01-01T00:00:00Z",
				End:           "2025-01-01T01:00:00Z",
				Step:          "30s",
				PrometheusURL: server.URL,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Status).To(Equal("success"))
		})
	})

	Describe("alerts", func() {
		It("lists firing alerts", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/api/v1/alerts"))
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"status":"success","data":{"alerts":[{"labels":{"alertname":"HighLatency"},"state":"firing"}]}}`)
			}))

			resp, err := m.query(context.Background(), metricsRequest{
				Operation:     "alerts",
				PrometheusURL: server.URL,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring("HighLatency"))
			Expect(resp.Result).To(ContainSubstring("firing"))
		})
	})

	Describe("targets", func() {
		It("lists scrape targets", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/api/v1/targets"))
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"status":"success","data":{"activeTargets":[{"health":"up","labels":{"job":"api"}}]}}`)
			}))

			resp, err := m.query(context.Background(), metricsRequest{
				Operation:     "targets",
				PrometheusURL: server.URL,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring("api"))
		})
	})

	Describe("series", func() {
		It("requires match parameter", func() {
			_, err := m.query(context.Background(), metricsRequest{
				Operation: "series",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("match"))
		})
	})

	Describe("labels", func() {
		It("lists all label names", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/api/v1/labels"))
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"status":"success","data":["__name__","job","instance"]}`)
			}))

			resp, err := m.query(context.Background(), metricsRequest{
				Operation:     "labels",
				PrometheusURL: server.URL,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring("job"))
		})

		It("lists values for a specific label", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/api/v1/label/job/values"))
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"status":"success","data":["prometheus","node-exporter","api-server"]}`)
			}))

			resp, err := m.query(context.Background(), metricsRequest{
				Operation:     "labels",
				LabelName:     "job",
				PrometheusURL: server.URL,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring("api-server"))
		})
	})

	Describe("error handling", func() {
		It("handles non-200 responses", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"status":"error","error":"bad query"}`)
			}))

			resp, err := m.query(context.Background(), metricsRequest{
				Operation:     "instant_query",
				Query:         "invalid{",
				PrometheusURL: server.URL,
			})
			Expect(err).NotTo(HaveOccurred()) // non-200 is not an error, just reported
			Expect(resp.Status).To(Equal("HTTP 400"))
		})
	})

	Describe("provider", func() {
		It("creates tool via provider", func() {
			p := NewToolProvider()
			tools := p.GetTools()
			Expect(tools).To(HaveLen(1))
		})
	})
})
