// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"encoding/json"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/slack-go/slack"

	"github.com/stackgenhq/genie/pkg/messenger"
)

var _ = Describe("extractBlocks", func() {
	Describe("nil / missing metadata", func() {
		It("returns nil for nil metadata", func() {
			Expect(extractBlocks(nil)).To(BeNil())
		})

		It("returns nil when blocks key is absent", func() {
			meta := map[string]any{"other": "value"}
			Expect(extractBlocks(meta)).To(BeNil())
		})
	})

	Describe("typed []slack.Block pass-through", func() {
		It("returns the same slice unchanged", func() {
			blocks := []slack.Block{
				slack.NewDividerBlock(),
			}
			result := extractBlocks(map[string]any{"blocks": blocks})
			Expect(result).To(HaveLen(1))
		})

		It("returns nil for empty typed slice", func() {
			result := extractBlocks(map[string]any{"blocks": []slack.Block{}})
			Expect(result).To(BeEmpty())
		})
	})

	Describe("JSON round-trip from golden testdata", func() {
		It("parses blocks.json into typed slack blocks", func() {
			raw := loadGoldenBlocks("testdata/blocks.json")
			meta := map[string]any{"blocks": raw}
			result := extractBlocks(meta)
			Expect(result).NotTo(BeEmpty())
			// Verify the first block is a section
			Expect(result[0].BlockType()).To(Equal(slack.MBTSection))
		})
	})

	Describe("JSON round-trip from []any maps (HITL store pattern)", func() {
		It("parses generic map blocks into typed slack blocks", func() {
			blocks := []any{
				map[string]any{
					"type": "section",
					"text": map[string]any{
						"type": "mrkdwn",
						"text": "Hello",
					},
				},
				map[string]any{
					"type": "divider",
				},
			}
			meta := map[string]any{"blocks": blocks}
			result := extractBlocks(meta)
			Expect(result).To(HaveLen(2))
			Expect(result[0].BlockType()).To(Equal(slack.MBTSection))
			Expect(result[1].BlockType()).To(Equal(slack.MBTDivider))
		})
	})

	Describe("invalid data", func() {
		It("returns nil for unmarshalable data", func() {
			meta := map[string]any{"blocks": make(chan int)}
			Expect(extractBlocks(meta)).To(BeNil())
		})

		It("returns nil for non-block JSON", func() {
			meta := map[string]any{"blocks": "just a string"}
			result := extractBlocks(meta)
			// A plain string marshals to JSON fine, but won't unmarshal
			// into slack.Blocks — BlockSet will be empty.
			Expect(result).To(BeEmpty())
		})
	})
})

var _ = Describe("FormatApproval", func() {
	var m *Messenger

	BeforeEach(func() {
		m = New(Config{}, "", nil)
	})

	It("populates blocks metadata with correct structure", func() {
		req := messenger.SendRequest{
			Channel: messenger.Channel{ID: "C12345"},
		}
		info := messenger.ApprovalInfo{
			ID:       "approval-001",
			ToolName: "write_file",
			Args:     `{"path": "/tmp/test.txt"}`,
		}

		result := m.FormatApproval(req, info)

		Expect(result.Metadata).To(HaveKey("blocks"))
		blocks, ok := result.Metadata["blocks"].([]any)
		Expect(ok).To(BeTrue())
		// Should have: header, args section, divider, actions, context = 5
		Expect(blocks).To(HaveLen(5))

		// Verify header block type
		header := blocks[0].(map[string]any)
		Expect(header["type"]).To(Equal("header"))
	})

	It("includes justification section when feedback is present", func() {
		req := messenger.SendRequest{
			Channel: messenger.Channel{ID: "C12345"},
		}
		info := messenger.ApprovalInfo{
			ID:       "approval-002",
			ToolName: "exec_command",
			Args:     `{"cmd": "rm -rf /"}`,
			Feedback: "Need to clean up temporary files",
		}

		result := m.FormatApproval(req, info)

		blocks := result.Metadata["blocks"].([]any)
		// Should have: header, justification, args, divider, actions, context = 6
		Expect(blocks).To(HaveLen(6))

		// Second block should be the justification section
		justification := blocks[1].(map[string]any)
		Expect(justification["type"]).To(Equal("section"))
		text := justification["text"].(map[string]any)
		Expect(text["text"]).To(ContainSubstring("Need to clean up"))
	})

	It("sets plaintext fallback content", func() {
		req := messenger.SendRequest{}
		info := messenger.ApprovalInfo{
			ID:       "id-1",
			ToolName: "shell",
			Args:     "{}",
		}

		result := m.FormatApproval(req, info)
		Expect(result.Content.Text).To(ContainSubstring("Approval Required"))
		Expect(result.Content.Text).To(ContainSubstring("shell"))
	})

	It("round-trips the generated blocks through extractBlocks", func() {
		req := messenger.SendRequest{}
		info := messenger.ApprovalInfo{
			ID:       "round-trip-001",
			ToolName: "write_file",
			Args:     `{"path": "/tmp/out.txt"}`,
			Feedback: "Creating output file",
		}

		result := m.FormatApproval(req, info)
		// The blocks in metadata are []any maps — simulate what Send does
		sdkBlocks := extractBlocks(result.Metadata)
		Expect(sdkBlocks).NotTo(BeEmpty())
		// Header is the first block
		Expect(sdkBlocks[0].BlockType()).To(Equal(slack.MBTHeader))
	})
})

var _ = Describe("FormatClarification", func() {
	var m *Messenger

	BeforeEach(func() {
		m = New(Config{}, "", nil)
	})

	It("formats a rich Block Kit clarification message", func() {
		req := messenger.SendRequest{
			Channel: messenger.Channel{ID: "C12345"},
			Content: messenger.MessageContent{Text: "original text"},
		}
		info := messenger.ClarificationInfo{
			RequestID: "clr-001",
			Question:  "What is the target environment?",
			Context:   "Deploying the application",
		}

		result := m.FormatClarification(req, info)

		// Content text is overwritten with the formatted question
		Expect(result.Content.Text).To(ContainSubstring("Question from Genie"))
		Expect(result.Content.Text).To(ContainSubstring("What is the target environment?"))
		// Blocks metadata is populated
		Expect(result.Metadata).To(HaveKey("blocks"))
		blocks, ok := result.Metadata["blocks"].([]any)
		Expect(ok).To(BeTrue())
		// header + context section + question section + divider + footer = 5
		Expect(blocks).To(HaveLen(5))
	})

	It("preserves existing metadata alongside blocks", func() {
		req := messenger.SendRequest{
			Channel:  messenger.Channel{ID: "C12345"},
			Content:  messenger.MessageContent{Text: "question text"},
			Metadata: map[string]any{"key": "value"},
		}
		info := messenger.ClarificationInfo{
			RequestID: "clr-002",
			Question:  "Which region?",
		}

		result := m.FormatClarification(req, info)
		Expect(result.Metadata).To(HaveKeyWithValue("key", "value"))
		Expect(result.Metadata).To(HaveKey("blocks"))
	})
})

// loadGoldenBlocks reads a golden JSON file and returns the "blocks" value as []any.
func loadGoldenBlocks(path string) []any {
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())

	var wrapper map[string]json.RawMessage
	Expect(json.Unmarshal(data, &wrapper)).To(Succeed())

	var blocks []any
	Expect(json.Unmarshal(wrapper["blocks"], &blocks)).To(Succeed())
	return blocks
}
