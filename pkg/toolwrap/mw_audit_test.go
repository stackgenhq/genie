package toolwrap_test

import (
	"context"
	"errors"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/audit/auditfakes"
	"github.com/appcd-dev/genie/pkg/toolwrap"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EmitterMiddleware", func() {
	It("should emit AgentToolResponseMsg to event channel", func() {
		eventChan := make(chan interface{}, 10)
		mw := toolwrap.EmitterMiddleware(eventChan)
		handler := mw.Wrap(passthrough("result"))

		_, err := handler(context.Background(), tc("my_tool"))
		Expect(err).NotTo(HaveOccurred())

		Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
			toolMsg, ok := msg.(agui.AgentToolResponseMsg)
			return ok && toolMsg.ToolName == "my_tool" && toolMsg.Response == "result"
		})))
	})

	It("should not panic when eventChan is nil", func() {
		mw := toolwrap.EmitterMiddleware(nil)
		handler := mw.Wrap(passthrough("ok"))
		result, err := handler(context.Background(), tc("test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})
})

var _ = Describe("AuditMiddleware", func() {
	It("should log successful calls via auditor", func() {
		auditor := &auditfakes.FakeAuditor{}
		mw := toolwrap.AuditMiddleware(auditor)
		handler := mw.Wrap(passthrough("data"))

		_, err := handler(context.Background(), tc("read_file"))
		Expect(err).NotTo(HaveOccurred())
		Expect(auditor.LogCallCount()).To(Equal(1))

		_, req := auditor.LogArgsForCall(0)
		Expect(req.Metadata["error"]).To(Equal(""))
	})

	It("should log failed calls with error info", func() {
		auditor := &auditfakes.FakeAuditor{}
		mw := toolwrap.AuditMiddleware(auditor)
		handler := mw.Wrap(failing(errors.New("broken")))

		_, _ = handler(context.Background(), tc("run_shell"))
		Expect(auditor.LogCallCount()).To(Equal(1))

		_, req := auditor.LogArgsForCall(0)
		Expect(req.Metadata["error"]).To(Equal("broken"))
	})

	It("should use basicAuditor when nil (no panic)", func() {
		mw := toolwrap.AuditMiddleware(nil)
		handler := mw.Wrap(passthrough("ok"))
		result, err := handler(context.Background(), tc("test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})
})

var _ = Describe("LoggerMiddleware", func() {
	It("should pass through successful calls", func() {
		mw := toolwrap.LoggerMiddleware()
		handler := mw.Wrap(passthrough("ok"))
		result, err := handler(context.Background(), tc("test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should pass through errors", func() {
		mw := toolwrap.LoggerMiddleware()
		handler := mw.Wrap(failing(errors.New("fail")))
		_, err := handler(context.Background(), tc("test"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("fail"))
	})
})
