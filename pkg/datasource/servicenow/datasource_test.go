// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package servicenow_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/datasource/servicenow"
	"github.com/stackgenhq/genie/pkg/mcp/mcpfakes"
)

var _ = Describe("ServiceNowConnector", func() {
	var fakeReader *mcpfakes.FakeMCPResourceReader

	BeforeEach(func() {
		fakeReader = new(mcpfakes.FakeMCPResourceReader)
	})

	Describe("Name", func() {
		It("returns servicenow", func() {
			conn := servicenow.NewServiceNowConnector(fakeReader)
			Expect(conn.Name()).To(Equal("servicenow"))
		})
	})

	Describe("ListItems", func() {
		It("returns empty when no resources", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{},
			}, nil)

			conn := servicenow.NewServiceNowConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
		})

		It("returns error when reader fails", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(nil, fmt.Errorf("auth failed"))

			conn := servicenow.NewServiceNowConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).To(HaveOccurred())
			Expect(items).To(BeNil())
		})

		It("returns normalized items from ServiceNow MCP resources", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{
						URI:         "servicenow://incident/INC0012345",
						Name:        "Production database outage",
						Description: "MySQL primary node unresponsive",
						MIMEType:    "text/plain",
					},
				},
			}, nil)

			fakeReader.ReadResourceReturns(&mcp.ReadResourceResult{
				Contents: []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:  "servicenow://incident/INC0012345",
						Text: "Priority: P1\nStatus: In Progress\nDescription: MySQL primary node unresponsive.",
					},
				},
			}, nil)

			conn := servicenow.NewServiceNowConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.NewScope("servicenow", []string{"incident"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Source).To(Equal("servicenow"))
			Expect(items[0].Content).To(ContainSubstring("Production database outage"))
		})

		It("filters by table name when scope is set", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "servicenow://incident/INC001", Name: "Incident 1"},
					{URI: "servicenow://change_request/CHG001", Name: "Change 1"},
					{URI: "servicenow://kb_knowledge/KB001", Name: "KB Article 1"},
				},
			}, nil)

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{URI: req.Params.URI, Text: "Content"},
					},
				}, nil
			}

			conn := servicenow.NewServiceNowConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.NewScope("servicenow", []string{"incident", "change_request"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			Expect(fakeReader.ReadResourceCallCount()).To(Equal(2))
		})

		It("includes all resources when table names scope is empty", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "servicenow://incident/INC001", Name: "Incident"},
					{URI: "servicenow://kb_knowledge/KB001", Name: "KB"},
				},
			}, nil)

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{URI: req.Params.URI, Text: "Content"},
					},
				}, nil
			}

			conn := servicenow.NewServiceNowConnector(fakeReader)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
		})
	})
})
