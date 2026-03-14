// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/security"
)

// fakeMCPResourceClient implements mcpResourceClient for tests.
type fakeMCPResourceClient struct {
	listResourcesResults []*mcp.ListResourcesResult // one per call (for pagination)
	listResourcesError   error
	listCallCount        int

	readResourceResults map[string]*mcp.ReadResourceResult // keyed by URI
	readResourceError   error

	closed bool
}

func (f *fakeMCPResourceClient) ListResources(_ context.Context, req mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	if f.listResourcesError != nil {
		return nil, f.listResourcesError
	}
	idx := f.listCallCount
	f.listCallCount++
	if idx >= len(f.listResourcesResults) {
		return nil, nil
	}
	return f.listResourcesResults[idx], nil
}

func (f *fakeMCPResourceClient) ReadResource(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if f.readResourceError != nil {
		return nil, f.readResourceError
	}
	if f.readResourceResults == nil {
		return nil, nil
	}
	return f.readResourceResults[req.Params.URI], nil
}

func (f *fakeMCPResourceClient) Close() error {
	f.closed = true
	return nil
}

var _ = Describe("mcpDatasource", func() {
	var (
		ds   *mcpDatasource
		fake *fakeMCPResourceClient
		ctx  context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		fake = &fakeMCPResourceClient{}
		ds = &mcpDatasource{
			cfg: MCPServerConfig{Name: "test-server"},
			dial: func(_ context.Context, _ MCPServerConfig, _ security.SecretProvider) (mcpResourceClient, error) {
				return fake, nil
			},
		}
	})

	Describe("Name", func() {
		It("should return the server name from config", func() {
			Expect(ds.Name()).To(Equal("test-server"))
		})
	})

	Describe("ListItems", func() {
		Context("when dial fails", func() {
			It("should return an error", func() {
				ds.dial = func(_ context.Context, _ MCPServerConfig, _ security.SecretProvider) (mcpResourceClient, error) {
					return nil, errors.New("connection refused")
				}

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("dial MCP"))
				Expect(items).To(BeNil())
			})
		})

		Context("when ListResources fails", func() {
			It("should return an error", func() {
				fake.listResourcesError = errors.New("list failed")

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("list resources"))
				Expect(items).To(BeNil())
			})
		})

		Context("when no resources exist", func() {
			It("should return nil", func() {
				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{Resources: []mcp.Resource{}},
				}

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(BeNil())
			})
		})

		Context("with text resources", func() {
			It("should convert to NormalizedItems", func() {
				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{Resources: []mcp.Resource{
						{URI: "file:///doc.txt", Name: "My Document", MIMEType: "text/plain", Description: "A test doc"},
					}},
				}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///doc.txt": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "file:///doc.txt", Text: "Hello World"},
						},
					},
				}

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(1))

				item := items[0]
				Expect(item.ID).To(Equal("test-server:file:///doc.txt"))
				Expect(item.Source).To(Equal("test-server"))
				Expect(item.SourceRef.Type).To(Equal("test-server"))
				Expect(item.SourceRef.RefID).To(Equal("file:///doc.txt"))
				Expect(item.Content).To(ContainSubstring("My Document"))
				Expect(item.Content).To(ContainSubstring("Hello World"))
				Expect(item.Metadata["uri"]).To(Equal("file:///doc.txt"))
				Expect(item.Metadata["title"]).To(Equal("My Document"))
				Expect(item.Metadata["mime_type"]).To(Equal("text/plain"))
				Expect(item.Metadata["description"]).To(Equal("A test doc"))
			})
		})

		Context("with binary-only resources", func() {
			It("should skip blob content and return empty", func() {
				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{Resources: []mcp.Resource{
						{URI: "file:///image.png", Name: "Image"},
					}},
				}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///image.png": {
						Contents: []mcp.ResourceContents{
							mcp.BlobResourceContents{URI: "file:///image.png", Blob: "base64data"},
						},
					},
				}

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(BeEmpty())
			})
		})

		Context("when ReadResource fails for individual resources", func() {
			It("should skip failed resources and return others", func() {
				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{Resources: []mcp.Resource{
						{URI: "file:///good.txt", Name: "Good"},
						{URI: "file:///bad.txt", Name: "Bad"},
					}},
				}
				// Only good.txt has a result; bad.txt returns error
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///good.txt": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "file:///good.txt", Text: "OK"},
						},
					},
				}
				// Override ReadResource to error on bad.txt
				origFake := fake
				ds.dial = func(_ context.Context, _ MCPServerConfig, _ security.SecretProvider) (mcpResourceClient, error) {
					return &selectiveErrorClient{
						base:     origFake,
						errorURI: "file:///bad.txt",
					}, nil
				}

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(1))
				Expect(items[0].ID).To(ContainSubstring("good.txt"))
			})
		})

		Context("with empty text content", func() {
			It("should skip resources with empty text", func() {
				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{Resources: []mcp.Resource{
						{URI: "file:///empty.txt", Name: "Empty"},
					}},
				}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///empty.txt": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "file:///empty.txt", Text: ""},
						},
					},
				}

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(BeEmpty())
			})
		})

		Context("when resource name equals text content", func() {
			It("should not duplicate the name as a prefix", func() {
				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{Resources: []mcp.Resource{
						{URI: "file:///exact.txt", Name: "same text"},
					}},
				}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///exact.txt": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "file:///exact.txt", Text: "same text"},
						},
					},
				}

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(1))
				// Content should just be the text, not "same text\n\nsame text"
				Expect(items[0].Content).To(Equal("same text"))
			})
		})

		It("should close the connection after use", func() {
			fake.listResourcesResults = []*mcp.ListResourcesResult{
				{Resources: []mcp.Resource{}},
			}
			_, _ = ds.ListItems(ctx, datasource.Scope{})
			Expect(fake.closed).To(BeTrue())
		})
	})

	Describe("ListItemsSince", func() {
		Context("with time-based filtering", func() {
			It("should skip resources older than the since threshold", func() {
				now := time.Now()
				oldTime := now.Add(-48 * time.Hour).Format(time.RFC3339)
				newTime := now.Add(-1 * time.Hour).Format(time.RFC3339)

				oldAnnot := mcp.Annotated{Annotations: &mcp.Annotations{LastModified: oldTime}}
				newAnnot := mcp.Annotated{Annotations: &mcp.Annotations{LastModified: newTime}}

				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{Resources: []mcp.Resource{
						{URI: "file:///old.txt", Name: "Old", Annotated: oldAnnot},
						{URI: "file:///new.txt", Name: "New", Annotated: newAnnot},
						{URI: "file:///notime.txt", Name: "NoTime"},
					}},
				}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///new.txt": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "file:///new.txt", Text: "recent content"},
						},
					},
					"file:///notime.txt": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "file:///notime.txt", Text: "unknown age"},
						},
					},
				}

				since := now.Add(-24 * time.Hour)
				items, err := ds.ListItemsSince(ctx, datasource.Scope{}, since)
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(2))

				// Should include new + notime, but not old
				ids := make([]string, len(items))
				for i, it := range items {
					ids[i] = it.ID
				}
				Expect(ids).To(ContainElement(ContainSubstring("new.txt")))
				Expect(ids).To(ContainElement(ContainSubstring("notime.txt")))
				Expect(ids).NotTo(ContainElement(ContainSubstring("old.txt")))
			})

			It("should parse lastModified into updatedAt", func() {
				ts := "2026-03-01T12:00:00Z"
				annot := mcp.Annotated{Annotations: &mcp.Annotations{LastModified: ts}}

				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{Resources: []mcp.Resource{
						{URI: "file:///dated.txt", Annotated: annot},
					}},
				}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///dated.txt": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "file:///dated.txt", Text: "content"},
						},
					},
				}

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(1))
				expected, _ := time.Parse(time.RFC3339, ts)
				Expect(items[0].UpdatedAt).To(BeTemporally("~", expected, time.Second))
			})
		})
	})

	Describe("DatasourceKeywordRegex filtering", func() {
		Context("with regex patterns configured", func() {
			It("should include only matching resources", func() {
				ds.cfg.DatasourceKeywordRegex = []string{"INCIDENT-.*", "sprint"}

				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{Resources: []mcp.Resource{
						{URI: "jira://INCIDENT-123", Name: "Server crash"},
						{URI: "jira://FEATURE-456", Name: "sprint planning"},
						{URI: "jira://BUG-789", Name: "Login broken"},
					}},
				}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"jira://INCIDENT-123": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "jira://INCIDENT-123", Text: "crash details"},
						},
					},
					"jira://FEATURE-456": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "jira://FEATURE-456", Text: "sprint plan"},
						},
					},
				}

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(2))

				ids := make([]string, len(items))
				for i, it := range items {
					ids[i] = it.ID
				}
				Expect(ids).To(ContainElement(ContainSubstring("INCIDENT-123")))
				Expect(ids).To(ContainElement(ContainSubstring("FEATURE-456")))
				Expect(ids).NotTo(ContainElement(ContainSubstring("BUG-789")))
			})
		})

		Context("with no regex patterns", func() {
			It("should include all resources", func() {
				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{Resources: []mcp.Resource{
						{URI: "file:///a.txt"},
						{URI: "file:///b.txt"},
					}},
				}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///a.txt": {Contents: []mcp.ResourceContents{mcp.TextResourceContents{URI: "file:///a.txt", Text: "a"}}},
					"file:///b.txt": {Contents: []mcp.ResourceContents{mcp.TextResourceContents{URI: "file:///b.txt", Text: "b"}}},
				}

				items, err := ds.ListItems(ctx, datasource.Scope{})
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(2))
			})
		})
	})

	Describe("compileKeywordPatterns", func() {
		It("should return nil when no patterns are configured", func() {
			ds.cfg.DatasourceKeywordRegex = nil
			patterns := ds.compileKeywordPatterns(ctx)
			Expect(patterns).To(BeNil())
		})

		It("should compile valid patterns", func() {
			ds.cfg.DatasourceKeywordRegex = []string{"^INCIDENT-", "sprint.*board"}
			patterns := ds.compileKeywordPatterns(ctx)
			Expect(patterns).To(HaveLen(2))
		})

		It("should skip invalid patterns and compile valid ones", func() {
			ds.cfg.DatasourceKeywordRegex = []string{"valid-.*", "[invalid", "also-valid"}
			patterns := ds.compileKeywordPatterns(ctx)
			Expect(patterns).To(HaveLen(2))
		})
	})

	Describe("matchesKeywordPatterns", func() {
		It("should match on URI", func() {
			ds.cfg.DatasourceKeywordRegex = []string{"INCIDENT-.*"}
			patterns := ds.compileKeywordPatterns(ctx)
			res := mcp.Resource{URI: "jira://INCIDENT-123", Name: "Some issue"}
			Expect(ds.matchesKeywordPatterns(res, patterns)).To(BeTrue())
		})

		It("should match on Name", func() {
			ds.cfg.DatasourceKeywordRegex = []string{"sprint"}
			patterns := ds.compileKeywordPatterns(ctx)
			res := mcp.Resource{URI: "jira://FEAT-1", Name: "sprint planning"}
			Expect(ds.matchesKeywordPatterns(res, patterns)).To(BeTrue())
		})

		It("should return false when nothing matches", func() {
			ds.cfg.DatasourceKeywordRegex = []string{"INCIDENT-.*"}
			patterns := ds.compileKeywordPatterns(ctx)
			res := mcp.Resource{URI: "jira://BUG-1", Name: "Login bug"}
			Expect(ds.matchesKeywordPatterns(res, patterns)).To(BeFalse())
		})
	})

	Describe("listAllResources", func() {
		Context("with a single page", func() {
			It("should return all resources", func() {
				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{
						Resources: []mcp.Resource{{URI: "a"}, {URI: "b"}},
					},
				}

				resources, err := ds.listAllResources(ctx, fake)
				Expect(err).NotTo(HaveOccurred())
				Expect(resources).To(HaveLen(2))
			})
		})

		Context("with pagination", func() {
			It("should fetch all pages", func() {
				fake.listResourcesResults = []*mcp.ListResourcesResult{
					{
						PaginatedResult: mcp.PaginatedResult{NextCursor: "page2"},
						Resources:       []mcp.Resource{{URI: "a"}},
					},
					{
						Resources: []mcp.Resource{{URI: "b"}},
					},
				}

				resources, err := ds.listAllResources(ctx, fake)
				Expect(err).NotTo(HaveOccurred())
				Expect(resources).To(HaveLen(2))
				Expect(fake.listCallCount).To(Equal(2))
			})
		})

		Context("when ListResources returns nil", func() {
			It("should return empty and stop", func() {
				fake.listResourcesResults = nil

				resources, err := ds.listAllResources(ctx, fake)
				Expect(err).NotTo(HaveOccurred())
				Expect(resources).To(BeEmpty())
			})
		})

		Context("when ListResources returns error", func() {
			It("should propagate the error", func() {
				fake.listResourcesError = errors.New("server error")

				_, err := ds.listAllResources(ctx, fake)
				Expect(err).To(MatchError("server error"))
			})
		})
	})

	Describe("readResource", func() {
		Context("with text content", func() {
			It("should return a NormalizedItem", func() {
				res := mcp.Resource{URI: "file:///test.txt", Name: "Test"}

				result := &mcp.ReadResourceResult{
					Contents: []mcp.ResourceContents{
						mcp.TextResourceContents{URI: "file:///test.txt", Text: "content"},
					},
				}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///test.txt": result,
				}

				items, err := ds.readResource(ctx, fake, res)
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(1))
				Expect(items[0].Content).To(ContainSubstring("content"))
			})
		})

		Context("with nil response", func() {
			It("should return nil", func() {
				res := mcp.Resource{URI: "file:///nil.txt"}
				items, err := ds.readResource(ctx, fake, res)
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(BeNil())
			})
		})

		Context("when ReadResource returns error", func() {
			It("should propagate the error", func() {
				fake.readResourceError = errors.New("read failed")
				res := mcp.Resource{URI: "file:///err.txt"}

				_, err := ds.readResource(ctx, fake, res)
				Expect(err).To(MatchError("read failed"))
			})
		})

		Context("with no Name on resource", func() {
			It("should not include title metadata", func() {
				res := mcp.Resource{URI: "file:///notitle.txt"}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///notitle.txt": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "file:///notitle.txt", Text: "content"},
						},
					},
				}

				items, err := ds.readResource(ctx, fake, res)
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(1))
				Expect(items[0].Metadata).NotTo(HaveKey("title"))
				// When Name is empty, content should just be the text
				Expect(items[0].Content).To(Equal("content"))
			})
		})

		Context("with multiple content items (mixed)", func() {
			It("should only include text content, skip blobs", func() {
				res := mcp.Resource{URI: "file:///mixed.txt"}
				fake.readResourceResults = map[string]*mcp.ReadResourceResult{
					"file:///mixed.txt": {
						Contents: []mcp.ResourceContents{
							mcp.TextResourceContents{URI: "u1", Text: "text1"},
							mcp.BlobResourceContents{URI: "u2", Blob: "binary"},
							mcp.TextResourceContents{URI: "u3", Text: "text2"},
						},
					},
				}

				items, err := ds.readResource(ctx, fake, res)
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(2))
			})
		})
	})

	Describe("compile-time interface compliance", func() {
		It("should implement datasource.DataSource", func() {
			var _ datasource.DataSource = (*mcpDatasource)(nil)
		})

		It("should implement datasource.ListItemsSince", func() {
			var _ datasource.ListItemsSince = (*mcpDatasource)(nil)
		})
	})
})

