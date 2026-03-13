// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package jira_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/datasource/jira"
	"github.com/stackgenhq/genie/pkg/mcp/mcpfakes"
)

var _ = Describe("JiraConnector", func() {
	var fakeReader *mcpfakes.FakeMCPResourceReader

	BeforeEach(func() {
		fakeReader = new(mcpfakes.FakeMCPResourceReader)
	})

	Describe("Name", func() {
		It("returns jira", func() {
			conn := jira.NewJiraConnector(fakeReader)
			Expect(conn.Name()).To(Equal("jira"))
		})
	})

	Describe("ListItems", func() {
		It("returns empty when no resources", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{},
			}, nil)

			conn := jira.NewJiraConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
		})

		It("returns error when reader fails", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(nil, fmt.Errorf("connection failed"))

			conn := jira.NewJiraConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).To(HaveOccurred())
			Expect(items).To(BeNil())
		})

		It("returns normalized items from Jira MCP resources", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{
						URI:      "jira://issue/ENG-42",
						Name:     "Fix database timeout",
						MIMEType: "text/plain",
					},
				},
			}, nil)

			fakeReader.ReadResourceReturns(&mcp.ReadResourceResult{
				Contents: []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:  "jira://issue/ENG-42",
						Text: "Database connections timing out under load. Need to increase pool size.",
					},
				},
			}, nil)

			conn := jira.NewJiraConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.NewScope("jira", []string{"ENG"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal("jira:jira://issue/ENG-42"))
			Expect(items[0].Source).To(Equal("jira"))
			Expect(items[0].Content).To(ContainSubstring("Fix database timeout"))
			Expect(items[0].SourceRef.Type).To(Equal("jira"))
		})

		It("filters by project key when scope is set", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "jira://issue/ENG-1", Name: "ENG-1 Fix login"},
					{URI: "jira://issue/OPS-1", Name: "OPS-1 Deploy fix"},
					{URI: "jira://issue/ENG-2", Name: "ENG-2 Add tests"},
				},
			}, nil)

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{URI: req.Params.URI, Text: "Body for " + req.Params.URI},
					},
				}, nil
			}

			conn := jira.NewJiraConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.NewScope("jira", []string{"ENG"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			Expect(fakeReader.ReadResourceCallCount()).To(Equal(2))
		})

		It("includes all resources when project keys scope is empty", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "jira://issue/ENG-1", Name: "ENG Issue"},
					{URI: "jira://issue/OPS-1", Name: "OPS Issue"},
				},
			}, nil)

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{URI: req.Params.URI, Text: "Body"},
					},
				}, nil
			}

			conn := jira.NewJiraConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
		})
	})
})
