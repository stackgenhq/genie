package toolwrap_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/hitl/hitlfakes"
	"github.com/stackgenhq/genie/pkg/messenger"
	rtmemory "github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("HITLApprovalMiddleware", func() {
	var (
		store     *hitlfakes.FakeApprovalStore
		eventChan chan interface{}
	)

	BeforeEach(func() {
		store = &hitlfakes.FakeApprovalStore{}
		eventChan = make(chan interface{}, 10)
	})

	It("should skip approval for allowed tools", func() {
		store.IsAllowedReturns(true)
		mw := toolwrap.HITLApprovalMiddleware(store, nil)
		handler := mw.Wrap(passthrough("ok"))

		result, err := handler(context.Background(), tc("read_file"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
		Expect(store.CreateCallCount()).To(Equal(0))
	})

	It("should pass through when store is nil", func() {
		mw := toolwrap.HITLApprovalMiddleware(nil, nil)
		handler := mw.Wrap(passthrough("ok"))

		result, err := handler(context.Background(), tc("dangerous_tool"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should approve and execute when approval is granted", func() {
		store.IsAllowedReturns(false)
		store.CreateReturns(hitl.ApprovalRequest{ID: "a1"}, nil)
		store.WaitForResolutionReturns(hitl.ApprovalRequest{
			Status: hitl.StatusApproved,
		}, nil)

		mw := toolwrap.HITLApprovalMiddleware(store, nil)
		handler := mw.Wrap(passthrough("executed"))

		result, err := handler(context.Background(), tc("write_file"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("executed"))
		Expect(store.CreateCallCount()).To(Equal(1))
	})

	It("should emit ToolApprovalRequestMsg on event channel", func() {
		store.IsAllowedReturns(false)
		store.CreateReturns(hitl.ApprovalRequest{ID: "a99"}, nil)
		store.WaitForResolutionReturns(hitl.ApprovalRequest{
			Status: hitl.StatusApproved,
		}, nil)

		// Register the eventChan on the bus with a MessageOrigin
		origin := messenger.MessageOrigin{
			Platform: messenger.PlatformAGUI,
			Channel:  messenger.Channel{ID: "hitl-emit-test"},
			Sender:   messenger.Sender{ID: "test"},
		}
		agui.Register(origin, eventChan)
		defer agui.Deregister(origin)
		ctx := messenger.WithMessageOrigin(context.Background(), origin)

		mw := toolwrap.HITLApprovalMiddleware(store, nil)
		handler := mw.Wrap(passthrough("ok"))
		handler(ctx, tc("deploy")) //nolint:errcheck

		Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
			req, ok := msg.(agui.ToolApprovalRequestMsg)
			return ok && req.ApprovalID == "a99" && req.ToolName == "deploy"
		})))
	})

	It("should reject when approval is denied", func() {
		store.IsAllowedReturns(false)
		store.CreateReturns(hitl.ApprovalRequest{ID: "a2"}, nil)
		store.WaitForResolutionReturns(hitl.ApprovalRequest{
			Status: hitl.StatusRejected,
		}, nil)

		mw := toolwrap.HITLApprovalMiddleware(store, nil)
		handler := mw.Wrap(passthrough("should-not-execute"))

		_, err := handler(context.Background(), tc("delete_all"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("rejected"))
	})

	It("should return rejection feedback as error", func() {
		store.IsAllowedReturns(false)
		store.CreateReturns(hitl.ApprovalRequest{ID: "a3"}, nil)
		store.WaitForResolutionReturns(hitl.ApprovalRequest{
			Status:   hitl.StatusRejected,
			Feedback: "too dangerous",
		}, nil)

		mw := toolwrap.HITLApprovalMiddleware(store, nil)
		handler := mw.Wrap(passthrough("nope"))

		_, err := handler(context.Background(), tc("rm_rf"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("too dangerous"))
	})

	It("should return approved-with-feedback as re-planning error", func() {
		store.IsAllowedReturns(false)
		store.CreateReturns(hitl.ApprovalRequest{ID: "a4"}, nil)
		store.WaitForResolutionReturns(hitl.ApprovalRequest{
			Status:   hitl.StatusApproved,
			Feedback: "use staging instead",
		}, nil)

		wm := rtmemory.NewWorkingMemory()
		mw := toolwrap.HITLApprovalMiddleware(store, wm)
		handler := mw.Wrap(passthrough("nope"))

		_, err := handler(context.Background(), tc("deploy"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("use staging instead"))

		feedback, ok := wm.Recall("hitl:feedback:deploy")
		Expect(ok).To(BeTrue())
		Expect(feedback).To(ContainSubstring("use staging instead"))
	})

	It("should auto-approve on cache hit (same session + tool + args)", func() {
		store.IsAllowedReturns(false)
		store.CreateReturns(hitl.ApprovalRequest{ID: "a5"}, nil)
		store.WaitForResolutionReturns(hitl.ApprovalRequest{
			Status: hitl.StatusApproved,
		}, nil)

		mw := toolwrap.HITLApprovalMiddleware(store, nil)
		handler := mw.Wrap(passthrough("ok"))
		tc := &toolwrap.ToolCallContext{ToolName: "write_file", Args: []byte(`{"path":"a.txt"}`)}

		_, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.CreateCallCount()).To(Equal(1))

		_, err = handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.CreateCallCount()).To(Equal(1)) // cache hit
	})

	It("should propagate Create errors", func() {
		store.IsAllowedReturns(false)
		store.CreateReturns(hitl.ApprovalRequest{}, errors.New("db down"))

		mw := toolwrap.HITLApprovalMiddleware(store, nil)
		handler := mw.Wrap(passthrough("nope"))

		_, err := handler(context.Background(), tc("write_file"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("db down"))
	})

	It("should propagate WaitForResolution errors", func() {
		store.IsAllowedReturns(false)
		store.CreateReturns(hitl.ApprovalRequest{ID: "a6"}, nil)
		store.WaitForResolutionReturns(hitl.ApprovalRequest{}, errors.New("timeout"))

		mw := toolwrap.HITLApprovalMiddleware(store, nil)
		handler := mw.Wrap(passthrough("nope"))

		_, err := handler(context.Background(), tc("write_file"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("timeout"))
	})
})
