// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package modelprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openai/openai-go/option"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// mockRoundTripper intercepts HTTP requests to provide fake responses.
type mockRoundTripper struct {
	HandlerFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.HandlerFunc(req)
}

var _ = Describe("OpenAICompletionModel", func() {
	var (
		ctx           context.Context
		modelName     string
		modelObj      model.Model
		mockRoundTrip func(req *http.Request) (*http.Response, error)
	)

	BeforeEach(func() {
		ctx = context.Background()
		modelName = "gpt-5.1-codex-mini"

		customTransport := &mockRoundTripper{
			HandlerFunc: func(req *http.Request) (*http.Response, error) {
				if mockRoundTrip != nil {
					return mockRoundTrip(req)
				}
				return nil, fmt.Errorf("no mock defined")
			},
		}

		httpClient := &http.Client{Transport: customTransport}

		modelObj = NewOpenAICompletionModel(
			modelName,
			option.WithHTTPClient(httpClient),
			option.WithAPIKey("test-key"),
		)
	})

	Describe("Info", func() {
		It("should return correct info", func() {
			info := modelObj.Info()
			Expect(info.Name).To(Equal(modelName))
		})
	})

	Describe("GenerateContent", func() {
		It("should return error when request is nil", func() {
			ch, err := modelObj.GenerateContent(ctx, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("request cannot be nil"))
			Expect(ch).To(BeNil())
		})

		It("should handle non-streaming successful request", func() {
			mockRoundTrip = func(req *http.Request) (*http.Response, error) {
				Expect(req.URL.Path).To(Equal("/v1/completions"))
				Expect(req.Method).To(Equal("POST"))

				bodyObj := map[string]interface{}{}
				err := json.NewDecoder(req.Body).Decode(&bodyObj)
				Expect(err).ToNot(HaveOccurred())

				Expect(bodyObj["model"]).To(Equal(modelName))
				Expect(bodyObj["prompt"]).To(Equal("Hi there\n")) // Single message appended with newline

				respJSON := `{
					"id": "cmpl-123",
					"object": "text_completion",
					"created": 1234567890,
					"model": "gpt-5.1-codex-mini",
					"choices": [
						{
							"text": "Hello! How can I help?",
							"index": 0,
							"logprobs": null,
							"finish_reason": "stop"
						}
					],
					"usage": {
						"prompt_tokens": 5,
						"completion_tokens": 7,
						"total_tokens": 12
					}
				}`

				header := make(http.Header)
				header.Set("Content-Type", "application/json")
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(respJSON)),
					Header:     header,
				}, nil
			}

			maxTokens := 50
			temp := float64(0.5)
			topP := float64(0.9)

			req := &model.Request{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: "Hi there"},
				},
				GenerationConfig: model.GenerationConfig{
					MaxTokens:   &maxTokens,
					Temperature: &temp,
					TopP:        &topP,
					Stop:        []string{"\n"},
				},
			}

			ch, err := modelObj.GenerateContent(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(ch).ToNot(BeNil())

			resp := <-ch
			Expect(resp.Error).To(BeNil())
			Expect(resp.Choices).To(HaveLen(1))
			Expect(resp.Choices[0].Message.Role).To(Equal(model.RoleAssistant))
			Expect(resp.Choices[0].Message.Content).To(Equal("Hello! How can I help?"))

			// Channel should be closed
			Eventually(ch).Should(BeClosed())
		})

		It("should handle empty choices in successful response", func() {
			mockRoundTrip = func(req *http.Request) (*http.Response, error) {
				respJSON := `{
					"id": "cmpl-123",
					"object": "text_completion",
					"created": 1234567890,
					"model": "gpt-5.1-codex-mini",
					"choices": []
				}`

				header := make(http.Header)
				header.Set("Content-Type", "application/json")
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(respJSON)),
					Header:     header,
				}, nil
			}

			req := &model.Request{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: "Hi"},
				},
			}

			ch, err := modelObj.GenerateContent(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			resp := <-ch
			Expect(resp.Error).To(BeNil())
			Expect(resp.Choices).To(HaveLen(1))
			Expect(resp.Choices[0].Message.Content).To(Equal(""))
		})

		It("should return error from non-streaming client failure", func() {
			mockRoundTrip = func(req *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("network error")
			}

			req := &model.Request{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: "Fail me"},
				},
			}

			ch, err := modelObj.GenerateContent(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("network error"))
			Expect(ch).To(BeNil())
		})

		It("should handle API error (400) in non-streaming", func() {
			mockRoundTrip = func(req *http.Request) (*http.Response, error) {
				respJSON := `{"error": {"message": "invalid request", "type": "invalid_request_error"}}`
				header := make(http.Header)
				header.Set("Content-Type", "application/json")
				return &http.Response{
					StatusCode: 400,
					Body:       io.NopCloser(bytes.NewBufferString(respJSON)),
					Header:     header,
				}, nil
			}

			req := &model.Request{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: "Bad"},
				},
			}

			ch, err := modelObj.GenerateContent(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid request"))
			Expect(ch).To(BeNil())
		})

		It("should handle streaming successful request", func() {
			mockRoundTrip = func(req *http.Request) (*http.Response, error) {
				Expect(req.URL.Path).To(Equal("/v1/completions"))
				Expect(req.Method).To(Equal("POST"))

				// Validate that stream is set to true
				bodyObj := map[string]interface{}{}
				err := json.NewDecoder(req.Body).Decode(&bodyObj)
				Expect(err).ToNot(HaveOccurred())
				Expect(bodyObj["stream"]).To(BeTrue())

				sseData := []string{
					`data: {"id": "cmpl-123", "object": "text_completion", "model": "gpt-5.1-codex-mini", "choices": [{"text": "Stream", "index": 0, "finish_reason": null}]}`,
					`data: {"id": "cmpl-123", "object": "text_completion", "model": "gpt-5.1-codex-mini", "choices": [{"text": "ing", "index": 0, "finish_reason": null}]}`,
					`data: {"id": "cmpl-123", "object": "text_completion", "model": "gpt-5.1-codex-mini", "choices": [{"text": "!", "index": 0, "finish_reason": "stop"}]}`,
					`data: [DONE]`,
				}

				respBody := ""
				for _, line := range sseData {
					respBody += line + "\n\n"
				}

				header := make(http.Header)
				header.Set("Content-Type", "text/event-stream")

				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(respBody)),
					Header:     header,
				}, nil
			}

			req := &model.Request{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: "Stream this"},
				},
				GenerationConfig: model.GenerationConfig{
					Stream: true,
				},
			}

			ch, err := modelObj.GenerateContent(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(ch).ToNot(BeNil())

			var fullContent string
			for resp := range ch {
				Expect(resp.Error).To(BeNil())
				if len(resp.Choices) > 0 {
					fullContent += resp.Choices[0].Message.Content
				}
			}

			Expect(fullContent).To(Equal("Streaming!"))
		})

		It("should handle streaming API error", func() {
			mockRoundTrip = func(req *http.Request) (*http.Response, error) {
				respJSON := `{"error": {"message": "stream error", "type": "server_error"}}`
				header := make(http.Header)
				header.Set("Content-Type", "application/json")
				return &http.Response{
					StatusCode: 500,
					Body:       io.NopCloser(bytes.NewBufferString(respJSON)),
					Header:     header,
				}, nil
			}

			req := &model.Request{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: "Stream this"},
				},
				GenerationConfig: model.GenerationConfig{
					Stream: true,
				},
			}

			ch, err := modelObj.GenerateContent(ctx, req)
			Expect(err).ToNot(HaveOccurred()) // Returns the channel immediately
			Expect(ch).ToNot(BeNil())

			resp := <-ch
			Expect(resp.Error).ToNot(BeNil())
			Expect(resp.Error.Message).To(ContainSubstring("stream error"))

			// Channel should be closed
			Eventually(ch).Should(BeClosed())
		})
	})
})
