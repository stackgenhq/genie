package reactree_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/reactree"
	"github.com/appcd-dev/genie/pkg/tools/toolsfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("Critic Middleware", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("DeterministicValidator", func() {
		It("should allow unblocked tools", func() {
			validator := reactree.NewDeterministicValidator([]string{"rm", "drop"})
			err := validator.Validate(ctx, "ls", []byte("{}"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should block blocklist tools", func() {
			validator := reactree.NewDeterministicValidator([]string{"drop_database"})
			err := validator.Validate(ctx, "drop_database", []byte("{}"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("prohibited by deterministic"))
		})
	})

	Context("ValidatingToolWrapper", func() {
		It("should forward calls if validator passes", func() {
			validator := reactree.NewDeterministicValidator([]string{"block_me"})
			ft := &toolsfakes.FakeCallableTool{}
			ft.DeclarationReturns(&tool.Declaration{Name: "safe_tool"})
			ft.CallReturns("success", nil)

			wrapped := reactree.WrapWithValidator(ft, validator)

			// Needs to be asserted to CallableTool to call
			callable, ok := wrapped.(tool.CallableTool)
			Expect(ok).To(BeTrue())

			res, err := callable.Call(ctx, []byte("{}"))
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal("success"))
			Expect(ft.CallCallCount()).To(Equal(1))
		})

		It("should block calls and return error if validator fails", func() {
			validator := reactree.NewDeterministicValidator([]string{"unsafe_tool"})
			ft := &toolsfakes.FakeCallableTool{}
			ft.DeclarationReturns(&tool.Declaration{Name: "unsafe_tool"})
			ft.CallReturns("success", nil)

			wrapped := reactree.WrapWithValidator(ft, validator)

			callable, ok := wrapped.(tool.CallableTool)
			Expect(ok).To(BeTrue())

			_, err := callable.Call(ctx, []byte("{}"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("action pruned by critic"))
			Expect(ft.CallCallCount()).To(Equal(0)) // Tool should not have been called
		})

		It("should return unmodified tool if validator is nil", func() {
			ft := &toolsfakes.FakeCallableTool{}
			ft.DeclarationReturns(&tool.Declaration{Name: "safe_tool"})
			wrapped := reactree.WrapWithValidator(ft, nil)
			Expect(wrapped).To(Equal(ft)) // Exact same pointer
		})
	})
})
