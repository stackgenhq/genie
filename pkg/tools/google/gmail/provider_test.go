package gmail

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Provider", func() {
	var (
		svc *mockService
		p   *ToolProvider
	)

	BeforeEach(func() {
		svc = &mockService{}
		p = NewToolProvider(svc)
	})

	Describe("GetTools", func() {
		It("returns the correct number of tools", func() {
			tools := p.GetTools("test")
			Expect(tools).To(HaveLen(3))
		})
	})
})
