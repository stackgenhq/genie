// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package media_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/messenger/media"
)

var _ = Describe("DescribeAttachments", func() {
	It("returns empty string for nil attachments", func() {
		Expect(media.DescribeAttachments(nil)).To(BeEmpty())
	})

	It("returns empty string for empty slice", func() {
		Expect(media.DescribeAttachments([]messenger.Attachment{})).To(BeEmpty())
	})

	It("describes a single attachment with all fields", func() {
		result := media.DescribeAttachments([]messenger.Attachment{{
			Name:        "report.pdf",
			ContentType: "application/pdf",
			Size:        1258291,
		}})
		Expect(result).To(Equal("[Attached: report.pdf (application/pdf, 1.2 MB)]"))
	})

	It("describes a single attachment with name only", func() {
		result := media.DescribeAttachments([]messenger.Attachment{{
			Name: "photo.jpg",
		}})
		Expect(result).To(Equal("[Attached: photo.jpg]"))
	})

	It("uses 'unnamed file' when name is empty", func() {
		result := media.DescribeAttachments([]messenger.Attachment{{
			ContentType: "image/png",
			Size:        500,
		}})
		Expect(result).To(Equal("[Attached: unnamed file (image/png, 500 B)]"))
	})

	It("joins multiple attachments with semicolons", func() {
		result := media.DescribeAttachments([]messenger.Attachment{
			{Name: "a.pdf", ContentType: "application/pdf"},
			{Name: "b.jpg", ContentType: "image/jpeg"},
		})
		Expect(result).To(Equal("[Attached: a.pdf (application/pdf); b.jpg (image/jpeg)]"))
	})

	It("includes LocalPath when available", func() {
		result := media.DescribeAttachments([]messenger.Attachment{{
			Name:        "report.pdf",
			ContentType: "application/pdf",
			Size:        1048576,
			LocalPath:   "/tmp/media/report_123.pdf",
		}})
		Expect(result).To(Equal("[Attached: report.pdf (application/pdf, 1.0 MB) → /tmp/media/report_123.pdf]"))
	})

	It("omits LocalPath when empty", func() {
		result := media.DescribeAttachments([]messenger.Attachment{{
			Name:        "photo.jpg",
			ContentType: "image/jpeg",
		}})
		Expect(result).NotTo(ContainSubstring("→"))
	})

	It("relativizes absolute LocalPath when baseDir is provided", func() {
		result := media.DescribeAttachments([]messenger.Attachment{{
			Name:        "report.pdf",
			ContentType: "application/pdf",
			Size:        1048576,
			LocalPath:   "/home/user/project/.genie/whatsapp/media/report_123.pdf",
		}}, "/home/user/project")
		Expect(result).To(Equal("[Attached: report.pdf (application/pdf, 1.0 MB) → .genie/whatsapp/media/report_123.pdf]"))
	})

	It("keeps absolute LocalPath when outside baseDir", func() {
		result := media.DescribeAttachments([]messenger.Attachment{{
			Name:      "secret.txt",
			LocalPath: "/etc/secrets/secret.txt",
		}}, "/home/user/project")
		Expect(result).To(Equal("[Attached: secret.txt → /etc/secrets/secret.txt]"))
	})

	It("keeps absolute LocalPath when no baseDir provided", func() {
		result := media.DescribeAttachments([]messenger.Attachment{{
			Name:        "report.pdf",
			ContentType: "application/pdf",
			Size:        1048576,
			LocalPath:   "/tmp/media/report_123.pdf",
		}})
		Expect(result).To(Equal("[Attached: report.pdf (application/pdf, 1.0 MB) → /tmp/media/report_123.pdf]"))
	})
})

var _ = Describe("FormatFileSize", func() {
	DescribeTable("formats sizes correctly",
		func(bytes int64, expected string) {
			Expect(media.FormatFileSize(bytes)).To(Equal(expected))
		},
		Entry("zero", int64(0), "0 B"),
		Entry("bytes", int64(500), "500 B"),
		Entry("1 KB", int64(1024), "1.0 KB"),
		Entry("1.5 KB", int64(1536), "1.5 KB"),
		Entry("1 MB", int64(1048576), "1.0 MB"),
		Entry("1.5 MB", int64(1572864), "1.5 MB"),
		Entry("1 GB", int64(1073741824), "1.0 GB"),
	)
})

var _ = Describe("MIMEFromFilename", func() {
	DescribeTable("detects MIME types",
		func(filename, expected string) {
			Expect(media.MIMEFromFilename(filename)).To(Equal(expected))
		},
		Entry("PDF", "report.pdf", "application/pdf"),
		Entry("PNG uppercase", "image.PNG", "image/png"),
		Entry("JSON", "data.json", "application/json"),
		Entry("unknown extension", "unknown.xyz", "application/octet-stream"),
		Entry("no extension", "noext", "application/octet-stream"),
	)
})

var _ = Describe("NameFromMIME", func() {
	It("generates filename with prefix and extension", func() {
		result := media.NameFromMIME("image/jpeg", "image")
		Expect(result).To(SatisfyAny(Equal("image.jpg"), Equal("image.jpeg")))
	})

	It("uses 'file' as default prefix", func() {
		Expect(media.NameFromMIME("application/pdf", "")).To(Equal("file.pdf"))
	})

	It("returns .bin for unknown MIME type", func() {
		Expect(media.NameFromMIME("application/unknown", "doc")).To(Equal("doc.bin"))
	})
})

var _ = Describe("ExtFromMIME", func() {
	DescribeTable("returns correct extensions",
		func(mime, expected string) {
			Expect(media.ExtFromMIME(mime)).To(Equal(expected))
		},
		Entry("PDF", "application/pdf", ".pdf"),
		Entry("PNG", "image/png", ".png"),
		Entry("MP4", "video/mp4", ".mp4"),
		Entry("unknown", "application/xyz", ".bin"),
	)
})