var _ = Describe("RegisterDatasources", func() {
	It("should register connector factories without error", func() {
		c := &Client{
			config: MCPConfig{
				Servers: []MCPServerConfig{
					{Name: "srv-alpha", Transport: "stdio", Command: "echo"},
					{Name: "srv-beta", Transport: "sse", ServerURL: "http://x"},
				},
			},
		}

		result := c.RegisterDatasources()
		Expect(result).To(BeNil())
	})

	It("should handle empty servers list", func() {
		c := &Client{
			config: MCPConfig{
				Servers: []MCPServerConfig{},
			},
		}

		result := c.RegisterDatasources()
		Expect(result).To(BeNil())
	})
})

// selectiveErrorClient wraps a fakeMCPResourceClient but returns an error for a
// specific URI on ReadResource.
type selectiveErrorClient struct {
	base     *fakeMCPResourceClient
	errorURI string
}

func (s *selectiveErrorClient) ListResources(ctx context.Context, req mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	return s.base.ListResources(ctx, req)
}

func (s *selectiveErrorClient) ReadResource(ctx context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if req.Params.URI == s.errorURI {
		return nil, errors.New("read error for " + s.errorURI)
	}
	return s.base.ReadResource(ctx, req)
}

func (s *selectiveErrorClient) Close() error {
	return s.base.Close()
}
