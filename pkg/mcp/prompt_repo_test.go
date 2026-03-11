// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakePromptCaller struct {
	getPromptReturns *mcp.GetPromptResult
	getPromptError   error
	callCount        int
	lastReq          mcp.GetPromptRequest
}

func (f *fakePromptCaller) GetPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	f.callCount++
	f.lastReq = req
	return f.getPromptReturns, f.getPromptError
}

var _ = Describe("PromptRepository", func() {
	var (
		client *Client
		repo   *PromptRepository
		fake1  *fakePromptCaller
		fake2  *fakePromptCaller
	)

	BeforeEach(func() {
		fake1 = &fakePromptCaller{}
		fake2 = &fakePromptCaller{}

		// Setup a fake client with some cached prompts
		client = &Client{
			prompts: []namespacedPrompt{
				{
					serverName: "serverA",
					prompt: mcp.Prompt{
						Name:        "prompt1",
						Description: "Description 1",
					},
					caller: fake1,
				},
				{
					serverName: "serverB",
					prompt: mcp.Prompt{
						Name:        "prompt2",
						Description: "Description 2",
					},
					caller: fake2,
				},
			},
		}

		repo = NewPromptRepository(client)
	})

	Describe("Summaries", func() {
		It("should return prefixed summaries from all servers", func() {
			summaries := repo.Summaries()
			Expect(summaries).To(HaveLen(2))
			
			Expect(summaries[0].Name).To(Equal("serverA_prompt1"))
			Expect(summaries[0].Description).To(Equal("Description 1"))
			
			Expect(summaries[1].Name).To(Equal("serverB_prompt2"))
			Expect(summaries[1].Description).To(Equal("Description 2"))
		})

		It("should handle nil client safely", func() {
			emptyRepo := NewPromptRepository(nil)
			Expect(emptyRepo.Summaries()).To(BeEmpty())
		})
	})

	Describe("Get", func() {
		It("should fetch the correct prompt from the corresponding caller", func() {
			fake1.getPromptReturns = &mcp.GetPromptResult{
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
			Expect(skillResp.Summary.Description).To(Equal("Description 1")) // Comes from cache
			Expect(skillResp.Body).To(ContainSubstring("Fetched Description 1"))
			Expect(skillResp.Body).To(ContainSubstring("Hello from server1"))

			// Verify the correct fake was called with the right param
			Expect(fake1.callCount).To(Equal(1))
			Expect(fake2.callCount).To(Equal(0))

			Expect(fake1.lastReq.Params.Name).To(Equal("prompt1"))
		})

		It("should return an error if the prompt is not found", func() {
			_, err := repo.Get("nonexistent_prompt")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Describe("Path", func() {
		It("should return os.ErrNotExist since prompts are remote", func() {
			_, err := repo.Path("serverA_prompt1")
			Expect(err).To(MatchError(os.ErrNotExist))
		})
	})
})
