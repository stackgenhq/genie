package browser_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/browser"
)

var _ = Describe("Browser Extended Tests", func() {
	It("should fail safe and block invalid URLs", func(ctx context.Context) {
		b, err := browser.New(
			ctx,
			browser.WithHeadless(true),
			browser.WithBlockedDomains([]string{"example.com"}),
		)
		Expect(err).NotTo(HaveOccurred())
		defer b.Close()

		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		tool := browser.NewNavigateTool(b)
		// "http://%41:8080/" is an invalid URL that often causes parse errors
		_, err = tool.Call(ctx, []byte(`{"url":"http://%41:8080/"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("blocked"))
	})

	It("should close the tab when the parent context is cancelled", func(ctx context.Context) {
		b, err := browser.New(ctx, browser.WithHeadless(true))
		Expect(err).NotTo(HaveOccurred())
		defer b.Close()

		parentCtx, parentCancel := context.WithCancel(ctx)

		tabCtx, _, err := b.NewTab(parentCtx)
		Expect(err).NotTo(HaveOccurred())

		// Verify tab is open (context not done)
		Consistently(tabCtx.Done()).ShouldNot(BeClosed())

		// Cancel parent
		parentCancel()

		// Verify tab context is closed
		Eventually(tabCtx.Done()).Should(BeClosed())
	})
})
