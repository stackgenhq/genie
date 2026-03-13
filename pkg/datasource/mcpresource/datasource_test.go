// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcpresource_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/datasource/mcpresource"
	"github.com/stackgenhq/genie/pkg/mcp/mcpfakes"
)

var _ = Describe("MCPResourceConnector", func() {
	var fakeReader *mcpfakes.FakeMCPResourceReader

	BeforeEach(func() {
		fakeReader = new(mcpfakes.FakeMCPResourceReader)
	})

	Describe("Name", func() {
		It("returns the configured source name", func() {
			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira")
			Expect(conn.Name()).To(Equal("jira"))
		})

		It("returns servicenow when configured as such", func() {
			conn := mcpresource.NewMCPResourceConnector(fakeReader, "servicenow")
			Expect(conn.Name()).To(Equal("servicenow"))
		})
	})

	Describe("ListItems", func() {
		It("returns error when reader is nil", func(ctx context.Context) {
			conn := mcpresource.NewMCPResourceConnector(nil, "jira")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("MCP resource reader is nil"))
			Expect(items).To(BeNil())
		})

		It("returns nil when no resources are listed", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{},
			}, nil)

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
			Expect(fakeReader.ListResourcesCallCount()).To(Equal(1))
		})

		It("returns error when ListResources fails", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(nil, fmt.Errorf("connection refused"))

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("list resources"))
			Expect(items).To(BeNil())
		})

		It("returns normalized items for text resources", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{
						URI:         "jira://issue/ENG-123",
						Name:        "Fix login bug",
						Description: "Login page returns 500",
						MIMEType:    "text/plain",
					},
				},
			}, nil)

			fakeReader.ReadResourceReturns(&mcp.ReadResourceResult{
				Contents: []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:  "jira://issue/ENG-123",
						Text: "Login page returns 500 when clicking submit button",
					},
				},
			}, nil)

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))

			item := items[0]
			Expect(item.ID).To(Equal("jira:jira://issue/ENG-123"))
			Expect(item.Source).To(Equal("jira"))
			Expect(item.Content).To(ContainSubstring("Fix login bug"))
			Expect(item.Content).To(ContainSubstring("Login page returns 500"))
			Expect(item.Metadata["title"]).To(Equal("Fix login bug"))
			Expect(item.Metadata["uri"]).To(Equal("jira://issue/ENG-123"))
			Expect(item.Metadata["mime_type"]).To(Equal("text/plain"))
			Expect(item.Metadata["description"]).To(Equal("Login page returns 500"))
			Expect(item.SourceRef).NotTo(BeNil())
			Expect(item.SourceRef.Type).To(Equal("jira"))
			Expect(item.SourceRef.RefID).To(Equal("jira://issue/ENG-123"))
		})

		It("skips blob/binary resources", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "jira://attachment/1", Name: "screenshot.png"},
				},
			}, nil)

			fakeReader.ReadResourceReturns(&mcp.ReadResourceResult{
				Contents: []mcp.ResourceContents{
					mcp.BlobResourceContents{
						URI:  "jira://attachment/1",
						Blob: "base64data...",
					},
				},
			}, nil)

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})

		It("skips resources with empty text", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "jira://issue/ENG-999", Name: "Empty Issue"},
				},
			}, nil)

			fakeReader.ReadResourceReturns(&mcp.ReadResourceResult{
				Contents: []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:  "jira://issue/ENG-999",
						Text: "",
					},
				},
			}, nil)

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})

		It("continues on individual read errors (partial results)", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "jira://issue/ENG-1", Name: "Issue 1"},
					{URI: "jira://issue/ENG-2", Name: "Issue 2"},
				},
			}, nil)

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				if req.Params.URI == "jira://issue/ENG-1" {
					return nil, fmt.Errorf("read error")
				}
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{
							URI:  "jira://issue/ENG-2",
							Text: "Issue 2 body",
						},
					},
				}, nil
			}

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Content).To(ContainSubstring("Issue 2"))
		})

		It("handles multiple resources across pages", func(ctx context.Context) {
			callIdx := 0
			fakeReader.ListResourcesStub = func(_ context.Context, req mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
				callIdx++
				if callIdx == 1 {
					return &mcp.ListResourcesResult{
						PaginatedResult: mcp.PaginatedResult{
							NextCursor: "page2",
						},
						Resources: []mcp.Resource{
							{URI: "jira://issue/1", Name: "Issue 1"},
						},
					}, nil
				}
				return &mcp.ListResourcesResult{
					Resources: []mcp.Resource{
						{URI: "jira://issue/2", Name: "Issue 2"},
					},
				}, nil
			}

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{
							URI:  req.Params.URI,
							Text: "Body for " + req.Params.URI,
						},
					},
				}, nil
			}

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			Expect(fakeReader.ListResourcesCallCount()).To(Equal(2))
		})

		It("uses lastModified from resource annotations when available", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{
						URI:  "conf://page/123",
						Name: "Runbook",
						Annotated: mcp.Annotated{
							Annotations: &mcp.Annotations{
								LastModified: "2025-06-15T10:30:00Z",
							},
						},
					},
				},
			}, nil)

			fakeReader.ReadResourceReturns(&mcp.ReadResourceResult{
				Contents: []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:  "conf://page/123",
						Text: "Runbook content",
					},
				},
			}, nil)

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "confluence")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].UpdatedAt.Year()).To(Equal(2025))
			Expect(items[0].UpdatedAt.Month()).To(BeEquivalentTo(6))
		})

		It("handles nil ReadResource response gracefully", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "snow://incident/INC001", Name: "Incident"},
				},
			}, nil)

			fakeReader.ReadResourceReturns(nil, nil)

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "servicenow")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})
	})

	Describe("ScopeFilter", func() {
		It("filters resources based on scope when filter is set", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "jira://issue/ENG-1", Name: "ENG Issue"},
					{URI: "jira://issue/OPS-1", Name: "OPS Issue"},
					{URI: "jira://issue/ENG-2", Name: "Another ENG"},
				},
			}, nil)

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{
							URI:  req.Params.URI,
							Text: "Body for " + req.Params.URI,
						},
					},
				}, nil
			}

			// Only include resources containing "ENG" in URI
			filter := mcpresource.ScopeFilter(func(res mcp.Resource, scope datasource.Scope) bool {
				for _, key := range scope.Get("jira") {
					if key == "ENG" && (res.URI == "jira://issue/ENG-1" || res.URI == "jira://issue/ENG-2") {
						return true
					}
				}
				return false
			})

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira", mcpresource.WithScopeFilter(filter))
			items, err := conn.ListItems(ctx, datasource.NewScope("jira", []string{"ENG"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			// ReadResource called only for matched resources (not OPS-1)
			Expect(fakeReader.ReadResourceCallCount()).To(Equal(2))
		})

		It("includes all resources when no filter is set", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{URI: "snow://incident/1", Name: "Inc 1"},
					{URI: "snow://change/1", Name: "Change 1"},
				},
			}, nil)

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{URI: req.Params.URI, Text: "Body"},
					},
				}, nil
			}

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "servicenow")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
		})
	})

	Describe("ListItemsSince", func() {
		It("skips resources with lastModified before since threshold", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{
						URI:  "jira://issue/OLD-1",
						Name: "Old Issue",
						Annotated: mcp.Annotated{
							Annotations: &mcp.Annotations{
								LastModified: "2024-01-01T00:00:00Z",
							},
						},
					},
					{
						URI:  "jira://issue/NEW-1",
						Name: "New Issue",
						Annotated: mcp.Annotated{
							Annotations: &mcp.Annotations{
								LastModified: "2025-06-01T00:00:00Z",
							},
						},
					},
					{
						URI:  "jira://issue/UNKNOWN-1",
						Name: "Unknown Modified",
						// No annotations — should be included
					},
				},
			}, nil)

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{URI: req.Params.URI, Text: "Body"},
					},
				}, nil
			}

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira")
			since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			items, err := conn.ListItemsSince(ctx, datasource.Scope{}, since)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			// ReadResource called for NEW-1 and UNKNOWN-1, not OLD-1
			Expect(fakeReader.ReadResourceCallCount()).To(Equal(2))
		})

		It("includes all resources when since is zero", func(ctx context.Context) {
			fakeReader.ListResourcesReturns(&mcp.ListResourcesResult{
				Resources: []mcp.Resource{
					{
						URI:  "jira://issue/1",
						Name: "Issue 1",
						Annotated: mcp.Annotated{
							Annotations: &mcp.Annotations{
								LastModified: "2024-01-01T00:00:00Z",
							},
						},
					},
				},
			}, nil)

			fakeReader.ReadResourceStub = func(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{URI: req.Params.URI, Text: "Body"},
					},
				}, nil
			}

			conn := mcpresource.NewMCPResourceConnector(fakeReader, "jira")
			items, err := conn.ListItemsSince(ctx, datasource.Scope{}, time.Time{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
		})
	})
})
