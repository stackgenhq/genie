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

var _ = Describe("TracingMiddleware", func() {
	It("should pass through successful calls", func() {
		mw := toolwrap.TracingMiddleware()
		handler := mw.Wrap(passthrough("ok"))
		result, err := handler(context.Background(), tc("test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should pass through errors", func() {
		mw := toolwrap.TracingMiddleware()
		handler := mw.Wrap(failing(errors.New("fail")))
		_, err := handler(context.Background(), tc("test"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("fail"))
	})

	It("should not alter the result or error", func() {
		mw := toolwrap.TracingMiddleware()
		handler := mw.Wrap(passthrough(map[string]string{"key": "value"}))
		result, err := handler(context.Background(), tc("complex_tool"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(map[string]string{"key": "value"}))
	})
})
