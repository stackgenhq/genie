package email_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/email"
)

var _ = Describe("Provider Test", func() {
	var (
		svc *mockService
		p   *email.ToolProvider
	)

	BeforeEach(func() {
		svc = &mockService{}
		p = email.NewToolProvider(svc)
	})

	Describe("GetTools", func() {
		It("returns the correct number of tools", func() {
			tools := p.GetTools()
			Expect(tools).To(HaveLen(2))
		})
	})
})
