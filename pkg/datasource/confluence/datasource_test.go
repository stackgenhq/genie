// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package confluence_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/datasource/confluence"
	"github.com/stackgenhq/genie/pkg/mcp/mcpfakes"
)

var _ = Describe("ConfluenceConnector", func() {
	var fakeReader *mcpfakes.FakeMCPResourceReader

	BeforeEach(func() {
		fakeReader = new(mcpfakes.FakeMCPResourceReader)
	})

	Describe("Name", func() {
		It("returns confluence", func() {
			conn := confluence.NewConfluenceConnector(fakeReader)
			Expect(conn.Name()).To(Equal("confluence"))
		})
	})

	Describe("ListItems", func() {
		It("returns empty when no resources", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{},
			}, nil)

			conn := confluence.NewConfluenceConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
		})

		It("returns error when reader fails", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(nil, fmt.Errorf("connection failed"))

			conn := confluence.NewConfluenceConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).To(HaveOccurred())
			Expect(items).To(BeNil())
		})

		It("returns normalized items from Confluence MCP resources", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{
						URI:         "confluence://ENG/page/12345",
						Name:        "Incident Response Runbook",
						Description: "Standard operating procedure for P1 incidents",
						MIMEType:    "text/html",
					},
				},
			}, nil)

			fakeReader.ReadResourceReturns(&mcp.ReadResourceResult{
				Contents: []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:  "confluence://ENG/page/12345",
						Text: "When a P1 incident occurs, follow these steps...",
					},
				},
			}, nil)

			conn := confluence.NewConfluenceConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.NewScope("confluence", []string{"ENG"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Source).To(Equal("confluence"))
			Expect(items[0].Metadata["description"]).To(Equal("Standard operating procedure for P1 incidents"))
		})

		It("filters by space key when scope is set", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "confluence://ENG/page/1", Name: "ENG Page"},
					{URI: "confluence://OPS/page/2", Name: "OPS Page"},
					{URI: "confluence://SALES/page/3", Name: "SALES Page"},
				},
			}, nil)

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{URI: req.Params.URI, Text: "Content"},
					},
				}, nil
			}

			conn := confluence.NewConfluenceConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.NewScope("confluence", []string{"ENG", "OPS"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			Expect(fakeReader.ReadResourceCallCount()).To(Equal(2))
		})
	})
})
