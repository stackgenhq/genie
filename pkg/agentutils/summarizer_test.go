package agentutils_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/agentutils"
	"github.com/appcd-dev/genie/pkg/audit/auditfakes"
	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/expertfakes"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider/modelproviderfakes"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// stubExpert is a test helper that implements expert.Expert for controlled tests.
type stubExpert struct {
	response expert.Response
	err      error
	lastReq  expert.Request
}

func (s *stubExpert) Do(_ context.Context, req expert.Request) (expert.Response, error) {
	s.lastReq = req
	return s.response, s.err
}

var _ = Describe("Summarizer", func() {
	var (
		fakeAuditor *auditfakes.FakeAuditor
	)

	BeforeEach(func() {
		fakeAuditor = &auditfakes.FakeAuditor{}
	})

	Describe("NewSummarizer", func() {
		It("should return a non-nil Summarizer on success", func(ctx context.Context) {
			fakeModelProvider := &modelproviderfakes.FakeModelProvider{}
			fakeModel := &modelproviderfakes.FakeModel{}
			fakeModel.InfoReturns(model.Info{Name: "test-model"})
			fakeModelProvider.GetModelReturns(map[string]model.Model{"test-model": fakeModel}, nil)

			s, err := agentutils.NewSummarizer(ctx, fakeModelProvider, fakeAuditor)
			Expect(err).NotTo(HaveOccurred())
			Expect(s).NotTo(BeNil())
		})
	})

	Describe("Summarize", func() {
		Context("input validation", func() {
			It("should return an error when content is empty", func(ctx context.Context) {
				stub := &stubExpert{}
				s := agentutils.NewSummarizerWithExpert(stub)

				_, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "",
					RequiredOutputFormat: agentutils.OutputFormatText,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("content must not be empty"))
			})

			It("should return an error when content is whitespace only", func(ctx context.Context) {
				stub := &stubExpert{}
				s := agentutils.NewSummarizerWithExpert(stub)

				_, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "   \n\t  ",
					RequiredOutputFormat: agentutils.OutputFormatJSON,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("content must not be empty"))
			})

			It("should return an error when output format is invalid", func(ctx context.Context) {
				stub := &stubExpert{}
				s := agentutils.NewSummarizerWithExpert(stub)

				_, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "some content",
					RequiredOutputFormat: agentutils.OutputFormat("INVALID"),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unsupported output format"))
			})
		})

		Context("when the expert returns valid choices", func() {
			It("should return the concatenated text output for JSON format", func(ctx context.Context) {
				stub := &stubExpert{
					response: expert.Response{
						Choices: []model.Choice{
							{Message: model.Message{Content: `{"summary": "test output"}`}},
						},
					},
				}
				s := agentutils.NewSummarizerWithExpert(stub)

				result, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "The API returned a list of users with pagination metadata.",
					RequiredOutputFormat: agentutils.OutputFormatJSON,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(`{"summary": "test output"}`))
			})

			It("should return plain text output for TEXT format", func(ctx context.Context) {
				stub := &stubExpert{
					response: expert.Response{
						Choices: []model.Choice{
							{Message: model.Message{Content: "This is a summary of the content."}},
						},
					},
				}
				s := agentutils.NewSummarizerWithExpert(stub)

				result, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "Long content that needs summarization.",
					RequiredOutputFormat: agentutils.OutputFormatText,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("This is a summary of the content."))
			})

			It("should return YAML output for YAML format", func(ctx context.Context) {
				yamlOutput := "summary: test\nkeys:\n  - a\n  - b"
				stub := &stubExpert{
					response: expert.Response{
						Choices: []model.Choice{
							{Message: model.Message{Content: yamlOutput}},
						},
					},
				}
				s := agentutils.NewSummarizerWithExpert(stub)

				result, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "Some structured data",
					RequiredOutputFormat: agentutils.OutputFormatYAML,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(yamlOutput))
			})
		})

		Context("when multiple choices are returned", func() {
			It("should return last choice content (streaming accumulation)", func(ctx context.Context) {
				stub := &stubExpert{
					response: expert.Response{
						Choices: []model.Choice{
							{Message: model.Message{Content: "part1"}},
							{Message: model.Message{Content: "part1part2"}},
						},
					},
				}
				s := agentutils.NewSummarizerWithExpert(stub)

				result, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "some content",
					RequiredOutputFormat: agentutils.OutputFormatText,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("part1part2"))
			})
		})

		Context("when the expert returns an error", func() {
			It("should wrap and return the error", func(ctx context.Context) {
				stub := &stubExpert{
					err: fmt.Errorf("model unavailable"),
				}
				s := agentutils.NewSummarizerWithExpert(stub)

				_, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "some content",
					RequiredOutputFormat: agentutils.OutputFormatText,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("summarization LLM call failed"))
				Expect(err.Error()).To(ContainSubstring("model unavailable"))
			})
		})

		Context("when the expert returns empty choices", func() {
			It("should return an error about empty output", func(ctx context.Context) {
				stub := &stubExpert{
					response: expert.Response{
						Choices: []model.Choice{},
					},
				}
				s := agentutils.NewSummarizerWithExpert(stub)

				_, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "some content",
					RequiredOutputFormat: agentutils.OutputFormatYAML,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("empty output"))
			})
		})

		Context("when the expert returns choices with empty content", func() {
			It("should return an error about empty output", func(ctx context.Context) {
				stub := &stubExpert{
					response: expert.Response{
						Choices: []model.Choice{
							{Message: model.Message{Content: ""}},
						},
					},
				}
				s := agentutils.NewSummarizerWithExpert(stub)

				_, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "some content",
					RequiredOutputFormat: agentutils.OutputFormatText,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("empty output"))
			})
		})

		Context("prompt construction", func() {
			It("should include the output format in the message sent to the expert", func(ctx context.Context) {
				stub := &stubExpert{
					response: expert.Response{
						Choices: []model.Choice{
							{Message: model.Message{Content: "summary output"}},
						},
					},
				}
				s := agentutils.NewSummarizerWithExpert(stub)

				_, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "test content",
					RequiredOutputFormat: agentutils.OutputFormatJSON,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(stub.lastReq.Message).To(ContainSubstring("JSON"))
				Expect(stub.lastReq.Message).To(ContainSubstring("test content"))
			})

			It("should include the content delimiters in the message", func(ctx context.Context) {
				stub := &stubExpert{
					response: expert.Response{
						Choices: []model.Choice{
							{Message: model.Message{Content: "summary"}},
						},
					},
				}
				s := agentutils.NewSummarizerWithExpert(stub)

				_, err := s.Summarize(ctx, agentutils.SummarizeRequest{
					Content:              "my important content",
					RequiredOutputFormat: agentutils.OutputFormatText,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(stub.lastReq.Message).To(ContainSubstring("--- BEGIN CONTENT ---"))
				Expect(stub.lastReq.Message).To(ContainSubstring("--- END CONTENT ---"))
				Expect(stub.lastReq.Message).To(ContainSubstring("my important content"))
			})
		})
	})

	Describe("OutputFormat constants", func() {
		It("should have correct string values", func() {
			Expect(string(agentutils.OutputFormatJSON)).To(Equal("JSON"))
			Expect(string(agentutils.OutputFormatText)).To(Equal("TEXT"))
			Expect(string(agentutils.OutputFormatYAML)).To(Equal("YAML"))
		})
	})

	Describe("NewSummarizerTool", func() {
		It("should create a tool with the correct name", func() {
			fakeExpert := &expertfakes.FakeExpert{}
			s := agentutils.NewSummarizerWithExpert(fakeExpert)
			t := agentutils.NewSummarizerTool(s)

			Expect(t.Declaration().Name).To(Equal("summarize_content"))
			Expect(t.Declaration().Description).To(ContainSubstring("Summarize"))
		})

		It("should delegate to the underlying summarizer via Call", func(ctx context.Context) {
			fakeExpert := &expertfakes.FakeExpert{}
			fakeExpert.DoReturns(expert.Response{
				Choices: []model.Choice{
					{Message: model.Message{Content: "tool summary result"}},
				},
			}, nil)
			s := agentutils.NewSummarizerWithExpert(fakeExpert)
			t := agentutils.NewSummarizerTool(s)

			ct, ok := t.(tool.CallableTool)
			Expect(ok).To(BeTrue(), "expected tool to implement CallableTool")

			result, err := ct.Call(ctx, []byte(`{"content":"some verbose output","required_output_format":"TEXT"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(result.(string)).To(Equal("tool summary result"))
		})

		It("should return validation error for empty content via Call", func(ctx context.Context) {
			fakeExpert := &expertfakes.FakeExpert{}
			s := agentutils.NewSummarizerWithExpert(fakeExpert)
			t := agentutils.NewSummarizerTool(s)

			ct, ok := t.(tool.CallableTool)
			Expect(ok).To(BeTrue())

			_, err := ct.Call(ctx, []byte(`{"content":"","required_output_format":"JSON"}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("content must not be empty"))
		})
	})
})
