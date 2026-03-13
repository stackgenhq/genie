// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"

	slackapi "github.com/slack-go/slack"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("markdownToBlocks", func() {
	It("returns nil for empty input", func(ctx context.Context) {
		Expect(markdownToBlocks(ctx, "")).To(BeNil())
	})

	It("returns nil or empty for whitespace-only input", func(ctx context.Context) {
		blocks := markdownToBlocks(ctx, "   \n\n  \t  ")
		Expect(blocks).To(BeNil())
	})

	It("converts headings to header blocks", func(ctx context.Context) {
		blocks := markdownToBlocks(ctx, "# Main Heading")
		Expect(blocks).To(HaveLen(1))
		Expect(blocks[0].BlockType()).To(Equal(slackapi.MBTHeader))
	})

	It("converts horizontal rules to divider blocks", func(ctx context.Context) {
		blocks := markdownToBlocks(ctx, "above\n\n---\n\nbelow")
		Expect(blocks).To(HaveLen(3))
		Expect(blocks[1].BlockType()).To(Equal(slackapi.MBTDivider))
	})

	It("converts standalone links to action button blocks", func(ctx context.Context) {
		blocks := markdownToBlocks(ctx, "[Click here](https://example.com)")
		Expect(blocks).To(HaveLen(1))
		Expect(blocks[0].BlockType()).To(Equal(slackapi.MBTAction))
	})

	It("converts a real LLM response into header, rich_text, and divider blocks", func(ctx context.Context) {
		input := `**Aiden** is a technical product/platform.

### 🔑 Key Details:

**Architecture**:
- Customers deploy a **local runner** in their own environment
- All data connections are **proxied through this local runner**

---

Would you like me to:
- Search for more documents?
- Help you build out a more complete POC plan?`

		blocks := markdownToBlocks(ctx, input)
		Expect(len(blocks)).To(BeNumerically(">=", 4))

		// Verify the structure contains the expected block types.
		typeSet := map[slackapi.MessageBlockType]bool{}
		for _, b := range blocks {
			typeSet[b.BlockType()] = true
		}
		Expect(typeSet).To(HaveKey(slackapi.MBTHeader))
		Expect(typeSet).To(HaveKey(slackapi.MBTDivider))
		Expect(typeSet).To(HaveKey(slackapi.MBTRichText))
	})
})
