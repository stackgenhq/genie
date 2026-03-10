// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package pm_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/tools/pm"
	"github.com/stackgenhq/genie/pkg/tools/pm/pmfakes"
)

var _ = Describe("LinearConnector", func() {
	var fake *pmfakes.FakeService

	BeforeEach(func() {
		fake = new(pmfakes.FakeService)
	})

	Describe("Name", func() {
		It("returns linear", func() {
			conn := pm.NewLinearConnector(fake)
			Expect(conn.Name()).To(Equal("linear"))
		})
	})

	Describe("ListItems", func() {
		It("returns empty slice when ListIssues returns no issues", func(ctx context.Context) {
			fake.ListIssuesReturns([]*pm.Issue{}, nil)
			conn := pm.NewLinearConnector(fake)
			scope := datasource.Scope{}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
			Expect(fake.ListIssuesCallCount()).To(Equal(1))
		})

		It("returns normalized items for each issue", func(ctx context.Context) {
			fake.ListIssuesReturns([]*pm.Issue{
				{ID: "LIN-1", Title: "Fix login", Description: "Login is broken", Status: "In Progress", Assignee: "Alice", Labels: []string{"bug"}},
			}, nil)
			conn := pm.NewLinearConnector(fake)
			scope := datasource.Scope{}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal("linear:LIN-1"))
			Expect(items[0].Source).To(Equal("linear"))
			Expect(items[0].Content).To(ContainSubstring("Fix login"))
			Expect(items[0].Content).To(ContainSubstring("Login is broken"))
			Expect(items[0].Metadata["title"]).To(Equal("Fix login"))
			Expect(items[0].Metadata["status"]).To(Equal("In Progress"))
			Expect(items[0].Metadata["assignee"]).To(Equal("Alice"))
			Expect(items[0].Metadata["labels"]).To(Equal("bug"))
		})
	})
})
