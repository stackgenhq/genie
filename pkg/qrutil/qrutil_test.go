package qrutil_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/qrutil"
)

var _ = Describe("qrutil", func() {
	Describe("GeneratePNG", func() {
		It("should create a PNG file in the specified directory", func() {
			dir := GinkgoT().TempDir()
			path, err := qrutil.GeneratePNG("https://example.com", dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal(filepath.Join(dir, "qr-code.png")))

			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Size()).To(BeNumerically(">", 0))
		})

		It("should return an error for an invalid directory", func() {
			_, err := qrutil.GeneratePNG("test", "/nonexistent/path/that/does/not/exist")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ToTerminalString", func() {
		It("should return a non-empty string with Unicode characters", func() {
			result := qrutil.ToTerminalString("https://example.com")
			Expect(result).NotTo(BeEmpty())
			// Unicode QR codes use half-block characters.
			Expect(result).To(ContainSubstring("█"))
		})

		It("should return different output for different content", func() {
			a := qrutil.ToTerminalString("hello")
			b := qrutil.ToTerminalString("world")
			Expect(a).NotTo(Equal(b))
		})
	})

	Describe("PrintToTerminal", func() {
		It("should create a PNG file and return its path", func() {
			dir := GinkgoT().TempDir()
			path, err := qrutil.PrintToTerminal("https://example.com", dir, "Test Header", "Test instruction")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal(filepath.Join(dir, "qr-code.png")))

			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Size()).To(BeNumerically(">", 0))
		})
	})
})
