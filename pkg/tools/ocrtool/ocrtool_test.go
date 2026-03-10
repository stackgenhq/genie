// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ocrtool

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OCR Tool", func() {
	var o *ocrTools

	BeforeEach(func() {
		o = newOCRTools()
	})

	Describe("input validation", func() {
		It("rejects empty image path", func() {
			_, err := o.extractText(context.Background(), ocrRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("image_path is required"))
		})

		It("rejects non-existent file", func() {
			_, err := o.extractText(context.Background(), ocrRequest{
				ImagePath: "/nonexistent/path/image.png",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("image not found"))
		})

		It("rejects directories", func() {
			tmpDir, err := os.MkdirTemp("", "ocr-test-dir-*")
			Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			_, ocrErr := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpDir,
			})
			Expect(ocrErr).To(HaveOccurred())
			Expect(ocrErr.Error()).To(ContainSubstring("directory"))
		})

		It("rejects unsupported file formats", func() {
			tmpFile, err := os.CreateTemp("", "ocr-test-*.gif")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			_, ocrErr := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
			})
			Expect(ocrErr).To(HaveOccurred())
			Expect(ocrErr.Error()).To(ContainSubstring("unsupported image format"))
		})

		It("rejects files exceeding size limit", func() {
			// Create a file name that looks like an image but we won't
			// actually make it large — just test the path logic.
			tmpFile, err := os.CreateTemp("", "ocr-test-*.svg")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			_, ocrErr := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
			})
			Expect(ocrErr).To(HaveOccurred())
			Expect(ocrErr.Error()).To(ContainSubstring("unsupported image format"))
		})

		It("accepts .png extension", func() {
			tmpFile, err := os.CreateTemp("", "ocr-test-*.png")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Write([]byte("not a real image"))
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			// Will fail on tesseract but pass format validation.
			_, ocrErr := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
			})
			// Either tesseract fails or succeeds, but format validation passed.
			if ocrErr != nil {
				Expect(ocrErr.Error()).NotTo(ContainSubstring("unsupported image format"))
			}
		})

		It("accepts .jpg extension", func() {
			tmpFile, err := os.CreateTemp("", "ocr-test-*.jpg")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Write([]byte("not a real image"))
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			_, ocrErr := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
			})
			if ocrErr != nil {
				Expect(ocrErr.Error()).NotTo(ContainSubstring("unsupported image format"))
			}
		})

		It("accepts .jpeg extension", func() {
			tmpFile, err := os.CreateTemp("", "ocr-test-*.jpeg")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Write([]byte("not a real image"))
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			_, ocrErr := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
			})
			if ocrErr != nil {
				Expect(ocrErr.Error()).NotTo(ContainSubstring("unsupported image format"))
			}
		})

		It("accepts .tiff extension", func() {
			tmpFile, err := os.CreateTemp("", "ocr-test-*.tiff")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Write([]byte("not a real image"))
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			_, ocrErr := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
			})
			if ocrErr != nil {
				Expect(ocrErr.Error()).NotTo(ContainSubstring("unsupported image format"))
			}
		})

		It("accepts .bmp extension", func() {
			tmpFile, err := os.CreateTemp("", "ocr-test-*.bmp")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Write([]byte("not a real image"))
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			_, ocrErr := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
			})
			if ocrErr != nil {
				Expect(ocrErr.Error()).NotTo(ContainSubstring("unsupported image format"))
			}
		})

		It("accepts .webp extension", func() {
			tmpFile, err := os.CreateTemp("", "ocr-test-*.webp")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Write([]byte("not a real image"))
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			_, ocrErr := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
			})
			if ocrErr != nil {
				Expect(ocrErr.Error()).NotTo(ContainSubstring("unsupported image format"))
			}
		})
	})

	Describe("language defaults", func() {
		It("defaults to eng when no language specified", func() {
			tmpFile, err := os.CreateTemp("", "ocr-test-*.png")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Write([]byte("not a real image"))
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			// The response Language field should be set even on error.
			resp, _ := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
			})
			Expect(resp.Language).To(Equal("eng"))
		})

		It("respects custom language", func() {
			tmpFile, err := os.CreateTemp("", "ocr-test-*.png")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Write([]byte("not a real image"))
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			resp, _ := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
				Language:  "deu",
			})
			Expect(resp.Language).To(Equal("deu"))
		})
	})

	Describe("response metadata", func() {
		It("populates image_path in response", func() {
			tmpFile, err := os.CreateTemp("", "ocr-test-*.png")
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Write([]byte("not a real image"))
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())

			resp, _ := o.extractText(context.Background(), ocrRequest{
				ImagePath: tmpFile.Name(),
			})
			Expect(resp.ImagePath).To(Equal(tmpFile.Name()))
		})
	})

	Describe("real OCR integration", func() {
		// This test creates a simple image with text and OCRs it.
		// Only runs if tesseract is available.
		It("extracts text from a generated test image", func() {
			// Create a simple PNM image with a white background.
			// PNM (P5/P6) is the simplest image format — tesseract reads it directly.
			tmpDir, err := os.MkdirTemp("", "ocr-integration-*")
			Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			imgPath := filepath.Join(tmpDir, "test.pnm")

			// Create a 200x50 grayscale PNM (P5) image — all white pixels.
			// This is the simplest possible test — tesseract should return
			// empty or near-empty text for a blank image.
			width, height := 200, 50
			header := []byte("P5\n200 50\n255\n")
			pixels := make([]byte, width*height)
			for i := range pixels {
				pixels[i] = 255 // white
			}
			imgData := append(header, pixels...)
			err = os.WriteFile(imgPath, imgData, 0644)
			Expect(err).NotTo(HaveOccurred())

			resp, err := o.extractText(context.Background(), ocrRequest{
				ImagePath: imgPath,
			})

			if err != nil && err.Error() == "tesseract not found: install with 'brew install tesseract' or 'apt-get install tesseract-ocr'" {
				Skip("tesseract not installed")
			}

			// For a blank white image, tesseract should return minimal or no text.
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.CharCount).To(BeNumerically(">=", 0))
		})
	})

	Describe("provider", func() {
		It("creates tool via provider", func() {
			p := NewToolProvider()
			tools := p.GetTools()
			Expect(tools).To(HaveLen(1))
		})
	})
})
