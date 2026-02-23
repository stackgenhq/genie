package osutils

import (
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Install Hints", func() {
	Describe("InstallHint", func() {
		It("returns a non-empty string", func() {
			hint := InstallHint("curl", nil)
			Expect(hint).NotTo(BeEmpty())
		})

		It("uses the package name as default on macOS", func() {
			if runtime.GOOS != "darwin" {
				Skip("macOS only")
			}
			hint := InstallHint("tesseract", nil)
			Expect(hint).To(Equal("brew install tesseract"))
		})

		It("uses map overrides when provided", func() {
			if runtime.GOOS != "darwin" {
				Skip("macOS only")
			}
			hint := InstallHint("tesseract", map[string]string{Brew: "tesseract-lang"})
			Expect(hint).To(Equal("brew install tesseract-lang"))
		})

		It("falls back to packageName when map has no entry", func() {
			if runtime.GOOS != "darwin" {
				Skip("macOS only")
			}
			hint := InstallHint("curl", map[string]string{Apt: "curl-special"})
			// On macOS, Brew key is not in map, so it uses "curl"
			Expect(hint).To(Equal("brew install curl"))
		})

		It("uses apt override on Linux", func() {
			if runtime.GOOS != "linux" {
				Skip("Linux only")
			}
			hint := InstallHint("tesseract", map[string]string{Apt: "tesseract-ocr"})
			Expect(hint).To(ContainSubstring("apt-get install tesseract-ocr"))
		})
	})

	Describe("ToolNotFoundError", func() {
		It("includes the tool name", func() {
			err := ToolNotFoundError("tesseract", map[string]string{Apt: "tesseract-ocr"})
			Expect(err.Error()).To(HavePrefix("tesseract not found"))
		})

		It("includes install instructions", func() {
			err := ToolNotFoundError("psql", map[string]string{Brew: "postgresql", Apt: "postgresql-client"})
			Expect(err.Error()).To(ContainSubstring("install with"))
		})

		It("works with nil map (uses tool name as package)", func() {
			err := ToolNotFoundError("curl", nil)
			Expect(err.Error()).To(ContainSubstring("curl not found"))
			Expect(err.Error()).To(ContainSubstring("install with"))
		})
	})
})
