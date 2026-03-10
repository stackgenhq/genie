// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package youtubetranscript_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/tools/youtubetranscript"
)

var _ = Describe("youtube_transcript tool", func() {
	It("declares name and description", func() {
		t := youtubetranscript.NewTool()
		decl := t.Declaration()
		Expect(decl.Name).To(Equal("youtube_transcript"))
		Expect(decl.Description).To(ContainSubstring("YouTube"))
		Expect(decl.Description).To(ContainSubstring("transcript"))
	})

	It("returns error when video_url is empty", func(ctx context.Context) {
		t := youtubetranscript.NewTool()
		_, err := t.Call(ctx, []byte(`{"video_url":""}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("video_url is required"))
	})

	It("returns error for non-YouTube URL", func(ctx context.Context) {
		t := youtubetranscript.NewTool()
		_, err := t.Call(ctx, []byte(`{"video_url":"https://example.com/not-youtube"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not extract video ID"))
	})
})
