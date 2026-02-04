package codeowner

import (
	"context"
	"errors"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/expertfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = Describe("CodeOwner", func() {
	var (
		fakeExpert *expertfakes.FakeExpert
		co         *codeOwner
		ctx        context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeExpert = &expertfakes.FakeExpert{}
		co = &codeOwner{
			expert: fakeExpert,
		}
	})

	Describe("Chat", func() {
		It("should return no error when expert succeeds", func() {
			fakeExpert.DoReturns(expert.Response{
				Choices: []model.Choice{
					{Message: model.Message{Content: "Hello "}},
					{Message: model.Message{Content: "World"}},
				},
			}, nil)

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "Hi",
			}, outputChan)

			Expect(err).NotTo(HaveOccurred())

			// Verify Expert was called correctly
			_, req := fakeExpert.DoArgsForCall(0)
			Expect(req.Message).To(Equal("Hi"))
		})

		It("should return error if expert fails", func() {
			fakeExpert.DoReturns(expert.Response{}, errors.New("expert failed"))

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "Hi",
			}, outputChan)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expert failed"))
		})
	})
})
