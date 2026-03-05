package youtubetranscript_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/tools/youtubetranscript"
)

var _ = Describe("Provider Test", func() {
	It("provides the correct tools", func() {
		p := youtubetranscript.NewToolProvider()
		tools := p.GetTools()
		Expect(tools).To(HaveLen(1))
	})
})
