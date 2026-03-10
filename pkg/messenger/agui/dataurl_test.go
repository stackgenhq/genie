// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui

import (
	"encoding/base64"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ExtractDataURLFiles", func() {
	It("returns the original message when no data URLs are present", func() {
		msg := "Hello, how are you?"
		clean, tempDir, atts := ExtractDataURLFiles(msg)
		Expect(clean).To(Equal(msg))
		Expect(atts).To(BeNil())
		Expect(tempDir).To(BeEmpty())
	})

	It("extracts a single image data URL", func() {
		imgData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
		b64 := base64.StdEncoding.EncodeToString(imgData)
		msg := "[file:photo.png:image/png]\ndata:image/png;base64," + b64 + "\n\nWhat is in this image?"

		clean, tempDir, atts := ExtractDataURLFiles(msg)
		defer os.RemoveAll(tempDir)

		Expect(clean).To(Equal("What is in this image?"))
		Expect(atts).To(HaveLen(1))
		Expect(atts[0].Name).To(Equal("photo.png"))
		Expect(atts[0].ContentType).To(Equal("image/png"))
		Expect(atts[0].Size).To(Equal(int64(4)))
		Expect(atts[0].LocalPath).To(HavePrefix(tempDir))

		// Verify file exists on disk.
		data, err := os.ReadFile(atts[0].LocalPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(imgData))
	})

	It("extracts multiple files", func() {
		img := base64.StdEncoding.EncodeToString([]byte("fakeimage"))
		audio := base64.StdEncoding.EncodeToString([]byte("fakeaudio"))
		msg := "[file:pic.jpg:image/jpeg]\ndata:image/jpeg;base64," + img + "\n\n[file:voice.wav:audio/wav]\ndata:audio/wav;base64," + audio + "\n\nDescribe both"

		clean, tempDir, atts := ExtractDataURLFiles(msg)
		defer os.RemoveAll(tempDir)

		Expect(clean).To(Equal("Describe both"))
		Expect(atts).To(HaveLen(2))
		Expect(atts[0].Name).To(Equal("pic.jpg"))
		Expect(atts[1].Name).To(Equal("voice.wav"))
	})

	It("handles message with no text beyond the file block", func() {
		b64 := base64.StdEncoding.EncodeToString([]byte("content"))
		msg := "[file:doc.pdf:application/pdf]\ndata:application/pdf;base64," + b64

		clean, tempDir, atts := ExtractDataURLFiles(msg)
		defer os.RemoveAll(tempDir)

		Expect(clean).To(BeEmpty())
		Expect(atts).To(HaveLen(1))
		Expect(atts[0].ContentType).To(Equal("application/pdf"))
	})

	It("preserves file extension from filename", func() {
		b64 := base64.StdEncoding.EncodeToString([]byte("video"))
		msg := "[file:clip.mp4:video/mp4]\ndata:video/mp4;base64," + b64

		_, tempDir, atts := ExtractDataURLFiles(msg)
		defer os.RemoveAll(tempDir)

		Expect(atts).To(HaveLen(1))
		Expect(filepath.Ext(atts[0].LocalPath)).To(Equal(".mp4"))
	})

	It("preserves text before and after the file block", func() {
		b64 := base64.StdEncoding.EncodeToString([]byte("img"))
		msg := "Here is the image:\n[file:pic.png:image/png]\ndata:image/png;base64," + b64 + "\nPlease describe it."

		clean, tempDir, atts := ExtractDataURLFiles(msg)
		defer os.RemoveAll(tempDir)

		Expect(clean).To(Equal("Here is the image:\n\nPlease describe it."))
		Expect(atts).To(HaveLen(1))
	})
})

var _ = Describe("decodeDataURL", func() {
	It("decodes a valid data URL", func() {
		imgBytes := []byte{0x89, 0x50, 0x4E, 0x47}
		encoded := base64.StdEncoding.EncodeToString(imgBytes)
		data, mime, err := decodeDataURL("data:image/png;base64," + encoded)
		Expect(err).NotTo(HaveOccurred())
		Expect(mime).To(Equal("image/png"))
		Expect(data).To(Equal(imgBytes))
	})

	It("returns error for non-data URL", func() {
		_, _, err := decodeDataURL("https://example.com/img.png")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not a data URL"))
	})

	It("returns error for malformed data URL without comma", func() {
		_, _, err := decodeDataURL("data:image/png;base64")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no comma separator"))
	})

	It("handles base64 without padding", func() {
		// "Hi" in base64 = "SGk" (no padding in raw) or "SGk=" (with padding)
		data, mime, err := decodeDataURL("data:text/plain;base64,SGk")
		Expect(err).NotTo(HaveOccurred())
		Expect(mime).To(Equal("text/plain"))
		Expect(string(data)).To(Equal("Hi"))
	})

	It("strips whitespace from encoded data", func() {
		imgBytes := []byte("test")
		encoded := base64.StdEncoding.EncodeToString(imgBytes)
		// Insert spaces and newlines (browsers sometimes do this)
		withSpaces := encoded[:2] + " \n " + encoded[2:]
		data, _, err := decodeDataURL("data:text/plain;base64," + withSpaces)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(imgBytes))
	})

	It("handles empty MIME type", func() {
		encoded := base64.StdEncoding.EncodeToString([]byte("x"))
		data, mime, err := decodeDataURL("data:;base64," + encoded)
		Expect(err).NotTo(HaveOccurred())
		Expect(mime).To(BeEmpty())
		Expect(data).To(Equal([]byte("x")))
	})
})
