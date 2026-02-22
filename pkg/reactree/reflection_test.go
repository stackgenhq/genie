package reactree_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/reactree"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ActionReflector", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("NoOpReflector", func() {
		It("should always return ShouldProceed=true", func() {
			reflector := &reactree.NoOpReflector{}
			result, err := reflector.Reflect(ctx, reactree.ReflectionRequest{
				Goal:           "test goal",
				ProposedOutput: "some output",
				IterationCount: 1,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.ShouldProceed).To(BeTrue())
			Expect(result.Monologue).To(BeEmpty())
		})
	})
})
