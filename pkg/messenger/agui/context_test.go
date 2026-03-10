// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/messenger"
	agui "github.com/stackgenhq/genie/pkg/messenger/agui"
)

var _ = Describe("Attachment Context Helpers", func() {
	Describe("WithAttachments / AttachmentsFromContext", func() {
		It("stores and retrieves attachments from context", func() {
			atts := []messenger.Attachment{
				{Name: "photo.png", ContentType: "image/png", Size: 1024, LocalPath: "/tmp/photo.png"},
				{Name: "voice.wav", ContentType: "audio/wav", Size: 2048, LocalPath: "/tmp/voice.wav"},
			}

			ctx := agui.WithAttachments(context.Background(), atts)
			retrieved := agui.AttachmentsFromContext(ctx)

			Expect(retrieved).To(HaveLen(2))
			Expect(retrieved[0].Name).To(Equal("photo.png"))
			Expect(retrieved[0].ContentType).To(Equal("image/png"))
			Expect(retrieved[0].LocalPath).To(Equal("/tmp/photo.png"))
			Expect(retrieved[1].Name).To(Equal("voice.wav"))
		})

		It("returns nil when no attachments in context", func() {
			atts := agui.AttachmentsFromContext(context.Background())
			Expect(atts).To(BeNil())
		})

		It("returns nil for a context with wrong value type", func() {
			ctx := context.WithValue(context.Background(), struct{}{}, "not-attachments")
			atts := agui.AttachmentsFromContext(ctx)
			Expect(atts).To(BeNil())
		})

		It("handles empty attachment slice", func() {
			ctx := agui.WithAttachments(context.Background(), []messenger.Attachment{})
			retrieved := agui.AttachmentsFromContext(ctx)
			Expect(retrieved).To(BeEmpty())
		})

		It("preserves all attachment fields", func() {
			att := messenger.Attachment{
				Name:        "receipt.pdf",
				ContentType: "application/pdf",
				Size:        50000,
				LocalPath:   "/tmp/receipt.pdf",
				URL:         "https://example.com/receipt.pdf",
			}

			ctx := agui.WithAttachments(context.Background(), []messenger.Attachment{att})
			retrieved := agui.AttachmentsFromContext(ctx)

			Expect(retrieved).To(HaveLen(1))
			Expect(retrieved[0].Name).To(Equal("receipt.pdf"))
			Expect(retrieved[0].ContentType).To(Equal("application/pdf"))
			Expect(retrieved[0].Size).To(Equal(int64(50000)))
			Expect(retrieved[0].LocalPath).To(Equal("/tmp/receipt.pdf"))
			Expect(retrieved[0].URL).To(Equal("https://example.com/receipt.pdf"))
		})
	})
})
