package hitl_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/hitl"
)

// These tests cover the String() methods and struct field tests
// in store.go that were previously at 0% coverage.
var _ = Describe("ApprovalRequest.String formatting", func() {
	It("formats a basic approval request with tool name and args", func() {
		req := hitl.ApprovalRequest{
			ID:       "test-id-123",
			ThreadID: "thread-1",
			RunID:    "run-1",
			ToolName: "run_shell",
			Args:     `{"command":"ls -la"}`,
			Status:   hitl.StatusPending,
		}
		s := req.String()
		Expect(s).To(ContainSubstring("Approval Required"))
		Expect(s).To(ContainSubstring("run_shell"))
		Expect(s).To(ContainSubstring("ls -la"))
	})

	It("includes feedback when present", func() {
		req := hitl.ApprovalRequest{
			ToolName: "write_file",
			Args:     `{"path":"/tmp/test"}`,
			Feedback: "Need to create configuration file",
			Status:   hitl.StatusPending,
		}
		s := req.String()
		Expect(s).To(ContainSubstring("Why"))
		Expect(s).To(ContainSubstring("Need to create configuration file"))
	})

	It("handles invalid JSON args gracefully", func() {
		req := hitl.ApprovalRequest{
			ToolName: "run_shell",
			Args:     "not-valid-json",
			Status:   hitl.StatusPending,
		}
		s := req.String()
		// Should not panic, just use the raw args
		Expect(s).To(ContainSubstring("not-valid-json"))
	})

	It("pretty-prints valid JSON args", func() {
		req := hitl.ApprovalRequest{
			ToolName: "run_shell",
			Args:     `{"key":"value","nested":{"a":"b"}}`,
			Status:   hitl.StatusPending,
		}
		s := req.String()
		// Pretty-printed JSON has indentation
		Expect(s).To(ContainSubstring("key"))
		Expect(s).To(ContainSubstring("value"))
	})

	It("handles empty args", func() {
		req := hitl.ApprovalRequest{
			ToolName: "test_tool",
			Args:     "",
			Status:   hitl.StatusPending,
		}
		s := req.String()
		Expect(s).To(ContainSubstring("test_tool"))
	})

	It("includes instructions for approval/rejection", func() {
		req := hitl.ApprovalRequest{
			ToolName: "dangerous_tool",
			Args:     `{}`,
			Status:   hitl.StatusPending,
		}
		s := req.String()
		Expect(s).To(ContainSubstring("Yes"))
		Expect(s).To(ContainSubstring("No"))
		Expect(s).To(ContainSubstring("approve"))
		Expect(s).To(ContainSubstring("reject"))
	})

	It("omits feedback section when feedback is empty", func() {
		req := hitl.ApprovalRequest{
			ToolName: "run_shell",
			Args:     `{}`,
			Feedback: "",
			Status:   hitl.StatusPending,
		}
		s := req.String()
		Expect(s).NotTo(ContainSubstring("Why"))
	})
})

var _ = Describe("Type data structures coverage", func() {
	Describe("ReplayableApproval", func() {
		It("has fields for replay", func() {
			ra := hitl.ReplayableApproval{
				ApprovalID:    "a-123",
				Question:      "Should I deploy?",
				SenderContext: "slack:U123:C456",
			}
			Expect(ra.ApprovalID).To(Equal("a-123"))
			Expect(ra.Question).To(Equal("Should I deploy?"))
			Expect(ra.SenderContext).To(Equal("slack:U123:C456"))
		})
	})

	Describe("RecoverResult", func() {
		It("tracks expired and recovered counts", func() {
			rr := hitl.RecoverResult{
				Expired:   3,
				Recovered: 2,
				Replayable: []hitl.ReplayableApproval{
					{ApprovalID: "a1", Question: "q1"},
				},
			}
			Expect(rr.Expired).To(Equal(3))
			Expect(rr.Recovered).To(Equal(2))
			Expect(rr.Replayable).To(HaveLen(1))
		})
	})
})
