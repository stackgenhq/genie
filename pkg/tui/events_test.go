package tui

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Events", func() {
	Describe("LogLevel", func() {
		It("should convert to string correctly", func() {
			Expect(LogDebug.String()).To(Equal("DEBUG"))
			Expect(LogInfo.String()).To(Equal("INFO"))
			Expect(LogWarn.String()).To(Equal("WARN"))
			Expect(LogError.String()).To(Equal("ERROR"))
		})

		It("should handle unknown levels", func() {
			unknown := LogLevel(999)
			Expect(unknown.String()).To(Equal("UNKNOWN"))
		})
	})
})
