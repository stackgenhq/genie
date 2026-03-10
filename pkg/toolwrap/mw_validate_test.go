// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("InputValidationMiddleware", func() {
	makeDeclFn := func(name string, required []string) func(string) *tool.Declaration {
		return func(n string) *tool.Declaration {
			if n != name {
				return nil
			}
			return &tool.Declaration{
				Name: name,
				InputSchema: &tool.Schema{
					Required: required,
				},
			}
		}
	}

	It("should pass through valid args with all required fields", func() {
		mw := toolwrap.InputValidationMiddleware(makeDeclFn("write_file", []string{"path", "content"}))
		handler := mw.Wrap(passthrough("ok"))

		tc := &toolwrap.ToolCallContext{
			ToolName: "write_file",
			Args:     []byte(`{"path":"/tmp/a","content":"hello"}`),
		}
		result, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should reject when required field is missing", func() {
		mw := toolwrap.InputValidationMiddleware(makeDeclFn("write_file", []string{"path", "content"}))
		handler := mw.Wrap(passthrough("should not reach"))

		tc := &toolwrap.ToolCallContext{
			ToolName: "write_file",
			Args:     []byte(`{"path":"/tmp/a"}`),
		}
		_, err := handler(context.Background(), tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing required argument"))
		Expect(err.Error()).To(ContainSubstring("content"))
	})

	It("should reject invalid JSON", func() {
		mw := toolwrap.InputValidationMiddleware(makeDeclFn("tool", nil))
		handler := mw.Wrap(passthrough("should not reach"))

		tc := &toolwrap.ToolCallContext{
			ToolName: "tool",
			Args:     []byte(`{bad json`),
		}
		_, err := handler(context.Background(), tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid JSON"))
	})

	It("should pass through when no declaration exists", func() {
		mw := toolwrap.InputValidationMiddleware(func(_ string) *tool.Declaration {
			return nil
		})
		handler := mw.Wrap(passthrough("ok"))

		tc := &toolwrap.ToolCallContext{
			ToolName: "unknown",
			Args:     []byte(`{"anything":"goes"}`),
		}
		result, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should pass through empty args", func() {
		mw := toolwrap.InputValidationMiddleware(makeDeclFn("tool", []string{"required"}))
		handler := mw.Wrap(passthrough("ok"))

		tc := &toolwrap.ToolCallContext{
			ToolName: "tool",
			Args:     nil,
		}
		result, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should pass through when declFn is nil", func() {
		mw := toolwrap.InputValidationMiddleware(nil)
		handler := mw.Wrap(passthrough("ok"))

		tc := &toolwrap.ToolCallContext{
			ToolName: "tool",
			Args:     []byte(`{"key":"value"}`),
		}
		result, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})
})
