// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/mcp"
	"github.com/stackgenhq/genie/pkg/mcp/mcpfakes"
)

var _ = Describe("ResourceReader", func() {
	Describe("GetResourceReader", func() {
		It("returns nil and false for nil client", func() {
			var c *mcp.Client
			reader, ok := c.GetResourceReader("anything")
			Expect(reader).To(BeNil())
			Expect(ok).To(BeFalse())
		})

		It("returns nil and false for unknown server name", func() {
			c := mcp.NewClientForTest()
			reader, ok := c.GetResourceReader("nonexistent")
			Expect(reader).To(BeNil())
			Expect(ok).To(BeFalse())
		})

		It("returns the resource reader for a known server name", func() {
			c := mcp.NewClientForTest()
			fakeReader := new(mcpfakes.FakeMCPResourceReader)
			c.SetResourceReaderForTest("jira", fakeReader)

			reader, ok := c.GetResourceReader("jira")
			Expect(ok).To(BeTrue())
			Expect(reader).To(Equal(fakeReader))
		})

		It("returns different readers for different server names", func() {
			c := mcp.NewClientForTest()
			fakeJira := new(mcpfakes.FakeMCPResourceReader)
			fakeConfluence := new(mcpfakes.FakeMCPResourceReader)

			c.SetResourceReaderForTest("jira", fakeJira)
			c.SetResourceReaderForTest("confluence", fakeConfluence)

			reader1, ok1 := c.GetResourceReader("jira")
			Expect(ok1).To(BeTrue())
			Expect(reader1).To(Equal(fakeJira))

			reader2, ok2 := c.GetResourceReader("confluence")
			Expect(ok2).To(BeTrue())
			Expect(reader2).To(Equal(fakeConfluence))
		})
	})
})
