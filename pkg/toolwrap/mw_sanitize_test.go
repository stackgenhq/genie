// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("OutputSanitizationMiddleware", func() {
	It("should redact matching patterns from output", func() {
		mw := toolwrap.OutputSanitizationMiddleware(
			map[string][]string{
				"env_reader": {"SECRET_KEY", "password"},
			}, "")
		handler := mw.Wrap(passthrough("TOKEN=SECRET_KEY_123 password=hunter2"))
		result, err := handler(context.Background(), tc("env_reader"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(ContainSubstring("[REDACTED]"))
		Expect(result).NotTo(ContainSubstring("SECRET_KEY"))
		Expect(result).NotTo(ContainSubstring("password"))
	})

	It("should use custom replacement string", func() {
		mw := toolwrap.OutputSanitizationMiddleware(
			map[string][]string{
				"read_file": {"api_key"},
			}, "***")
		handler := mw.Wrap(passthrough("my api_key is abc"))
		result, err := handler(context.Background(), tc("read_file"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("my *** is abc"))
	})

	It("should be case-insensitive", func() {
		mw := toolwrap.OutputSanitizationMiddleware(
			map[string][]string{
				"tool": {"secret"},
			}, "[X]")
		handler := mw.Wrap(passthrough("my SECRET is SECRET and Secret"))
		result, err := handler(context.Background(), tc("tool"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("my [X] is [X] and [X]"))
	})

	It("should pass through unconfigured tools", func() {
		mw := toolwrap.OutputSanitizationMiddleware(
			map[string][]string{
				"other_tool": {"secret"},
			}, "")
		handler := mw.Wrap(passthrough("my secret data"))
		result, err := handler(context.Background(), tc("unrelated_tool"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("my secret data"))
	})

	It("should pass through errors without sanitizing", func() {
		mw := toolwrap.OutputSanitizationMiddleware(
			map[string][]string{
				"tool": {"secret"},
			}, "")
		handler := mw.Wrap(failing(errors.New("secret error")))
		_, err := handler(context.Background(), tc("tool"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("secret error"))
	})

	It("should handle nil output", func() {
		mw := toolwrap.OutputSanitizationMiddleware(
			map[string][]string{
				"tool": {"secret"},
			}, "")
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			return nil, nil
		})
		result, err := handler(context.Background(), tc("tool"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeNil())
	})
})
