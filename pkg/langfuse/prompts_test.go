package langfuse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/appcd-dev/go-lib/ttlcache"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Prompts", func() {
	Describe("GetPrompt", func() {
		var (
			server     *httptest.Server
			c          *client
			origHost   string
			origPublic string
			origSecret string
		)

		BeforeEach(func() {
			// Save original globals
			origHost = LangfuseHost
			origPublic = LangfusePublicKey
			origSecret = LangfuseSecretKey

			// Default setup: valid keys
			LangfusePublicKey = "pk"
			LangfuseSecretKey = "sk"
		})

		AfterEach(func() {
			if server != nil {
				server.Close()
			}
			// Restore globals
			LangfuseHost = origHost
			LangfusePublicKey = origPublic
			LangfuseSecretKey = origSecret
		})

		Context("when keys are missing", func() {
			It("returns default value when public key is empty", func() {
				LangfusePublicKey = ""
				c = &client{
					httpClient: http.DefaultClient,
				}
				p := c.GetPrompt(context.Background(), "test-prompt", "default value")
				Expect(p).To(Equal("default value"))
			})

			It("returns default value when secret key is empty", func() {
				LangfuseSecretKey = ""
				c = &client{
					httpClient: http.DefaultClient,
				}
				p := c.GetPrompt(context.Background(), "test-prompt", "default value")
				Expect(p).To(Equal("default value"))
			})
		})

		Context("with mocked server", func() {
			var (
				serverHandler http.HandlerFunc
			)

			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if serverHandler != nil {
						serverHandler(w, r)
					}
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
				c.promptsCache = ttlcache.NewItem(c.getAllPrompts, 10*time.Minute)
			})

			DescribeTable("GetPrompt Scenarios",
				func(handler http.HandlerFunc, expectedResult string) {
					serverHandler = handler
					p := c.GetPrompt(context.Background(), "test-prompt", "default value")
					Expect(p).To(Equal(expectedResult))
				},
				Entry("when listing prompts returns 404",
					func(w http.ResponseWriter, r *http.Request) {
						w.WriteHeader(http.StatusNotFound)
					},
					"default value",
				),
				Entry("when listing prompts returns 500",
					func(w http.ResponseWriter, r *http.Request) {
						w.WriteHeader(http.StatusInternalServerError)
					},
					"default value",
				),
				Entry("when API returns success",
					func(w http.ResponseWriter, r *http.Request) {
						switch r.URL.Path {
						case "/api/public/v2/prompts":
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write([]byte(`{
							"data": [{"name": "test-prompt", "type": "text", "versions": [1]}],
							"meta": {"page": 1, "limit": 10, "totalItems": 1, "totalPages": 1}
						}`))
						case "/api/public/v2/prompts/test-prompt":
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write([]byte(`{"prompt": "remote content"}`))
						default:
							w.WriteHeader(http.StatusNotFound)
						}
					},
					"remote content",
				),
				Entry("when response JSON is invalid",
					func(w http.ResponseWriter, r *http.Request) {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`invalid json`))
					},
					"default value",
				),
				Entry("when prompt is not in the list",
					func(w http.ResponseWriter, r *http.Request) {
						switch r.URL.Path {
						case "/api/public/v2/prompts":
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write([]byte(`{
							"data": [{"name": "other-prompt", "type": "text", "versions": [1]}],
							"meta": {"page": 1, "limit": 10, "totalItems": 1, "totalPages": 1}
						}`))
						case "/api/public/v2/prompts/other-prompt":
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write([]byte(`{"prompt": "other content"}`))
						default:
							w.WriteHeader(http.StatusNotFound)
						}
					},
					"default value",
				),
			)
		})
	})

	Describe("getAllPromptNames", func() {
		var (
			server     *httptest.Server
			c          *client
			origHost   string
			origPublic string
			origSecret string
		)

		BeforeEach(func() {
			origHost = LangfuseHost
			origPublic = LangfusePublicKey
			origSecret = LangfuseSecretKey
			LangfusePublicKey = "pk"
			LangfuseSecretKey = "sk"
		})

		AfterEach(func() {
			if server != nil {
				server.Close()
			}
			LangfuseHost = origHost
			LangfusePublicKey = origPublic
			LangfuseSecretKey = origSecret
		})

		Context("with mocked server", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/api/public/v2/prompts"))
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{
					"data": [
						{"name": "prompt1", "type": "text", "versions": [1, 2]},
						{"name": "prompt2", "type": "text", "versions": [1]}
					],
					"meta": {"page": 1, "limit": 10, "totalItems": 2, "totalPages": 1}
				}`))
					Expect(err).NotTo(HaveOccurred())
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
			})

			It("returns all prompt names", func() {
				names, err := c.getAllPromptNames(context.Background())
				Expect(err).NotTo(HaveOccurred())
				Expect(names).To(ConsistOf("prompt1", "prompt2"))
			})
		})

		Context("when server returns error", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
			})

			It("returns error", func() {
				names, err := c.getAllPromptNames(context.Background())
				Expect(err).To(HaveOccurred())
				Expect(names).To(BeNil())
			})
		})

		Context("when response is invalid JSON", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`not valid json`))
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
			})

			It("returns error", func() {
				names, err := c.getAllPromptNames(context.Background())
				Expect(err).To(HaveOccurred())
				Expect(names).To(BeNil())
			})
		})
	})

	Describe("getPromptByName", func() {
		var (
			server     *httptest.Server
			c          *client
			origHost   string
			origPublic string
			origSecret string
		)

		BeforeEach(func() {
			origHost = LangfuseHost
			origPublic = LangfusePublicKey
			origSecret = LangfuseSecretKey
			LangfusePublicKey = "pk"
			LangfuseSecretKey = "sk"
		})

		AfterEach(func() {
			if server != nil {
				server.Close()
			}
			LangfuseHost = origHost
			LangfusePublicKey = origPublic
			LangfuseSecretKey = origSecret
		})

		Context("with successful response", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/api/public/v2/prompts/test-prompt"))
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"prompt": "This is the prompt content"}`))
					Expect(err).NotTo(HaveOccurred())
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
			})

			It("returns the prompt content", func() {
				content, err := c.getPromptByName(context.Background(), "test-prompt")
				Expect(err).NotTo(HaveOccurred())
				Expect(content).To(Equal("This is the prompt content"))
			})
		})

		Context("when server returns 404", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
			})

			It("returns error", func() {
				content, err := c.getPromptByName(context.Background(), "missing-prompt")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unexpected status code: 404"))
				Expect(content).To(BeEmpty())
			})
		})

		Context("when response is invalid JSON", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`not valid json`))
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
			})

			It("returns error", func() {
				content, err := c.getPromptByName(context.Background(), "test-prompt")
				Expect(err).To(HaveOccurred())
				Expect(content).To(BeEmpty())
			})
		})
	})

	Describe("getAllPrompts", func() {
		var (
			server     *httptest.Server
			c          *client
			origHost   string
			origPublic string
			origSecret string
		)

		BeforeEach(func() {
			origHost = LangfuseHost
			origPublic = LangfusePublicKey
			origSecret = LangfuseSecretKey
			LangfusePublicKey = "pk"
			LangfuseSecretKey = "sk"
		})

		AfterEach(func() {
			if server != nil {
				server.Close()
			}
			LangfuseHost = origHost
			LangfusePublicKey = origPublic
			LangfuseSecretKey = origSecret
		})

		Context("with successful responses for all prompts", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/public/v2/prompts":
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{
						"data": [
							{"name": "architect-prompt", "type": "text", "versions": [1]},
							{"name": "ops-prompt", "type": "text", "versions": [1, 2]}
						],
						"meta": {"page": 1, "limit": 10, "totalItems": 2, "totalPages": 1}
					}`))
					case "/api/public/v2/prompts/architect-prompt":
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{"prompt": "You are an architect..."}`))
					case "/api/public/v2/prompts/ops-prompt":
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{"prompt": "You are an ops engineer..."}`))
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
			})

			It("returns all prompts with their content", func() {
				prompts, err := c.getAllPrompts(context.Background())
				Expect(err).NotTo(HaveOccurred())
				Expect(prompts).To(HaveLen(2))
				Expect(prompts["architect-prompt"]).To(Equal("You are an architect..."))
				Expect(prompts["ops-prompt"]).To(Equal("You are an ops engineer..."))
			})
		})

		Context("when listing prompts fails", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
			})

			It("returns error", func() {
				prompts, err := c.getAllPrompts(context.Background())
				Expect(err).To(HaveOccurred())
				Expect(prompts).To(BeNil())
			})
		})

		Context("when some individual prompts fail to fetch", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/public/v2/prompts":
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{
						"data": [
							{"name": "good-prompt", "type": "text", "versions": [1]},
							{"name": "bad-prompt", "type": "text", "versions": [1]}
						],
						"meta": {"page": 1, "limit": 10, "totalItems": 2, "totalPages": 1}
					}`))
					case "/api/public/v2/prompts/good-prompt":
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{"prompt": "Good prompt content"}`))
					case "/api/public/v2/prompts/bad-prompt":
						w.WriteHeader(http.StatusInternalServerError)
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
			})

			It("skips failed prompts and returns successful ones", func() {
				prompts, err := c.getAllPrompts(context.Background())
				Expect(err).NotTo(HaveOccurred())
				Expect(prompts).To(HaveLen(1))
				Expect(prompts["good-prompt"]).To(Equal("Good prompt content"))
				Expect(prompts).NotTo(HaveKey("bad-prompt"))
			})
		})

		Context("when prompt content is empty", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/public/v2/prompts":
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{
						"data": [
							{"name": "empty-prompt", "type": "text", "versions": [1]}
						],
						"meta": {"page": 1, "limit": 10, "totalItems": 1, "totalPages": 1}
					}`))
					case "/api/public/v2/prompts/empty-prompt":
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{"prompt": ""}`))
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				LangfuseHost = server.URL
				c = &client{
					httpClient: server.Client(),
				}
			})

			It("skips prompts with empty content", func() {
				prompts, err := c.getAllPrompts(context.Background())
				Expect(err).NotTo(HaveOccurred())
				Expect(prompts).To(BeEmpty())
			})
		})
	})
})
