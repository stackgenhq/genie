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
			Expect(fake.ListFolderCallCount()).To(Equal(0))
		})

		It("returns normalized items with enriched metadata and structured content", func(ctx context.Context) {
			fake.ListFolderReturns([]gdrive.FileInfo{
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

			Expect(fake.ListFolderCallCount()).To(Equal(1))
			_, folderID, _ := fake.ListFolderArgsForCall(0)
			Expect(folderID).To(Equal("folder1"))
		})

		It("handles files without optional fields gracefully", func(ctx context.Context) {
			fake.ListFolderReturns([]gdrive.FileInfo{
				{ID: "f2", Name: "binary.zip", MimeType: "application/zip", IsFolder: false},
			}, nil)

			conn := gdrive.NewGDriveConnector(fake, 0)
			items, err := conn.ListItems(ctx, datasource.Scope{GDriveFolderIDs: []string{"root"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))

			item := items[0]
			// Content header should still be present even for non-text files.
			Expect(item.Content).To(ContainSubstring("[Google Drive File]"))
			Expect(item.Content).To(ContainSubstring("Title: binary.zip"))
			Expect(item.Content).To(ContainSubstring("Type: file"))

			// Optional fields should be absent from metadata.
			Expect(item.Metadata).NotTo(HaveKey("modified_date"))
			Expect(item.Metadata).NotTo(HaveKey("size"))
			Expect(item.Metadata).NotTo(HaveKey("url"))
			Expect(item.Metadata["file_type"]).To(Equal("file"))

			// ReadFile should not be called for non-text MIME types.
			Expect(fake.ReadFileCallCount()).To(Equal(0))
		})

		It("maps Google Workspace MIME types to friendly file types", func(ctx context.Context) {
			fake.ListFolderReturns([]gdrive.FileInfo{
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

		It("recursively lists items in subfolders", func(ctx context.Context) {
			// First call returns a subfolder, second call returns a file inside it.
			fake.ListFolderReturnsOnCall(0, []gdrive.FileInfo{
				{ID: "d1", Name: "Subfolder", IsFolder: true},
			}, nil)
			fake.ListFolderReturnsOnCall(1, []gdrive.FileInfo{
				{ID: "f2", Name: "Nested.txt", MimeType: "text/plain", ModifiedTime: "2025-01-20T08:00:00Z", IsFolder: false},
			}, nil)
			fake.ReadFileReturns("Hello from subfolder", nil)

			conn := gdrive.NewGDriveConnector(fake, 0)
			scope := datasource.Scope{GDriveFolderIDs: []string{"root"}}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal("gdrive:f2"))
			Expect(items[0].Content).To(ContainSubstring("Hello from subfolder"))
			Expect(items[0].Content).To(ContainSubstring("[Google Drive File]"))

			// ListFolder called twice: once for root, once for subfolder d1.
			Expect(fake.ListFolderCallCount()).To(Equal(2))
			_, subFolderID, _ := fake.ListFolderArgsForCall(1)
			Expect(subFolderID).To(Equal("d1"))
		})
	})
})
