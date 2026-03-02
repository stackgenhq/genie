package modelprovider

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// stubModel is a minimal model.Model implementation used in internal tests to
// avoid importing modelproviderfakes (which would create an import cycle).
type stubModel struct {
	info    model.Info
	resp    *model.Response
	callErr error
	lastReq *model.Request
}

func (s *stubModel) Info() model.Info { return s.info }
func (s *stubModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	s.lastReq = req
	if s.callErr != nil {
		return nil, s.callErr
	}
	ch := make(chan *model.Response, 1)
	if s.resp != nil {
		ch <- s.resp
	}
	close(ch)
	return ch, nil
}

var _ = Describe("ModelProvider Internal", func() {
	Describe("ProviderConfigs.getForTask", func() {
		Context("when providers list is empty", func() {
			It("should return an error", func() {
				var providers ProviderConfigs
				config, usedFallback, err := providers.getForTask(TaskEfficiency)
				Expect(err).To(MatchError("no providers configured"))
				Expect(config.Providers()).To(BeEmpty())
				Expect(usedFallback).To(BeFalse())
			})
		})

		Context("when no matching provider is found", func() {
			It("should return default provider if available (logic check)", func() {
				providers := ProviderConfigs{
					{
						Provider:    "openai",
						GoodForTask: TaskEfficiency,
					},
				}

				config, usedFallback, err := providers.getForTask(TaskToolCalling)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Providers()).To(Equal([]string{"openai"}))
				Expect(usedFallback).To(BeTrue())
			})
		})

		Context("when matching provider is found", func() {
			It("should return the matching provider without fallback", func() {
				providers := ProviderConfigs{
					{
						Provider:    "openai",
						GoodForTask: TaskEfficiency,
					},
					{
						Provider:    "gemini",
						GoodForTask: TaskToolCalling,
					},
				}

				config, usedFallback, err := providers.getForTask(TaskToolCalling)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Providers()).To(Equal([]string{"gemini"}))
				Expect(usedFallback).To(BeFalse())
			})
		})
	})

	Describe("resolveAnthropicMaxOutput", func() {
		It("returns the known limit for claude-sonnet-4-6", func() {
			Expect(resolveAnthropicMaxOutput("claude-sonnet-4-6")).To(Equal(128000))
		})

		It("matches via prefix for date-versioned variants", func() {
			Expect(resolveAnthropicMaxOutput("claude-sonnet-4-6-20250219")).To(Equal(128000))
		})

		It("is case-insensitive", func() {
			Expect(resolveAnthropicMaxOutput("Claude-Sonnet-4-6")).To(Equal(128000))
		})

		It("returns 0 for unknown models (no cap applied)", func() {
			Expect(resolveAnthropicMaxOutput("claude-haiku-4.5")).To(Equal(0))
			Expect(resolveAnthropicMaxOutput("some-future-model")).To(Equal(0))
			Expect(resolveAnthropicMaxOutput("")).To(Equal(0))
		})

		It("returns known limits for other classic models", func() {
			Expect(resolveAnthropicMaxOutput("claude-3-5-sonnet")).To(Equal(8192))
			Expect(resolveAnthropicMaxOutput("claude-3-opus")).To(Equal(4096))
			Expect(resolveAnthropicMaxOutput("claude-3-7-sonnet")).To(Equal(64000))
		})
	})

	Describe("maxOutputCapModel", func() {
		var (
			stub *stubModel
			ctx  context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			stub = &stubModel{resp: &model.Response{}}
		})

		It("sets MaxTokens to the cap when the request has none", func() {
			wrapped := &maxOutputCapModel{inner: stub, maxOutput: 128000}
			req := &model.Request{}
			_, err := wrapped.GenerateContent(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(req.MaxTokens).NotTo(BeNil())
			Expect(*req.MaxTokens).To(Equal(128000))
		})

		// Regression: token tailoring computed ~179487 for claude-sonnet-4-6 (200k window,
		// tiny input), exceeding the 128k API limit and causing a 400 on every request.
		It("clamps MaxTokens when the request exceeds the API limit (regression for 400 errors)", func() {
			wrapped := &maxOutputCapModel{inner: stub, maxOutput: 128000}
			oversized := 179487
			req := &model.Request{GenerationConfig: model.GenerationConfig{MaxTokens: &oversized}}
			_, err := wrapped.GenerateContent(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(*req.MaxTokens).To(Equal(128000))
		})

		It("leaves MaxTokens unchanged when it is within the cap", func() {
			wrapped := &maxOutputCapModel{inner: stub, maxOutput: 128000}
			reasonable := 4096
			req := &model.Request{GenerationConfig: model.GenerationConfig{MaxTokens: &reasonable}}
			_, err := wrapped.GenerateContent(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(*req.MaxTokens).To(Equal(4096))
		})

		It("does not modify request when maxOutput is 0 (no-cap sentinel)", func() {
			wrapped := &maxOutputCapModel{inner: stub, maxOutput: 0}
			req := &model.Request{}
			_, err := wrapped.GenerateContent(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(req.MaxTokens).To(BeNil())
		})

		It("forwards Info from the inner model", func() {
			stub.info = model.Info{Name: "test-model"}
			wrapped := &maxOutputCapModel{inner: stub, maxOutput: 128000}
			Expect(wrapped.Info().Name).To(Equal("test-model"))
		})

		It("forwards GenerateContent errors from the inner model", func() {
			stub.callErr = context.DeadlineExceeded
			wrapped := &maxOutputCapModel{inner: stub, maxOutput: 128000}
			ch, err := wrapped.GenerateContent(ctx, &model.Request{})
			Expect(err).To(MatchError(context.DeadlineExceeded))
			Expect(ch).To(BeNil())
		})
	})
})
