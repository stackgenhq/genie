// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package osutils_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/osutils"
)

var _ = Describe("SanitizeForFilename", func() {
	DescribeTable("produces filesystem-safe lowercase alphanumeric and underscore",
		func(input, expected string) {
			Expect(osutils.SanitizeForFilename(input)).To(Equal(expected))
		},
		Entry("empty", "", ""),
		Entry("already safe", "my_agent", "my_agent"),
		Entry("lowercase", "Agent", "agent"),
		Entry("spaces to underscore", "My Agent", "my_agent"),
		Entry("hyphens to underscore", "my-agent", "my_agent"),
		Entry("mixed spaces and hyphens", "My-Agent Name", "my_agent_name"),
		Entry("leading and trailing space", "  genie  ", "genie"),
		Entry("numbers allowed", "agent123", "agent123"),
		Entry("special chars dropped", "agent@dev!", "agentdev"),
		Entry("unicode dropped", "café", "caf"),
	)

	It("returns empty for input with no alphanumeric or space/hyphen", func() {
		Expect(osutils.SanitizeForFilename("***")).To(Equal(""))
		Expect(osutils.SanitizeForFilename("   ---   ")).To(Equal("___")) // spaces and hyphens become underscores
	})
})
