// Copyright (C) StackGen, Inc. All rights reserved.
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
			conn := gdrive.NewGDriveConnector(fake)
			Expect(conn.Name()).To(Equal("gdrive"))
		})
	})

	Describe("ListItems", func() {
		It("returns nil when scope has no GDrive folder IDs", func(ctx context.Context) {
			conn := gdrive.NewGDriveConnector(fake)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
			Expect(fake.ListFolderCallCount()).To(Equal(0))
		})

		It("returns normalized items for files in scope folders", func(ctx context.Context) {
			fake.ListFolderReturns([]gdrive.FileInfo{
				{ID: "f1", Name: "Doc1", MimeType: "application/vnd.google-apps.document", ModifiedTime: "2025-01-15T10:00:00Z", IsFolder: false},
			}, nil)
			fake.ReadFileReturns("Document body text", nil)

			conn := gdrive.NewGDriveConnector(fake)
			scope := datasource.Scope{GDriveFolderIDs: []string{"folder1"}}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal("gdrive:f1"))
			Expect(items[0].Source).To(Equal("gdrive"))
			Expect(items[0].Content).To(ContainSubstring("Doc1"))
			Expect(items[0].Content).To(ContainSubstring("Document body text"))
			Expect(items[0].Metadata["title"]).To(Equal("Doc1"))

			Expect(fake.ListFolderCallCount()).To(Equal(1))
			_, folderID, _ := fake.ListFolderArgsForCall(0)
			Expect(folderID).To(Equal("folder1"))
		})

		It("skips folders", func(ctx context.Context) {
			fake.ListFolderReturns([]gdrive.FileInfo{
				{ID: "d1", Name: "Subfolder", IsFolder: true},
			}, nil)

			conn := gdrive.NewGDriveConnector(fake)
			scope := datasource.Scope{GDriveFolderIDs: []string{"root"}}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
			Expect(fake.ReadFileCallCount()).To(Equal(0))
		})
	})
})
