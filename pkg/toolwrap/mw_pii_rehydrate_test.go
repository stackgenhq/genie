// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap_test

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/pii"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("PIIRehydrateMiddleware", func() {
	It("rehydrates [HIDDEN:hash] in tool args before execution", func() {
		// Simulate: user message had "send to real@example.com", redacted to [HIDDEN:53233f];
		// LLM echoed that in tool call; we must rehydrate so email_send gets real address.
		replacer := strings.NewReplacer("[HIDDEN:53233f]", "real@example.com")
		ctx := pii.WithReplacer(context.Background(), replacer)

		argsWithPlaceholder := `{"to":["[HIDDEN:53233f]"],"subject":"Hi","body":"Hello"}`
		tc := &toolwrap.ToolCallContext{
			ToolName: "email_send",
			Args:     []byte(argsWithPlaceholder),
		}

		var capturedArgs []byte
		handler := toolwrap.Handler(func(_ context.Context, c *toolwrap.ToolCallContext) (any, error) {
			capturedArgs = append([]byte(nil), c.Args...)
			return "ok", nil
		})

		mw := toolwrap.PIIRehydrateMiddleware()
		_, err := mw.Wrap(handler)(ctx, tc)
		Expect(err).NotTo(HaveOccurred())

		Expect(string(capturedArgs)).To(ContainSubstring("real@example.com"))
		Expect(string(capturedArgs)).NotTo(ContainSubstring("[HIDDEN:53233f]"))
	})

	It("passes args unchanged when context has no replacer", func() {
		ctx := context.Background()
		argsWithPlaceholder := `{"to":["[HIDDEN:53233f]"]}`
		tc := &toolwrap.ToolCallContext{ToolName: "email_send", Args: []byte(argsWithPlaceholder)}

		var capturedArgs []byte
		handler := toolwrap.Handler(func(_ context.Context, c *toolwrap.ToolCallContext) (any, error) {
			capturedArgs = append([]byte(nil), c.Args...)
			return nil, nil
		})

		mw := toolwrap.PIIRehydrateMiddleware()
		_, _ = mw.Wrap(handler)(ctx, tc)
		Expect(string(capturedArgs)).To(Equal(argsWithPlaceholder))
	})
})
