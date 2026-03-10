// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package youtubetranscript

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Provider Test", func() {
	It("provides the correct tools", func() {
		p := NewToolProvider()
		tools := p.GetTools()
		Expect(tools).To(HaveLen(1))
	})
})
