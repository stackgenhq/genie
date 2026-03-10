// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap_test

import (
	"context"
	"errors"
	"time"

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

	It("should skip approval when tool is on approve list (blind)", func() {
		store.IsAllowedReturns(false)
		list := toolwrap.NewApproveList()
		list.AddBlind("write_file", 10*time.Minute)

		mw := toolwrap.HITLApprovalMiddleware(store, nil, toolwrap.WithApproveListOption(list))
		handler := mw.Wrap(passthrough("executed"))

		tc := &toolwrap.ToolCallContext{ToolName: "write_file", Args: []byte(`{"path":"/any"}`)}
		result, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("executed"))
		Expect(store.CreateCallCount()).To(Equal(0))
	})

	It("should skip approval when tool+args match approve list filter", func() {
		store.IsAllowedReturns(false)
		list := toolwrap.NewApproveList()
		list.AddWithArgsFilter("run_shell", []string{"/tmp"}, 10*time.Minute)

		mw := toolwrap.HITLApprovalMiddleware(store, nil, toolwrap.WithApproveListOption(list))
		handler := mw.Wrap(passthrough("executed"))

		tc := &toolwrap.ToolCallContext{ToolName: "run_shell", Args: []byte(`{"cmd":"ls /tmp"}`)}
		result, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("executed"))
		Expect(store.CreateCallCount()).To(Equal(0))
	})

	It("should create approval when tool not on approve list", func() {
		store.IsAllowedReturns(false)
		list := toolwrap.NewApproveList()
		list.AddBlind("write_file", 10*time.Minute)

		mw := toolwrap.HITLApprovalMiddleware(store, nil, toolwrap.WithApproveListOption(list))
		handler := mw.Wrap(passthrough("executed"))

		// Different tool — not on list
		store.CreateReturns(hitl.ApprovalRequest{ID: "a1"}, nil)
		store.WaitForResolutionReturns(hitl.ApprovalRequest{Status: hitl.StatusApproved}, nil)

		result, err := handler(context.Background(), tc("run_shell"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("executed"))
		Expect(store.CreateCallCount()).To(Equal(1))
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

	It("should auto-approve across middleware instances sharing a cache", func() {
		store.IsAllowedReturns(false)
		store.CreateReturns(hitl.ApprovalRequest{ID: "shared-1"}, nil)
		store.WaitForResolutionReturns(hitl.ApprovalRequest{
			Status: hitl.StatusApproved,
		}, nil)

		sharedOpt := toolwrap.NewSharedHITLCacheForTest()
		mw1 := toolwrap.HITLApprovalMiddleware(store, nil, sharedOpt)
		mw2 := toolwrap.HITLApprovalMiddleware(store, nil, sharedOpt)

		sharedTC := &toolwrap.ToolCallContext{ToolName: "write_file", Args: []byte(`{"path":"shared.txt"}`)}

		handler1 := mw1.Wrap(passthrough("ok-1"))
		result1, err := handler1(context.Background(), sharedTC)
		Expect(err).NotTo(HaveOccurred())
		Expect(result1).To(Equal("ok-1"))
		Expect(store.CreateCallCount()).To(Equal(1))

		handler2 := mw2.Wrap(passthrough("ok-2"))
		result2, err := handler2(context.Background(), sharedTC)
		Expect(err).NotTo(HaveOccurred())
		Expect(result2).To(Equal("ok-2"))
		Expect(store.CreateCallCount()).To(Equal(1)) // still 1 — cache hit across middlewares
	})
})
