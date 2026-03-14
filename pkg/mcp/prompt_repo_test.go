// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/security"
)

// fakePromptCaller implements MCPPromptCaller for unit tests.
type fakePromptCaller struct {
	listPromptsResult  *mcp.ListPromptsResult
	listPromptsError   error
	getPromptResult    *mcp.GetPromptResult
	getPromptError     error
	getPromptCallCount int
	lastGetReq         mcp.GetPromptRequest
	closed             bool
}

func (f *fakePromptCaller) ListPrompts(_ context.Context, _ mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error) {
	return f.listPromptsResult, f.listPromptsError
}

func (f *fakePromptCaller) GetPrompt(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	f.getPromptCallCount++
	f.lastGetReq = req
	return f.getPromptResult, f.getPromptError
}

func (f *fakePromptCaller) Close() error {
	f.closed = true
	return nil
}

var _ = Describe("PromptRepository", func() {
	var (
		repo *PromptRepository
		fake *fakePromptCaller
	)

	BeforeEach(func() {
		fake = &fakePromptCaller{}

		repo = NewPromptRepository(MCPServerConfig{Name: "serverA"}, nil)
		// Override the dialer with one that returns our fake.
		repo.dial = func(_ context.Context, _ MCPServerConfig, _ security.SecretProvider) (MCPPromptCaller, error) {
			return fake, nil
		}
	})

	Describe("Summaries", func() {
		It("should return prefixed summaries from the server", func() {
			fake.listPromptsResult = &mcp.ListPromptsResult{
				Prompts: []mcp.Prompt{
					{Name: "prompt1", Description: "Description 1"},
					{Name: "prompt2", Description: "Description 2"},
				},
			}

			summaries := repo.Summaries()
			Expect(summaries).To(HaveLen(2))
			Expect(summaries[0].Name).To(Equal("serverA_prompt1"))
			Expect(summaries[0].Description).To(Equal("Description 1"))
			Expect(summaries[1].Name).To(Equal("serverA_prompt2"))
			Expect(summaries[1].Description).To(Equal("Description 2"))
		})

		It("should return empty when dialer fails", func() {
			repo.dial = func(_ context.Context, _ MCPServerConfig, _ security.SecretProvider) (MCPPromptCaller, error) {
				return nil, errors.New("connection refused")
			}

			summaries := repo.Summaries()
			Expect(summaries).To(BeEmpty())
		})

		It("should return empty when ListPrompts fails", func() {
			fake.listPromptsError = errors.New("server error")

			summaries := repo.Summaries()
			Expect(summaries).To(BeEmpty())
		})

		It("should close the connection after use", func() {
			fake.listPromptsResult = &mcp.ListPromptsResult{}

			_ = repo.Summaries()
			Expect(fake.closed).To(BeTrue())
		})
	})

	Describe("Get", func() {
		It("should fetch the correct prompt using the stripped name", func() {
			fake.getPromptResult = &mcp.GetPromptResult{
				Description: "Fetched Description 1",
				Messages: []mcp.PromptMessage{
					{
						Role: mcp.RoleUser,
						Content: mcp.TextContent{
							Type: "text",
							Text: "Hello from server1",
						},
					},
				},
			}

			skillResp, err := repo.Get("serverA_prompt1")
			Expect(err).NotTo(HaveOccurred())
			Expect(skillResp.Summary.Name).To(Equal("serverA_prompt1"))
			Expect(skillResp.Summary.Description).To(Equal("Fetched Description 1"))
			Expect(skillResp.Body).To(ContainSubstring("Fetched Description 1"))
			Expect(skillResp.Body).To(ContainSubstring("Hello from server1"))

			// Verify the stripped name was used in the request.
			Expect(fake.getPromptCallCount).To(Equal(1))
			Expect(fake.lastGetReq.Params.Name).To(Equal("prompt1"))
		})

		It("should return an error if the name prefix does not match", func() {
			_, err := repo.Get("nonexistent_prompt")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("should return an error when dialer fails", func() {
			repo.dial = func(_ context.Context, _ MCPServerConfig, _ security.SecretProvider) (MCPPromptCaller, error) {
				return nil, errors.New("connection refused")
			}

			_, err := repo.Get("serverA_prompt1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to connect"))
		})

		It("should return an error when GetPrompt fails", func() {
			fake.getPromptError = errors.New("prompt not available")

			_, err := repo.Get("serverA_prompt1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get prompt"))
		})

		It("should close the connection after use", func() {
			fake.getPromptResult = &mcp.GetPromptResult{
				Description: "test",
				Messages:    []mcp.PromptMessage{},
			}

			_, _ = repo.Get("serverA_prompt1")
			Expect(fake.closed).To(BeTrue())
		})
	})

	Describe("Path", func() {
		It("should return os.ErrNotExist since prompts are remote", func() {
			_, err := repo.Path("serverA_prompt1")
			Expect(err).To(MatchError(os.ErrNotExist))
		})
	})
})
