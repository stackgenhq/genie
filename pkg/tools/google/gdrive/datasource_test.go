// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package gdrive_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/tools/google/gdrive"
	"github.com/stackgenhq/genie/pkg/tools/google/gdrive/gdrivefakes"
)

var _ = Describe("GDriveConnector", func() {
	var fake *gdrivefakes.FakeService

	BeforeEach(func() {
		fake = new(gdrivefakes.FakeService)
	})

	Describe("Name", func() {
		It("returns gdrive", func() {
			conn := gdrive.NewGDriveConnector(fake, 0)
			Expect(conn.Name()).To(Equal("gdrive"))
		})
	})

	Describe("ListItems", func() {
		It("returns nil when scope has no GDrive folder IDs", func(ctx context.Context) {
			conn := gdrive.NewGDriveConnector(fake, 0)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
			Expect(fake.ListFolderModifiedSinceCallCount()).To(Equal(0))
		})

		It("returns normalized items with enriched metadata and structured content", func(ctx context.Context) {
			fake.ListFolderModifiedSinceReturns([]gdrive.FileInfo{
				{
					ID:           "f1",
					Name:         "Q1 Marketing Brief",
					MimeType:     "application/vnd.google-apps.document",
					ModifiedTime: "2026-03-11T15:33:01Z",
					WebViewLink:  "https://docs.google.com/document/d/f1/edit",
					Size:         4096,
					IsFolder:     false,
				},
			}, nil)
			fake.ReadFileReturns("Campaign objectives and target audience analysis.", nil)

			conn := gdrive.NewGDriveConnector(fake, 0)
			scope := datasource.Scope{GDriveFolderIDs: []string{"folder1"}}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))

			item := items[0]
			Expect(item.ID).To(Equal("gdrive:f1"))
			Expect(item.Source).To(Equal("gdrive"))

			// Content should have structured header followed by body.
			Expect(item.Content).To(ContainSubstring("[Google Drive File]"))
			Expect(item.Content).To(ContainSubstring("Title: Q1 Marketing Brief"))
			Expect(item.Content).To(ContainSubstring("Type: document"))
			Expect(item.Content).To(ContainSubstring("Modified: 2026-03-11"))
			Expect(item.Content).To(ContainSubstring("URL: https://docs.google.com/document/d/f1/edit"))
			Expect(item.Content).To(ContainSubstring("Campaign objectives and target audience analysis."))

			// Metadata should include enriched fields.
			Expect(item.Metadata["title"]).To(Equal("Q1 Marketing Brief"))
			Expect(item.Metadata["mime_type"]).To(Equal("application/vnd.google-apps.document"))
			Expect(item.Metadata["file_type"]).To(Equal("document"))
			Expect(item.Metadata["url"]).To(Equal("https://docs.google.com/document/d/f1/edit"))
			Expect(item.Metadata["modified_date"]).To(Equal("2026-03-11T15:33:01Z"))
			Expect(item.Metadata["size"]).To(Equal("4096"))
		})

		It("handles files without optional fields gracefully", func(ctx context.Context) {
			fake.ListFolderModifiedSinceReturns([]gdrive.FileInfo{
				{ID: "f2", Name: "binary.zip", MimeType: "application/zip", IsFolder: false},
			}, nil)

			conn := gdrive.NewGDriveConnector(fake, 0)
			items, err := conn.ListItems(ctx, datasource.Scope{GDriveFolderIDs: []string{"root"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))

			item := items[0]
			Expect(item.Content).To(ContainSubstring("[Google Drive File]"))
			Expect(item.Content).To(ContainSubstring("Title: binary.zip"))
			Expect(item.Content).To(ContainSubstring("Type: file"))
			Expect(item.Metadata).NotTo(HaveKey("modified_date"))
			Expect(item.Metadata).NotTo(HaveKey("size"))
			Expect(item.Metadata).NotTo(HaveKey("url"))
			Expect(item.Metadata).NotTo(HaveKey("folder_path"))
			Expect(item.Metadata["file_type"]).To(Equal("file"))
			Expect(fake.ReadFileCallCount()).To(Equal(0))
		})

		It("maps Google Workspace MIME types to friendly file types", func(ctx context.Context) {
			fake.ListFolderModifiedSinceReturns([]gdrive.FileInfo{
				{ID: "s1", Name: "Budget", MimeType: "application/vnd.google-apps.spreadsheet", IsFolder: false},
				{ID: "p1", Name: "Deck", MimeType: "application/vnd.google-apps.presentation", IsFolder: false},
				{ID: "pdf1", Name: "Report.pdf", MimeType: "application/pdf", IsFolder: false},
				{ID: "t1", Name: "notes.txt", MimeType: "text/plain", IsFolder: false},
			}, nil)
			fake.ReadFileReturns("content", nil)

			conn := gdrive.NewGDriveConnector(fake, 0)
			items, err := conn.ListItems(ctx, datasource.Scope{GDriveFolderIDs: []string{"root"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(4))
			Expect(items[0].Metadata["file_type"]).To(Equal("spreadsheet"))
			Expect(items[1].Metadata["file_type"]).To(Equal("presentation"))
			Expect(items[2].Metadata["file_type"]).To(Equal("pdf"))
			Expect(items[3].Metadata["file_type"]).To(Equal("text"))
		})
	})

	Describe("Folder path tracking", func() {
		It("tracks folder path through recursive traversal", func(ctx context.Context) {
			// Root folder has subfolder "Marketing"
			fake.ListFolderModifiedSinceReturnsOnCall(0, []gdrive.FileInfo{
				{ID: "d1", Name: "Marketing", IsFolder: true},
			}, nil)
			// "Marketing" has subfolder "Campaigns"
			fake.ListFolderModifiedSinceReturnsOnCall(1, []gdrive.FileInfo{
				{ID: "d2", Name: "Campaigns", IsFolder: true},
			}, nil)
			// "Campaigns" has a file
			fake.ListFolderModifiedSinceReturnsOnCall(2, []gdrive.FileInfo{
				{ID: "f1", Name: "Q1 Brief.txt", MimeType: "text/plain", IsFolder: false},
			}, nil)
			fake.ReadFileReturns("Campaign details", nil)

			conn := gdrive.NewGDriveConnector(fake, 0)
			items, err := conn.ListItems(ctx, datasource.Scope{GDriveFolderIDs: []string{"root"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))

			item := items[0]
			// Folder path should be "Marketing/Campaigns" (root is excluded).
			Expect(item.Metadata["folder_path"]).To(Equal("Marketing/Campaigns"))
			Expect(item.Content).To(ContainSubstring("Folder: Marketing/Campaigns"))
		})

		It("omits folder_path for files in root-level scope folders", func(ctx context.Context) {
			fake.ListFolderModifiedSinceReturns([]gdrive.FileInfo{
				{ID: "f1", Name: "TopLevel.txt", MimeType: "text/plain", IsFolder: false},
			}, nil)
			fake.ReadFileReturns("content", nil)

			conn := gdrive.NewGDriveConnector(fake, 0)
			items, err := conn.ListItems(ctx, datasource.Scope{GDriveFolderIDs: []string{"root"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Metadata).NotTo(HaveKey("folder_path"))
			Expect(items[0].Content).NotTo(ContainSubstring("Folder:"))
		})

		It("respects maxDepth limit", func(ctx context.Context) {
			// Create a chain: root → d1 → d2 (but maxDepth=2 should stop at d2)
			fake.ListFolderModifiedSinceReturnsOnCall(0, []gdrive.FileInfo{
				{ID: "d1", Name: "Level1", IsFolder: true},
			}, nil)
			fake.ListFolderModifiedSinceReturnsOnCall(1, []gdrive.FileInfo{
				{ID: "d2", Name: "Level2", IsFolder: true},
			}, nil)
			// d2 would have files, but maxDepth=2 should prevent listing its contents.

			conn := gdrive.NewGDriveConnector(fake, 2)
			items, err := conn.ListItems(ctx, datasource.Scope{GDriveFolderIDs: []string{"root"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
			// ListFolder called for root (depth=0) and d1 (depth=1), but NOT d2 (depth=2).
			Expect(fake.ListFolderModifiedSinceCallCount()).To(Equal(2))
		})
	})

	Describe("Content header structure", func() {
		It("includes all available metadata in the header", func(ctx context.Context) {
			fake.ListFolderModifiedSinceReturnsOnCall(0, []gdrive.FileInfo{
				{ID: "d1", Name: "Reports", IsFolder: true},
			}, nil)
			fake.ListFolderModifiedSinceReturnsOnCall(1, []gdrive.FileInfo{
				{
					ID:           "f1",
					Name:         "Annual Review",
					MimeType:     "application/vnd.google-apps.document",
					ModifiedTime: "2026-01-15T10:00:00Z",
					WebViewLink:  "https://docs.google.com/document/d/f1",
					IsFolder:     false,
				},
			}, nil)
			fake.ReadFileReturns("The annual review covers...", nil)

			conn := gdrive.NewGDriveConnector(fake, 0)
			items, err := conn.ListItems(ctx, datasource.Scope{GDriveFolderIDs: []string{"root"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))

			content := items[0].Content
			// Header fields in expected order.
			Expect(content).To(HavePrefix("[Google Drive File]\n"))
			Expect(content).To(ContainSubstring("Title: Annual Review\n"))
			Expect(content).To(ContainSubstring("Type: document\n"))
			Expect(content).To(ContainSubstring("Folder: Reports\n"))
			Expect(content).To(ContainSubstring("Modified: 2026-01-15\n"))
			Expect(content).To(ContainSubstring("URL: https://docs.google.com/document/d/f1\n"))
			// Body follows after double newline.
			Expect(content).To(ContainSubstring("\n\nThe annual review covers..."))
		})
	})
})
