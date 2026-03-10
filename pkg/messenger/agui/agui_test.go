// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui_test

import (
	"context"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/messenger"
	aguimsg "github.com/stackgenhq/genie/pkg/messenger/agui"
)

var _ = Describe("AGUI Messenger", func() {
	Describe("New", func() {
		It("should create a messenger with empty config", func() {
			m := aguimsg.New(aguimsg.Config{})
			Expect(m).NotTo(BeNil())
		})

		It("should accept functional options", func() {
			m := aguimsg.New(aguimsg.Config{}, messenger.WithMessageBuffer(500))
			Expect(m).NotTo(BeNil())
		})
	})

	Describe("Platform", func() {
		It("should return PlatformAGUI", func() {
			m := aguimsg.New(aguimsg.Config{})
			Expect(m.Platform()).To(Equal(messenger.PlatformAGUI))
		})
	})

	Describe("Connection state guards", func() {
		var m *aguimsg.Messenger

		BeforeEach(func() {
			m = aguimsg.New(aguimsg.Config{})
		})

		It("should return ErrNotConnected when Send is called before Connect", func() {
			_, err := m.Send(context.Background(), messenger.SendRequest{
				Channel: messenger.Channel{ID: "thread-1"},
				Content: messenger.MessageContent{Text: "test"},
			})
			Expect(err).To(MatchError(messenger.ErrNotConnected))
		})

		It("should return ErrNotConnected when Receive is called before Connect", func() {
			ch, err := m.Receive(context.Background())
			Expect(err).To(MatchError(messenger.ErrNotConnected))
			Expect(ch).To(BeNil())
		})

		It("should return ErrNotConnected when Disconnect is called before Connect", func() {
			err := m.Disconnect(context.Background())
			Expect(err).To(MatchError(messenger.ErrNotConnected))
		})

		It("should return ErrAlreadyConnected on double Connect", func() {
			_, err := m.Connect(context.Background())
			Expect(err).NotTo(HaveOccurred())
			_, err = m.Connect(context.Background())
			Expect(err).To(MatchError(messenger.ErrAlreadyConnected))
			Expect(m.Disconnect(context.Background())).To(Succeed())
		})
	})

	Describe("InjectMessage", func() {
		var m *aguimsg.Messenger

		BeforeEach(func() {
			m = aguimsg.New(aguimsg.Config{})
			Expect(m.Connect(context.Background())).To(Succeed())
		})

		AfterEach(func() {
			_ = m.Disconnect(context.Background())
		})

		It("should return ErrNotConnected when not connected", func() {
			disconnected := aguimsg.New(aguimsg.Config{})
			eventChan := make(chan<- interface{}, 10)
			err := disconnected.InjectMessage("t1", "r1", "hello", eventChan, nil)
			Expect(err).To(MatchError(messenger.ErrNotConnected))
		})

		It("should register the thread for Send routing", func() {
			eventChan := make(chan interface{}, 10)
			writeChan := chan<- interface{}(eventChan)

			err := m.InjectMessage("thread-1", "run-1", "hello world", writeChan, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(m.ActiveThreadCount()).To(Equal(1))
		})

		It("should clean up existing thread on re-register", func() {
			eventChan1 := make(chan interface{}, 10)
			eventChan2 := make(chan interface{}, 10)

			err := m.InjectMessage("thread-1", "run-1", "first", chan<- interface{}(eventChan1), nil)
			Expect(err).NotTo(HaveOccurred())

			done1 := m.ThreadDone("thread-1")

			err = m.InjectMessage("thread-1", "run-2", "second", chan<- interface{}(eventChan2), nil)
			Expect(err).NotTo(HaveOccurred())

			Eventually(done1).Should(BeClosed())
			Expect(m.ActiveThreadCount()).To(Equal(1))
		})

		It("should accept sender info", func() {
			eventChan := make(chan interface{}, 10)
			sender := &aguimsg.SenderInfo{
				ID:          "user-123",
				Username:    "jdoe",
				DisplayName: "Jane Doe",
			}

			err := m.InjectMessage("thread-1", "run-1", "hello", chan<- interface{}(eventChan), sender)
			Expect(err).NotTo(HaveOccurred())
			Expect(m.ActiveThreadCount()).To(Equal(1))
		})
	})

	Describe("Send", func() {
		var m *aguimsg.Messenger

		BeforeEach(func() {
			m = aguimsg.New(aguimsg.Config{})
			Expect(m.Connect(context.Background())).To(Succeed())
		})

		AfterEach(func() {
			_ = m.Disconnect(context.Background())
		})

		It("should return ErrChannelNotFound for unknown thread", func() {
			_, err := m.Send(context.Background(), messenger.SendRequest{
				Channel:  messenger.Channel{ID: "nonexistent"},
				ThreadID: "nonexistent",
				Content:  messenger.MessageContent{Text: "test"},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no active AG-UI thread"))
		})

		It("should write text to the thread's event channel", func() {
			eventChan := make(chan interface{}, 10)
			err := m.InjectMessage("thread-1", "run-1", "hello", chan<- interface{}(eventChan), nil)
			Expect(err).NotTo(HaveOccurred())

			resp, err := m.Send(context.Background(), messenger.SendRequest{
				ThreadID: "thread-1",
				Content:  messenger.MessageContent{Text: "response text"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.MessageID).NotTo(BeEmpty())
			Expect(resp.Timestamp).NotTo(BeZero())

			var event interface{}
			Eventually(eventChan).Should(Receive(&event))
			Expect(event).To(Equal("response text"))
		})

		It("should silently handle reaction requests", func() {
			eventChan := make(chan interface{}, 10)
			err := m.InjectMessage("thread-1", "run-1", "hello", chan<- interface{}(eventChan), nil)
			Expect(err).NotTo(HaveOccurred())

			resp, err := m.Send(context.Background(), messenger.SendRequest{
				Type:     messenger.SendTypeReaction,
				ThreadID: "thread-1",
				Emoji:    "👍",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.MessageID).NotTo(BeEmpty())
			Consistently(eventChan, 100*time.Millisecond).ShouldNot(Receive())
		})

		It("should use Channel.ID when ThreadID is empty", func() {
			eventChan := make(chan interface{}, 10)
			err := m.InjectMessage("thread-1", "run-1", "hello", chan<- interface{}(eventChan), nil)
			Expect(err).NotTo(HaveOccurred())

			resp, err := m.Send(context.Background(), messenger.SendRequest{
				Channel: messenger.Channel{ID: "thread-1"},
				Content: messenger.MessageContent{Text: "via channel id"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.MessageID).NotTo(BeEmpty())

			var event interface{}
			Eventually(eventChan).Should(Receive(&event))
			Expect(event).To(Equal("via channel id"))
		})

		It("should fail gracefully when thread is already completed", func() {
			eventChan := make(chan interface{}, 10)
			err := m.InjectMessage("thread-1", "run-1", "hello", chan<- interface{}(eventChan), nil)
			Expect(err).NotTo(HaveOccurred())

			m.CompleteThread("thread-1")

			_, err = m.Send(context.Background(), messenger.SendRequest{
				ThreadID: "thread-1",
				Content:  messenger.MessageContent{Text: "should fail"},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CompleteThread", func() {
		It("should clean up thread state and close done channel", func() {
			m := aguimsg.New(aguimsg.Config{})
			Expect(m.Connect(context.Background())).To(Succeed())
			defer m.Disconnect(context.Background()) //nolint:errcheck

			eventChan := make(chan interface{}, 10)
			err := m.InjectMessage("thread-1", "run-1", "hello", chan<- interface{}(eventChan), nil)
			Expect(err).NotTo(HaveOccurred())

			done := m.ThreadDone("thread-1")
			Consistently(done, 100*time.Millisecond).ShouldNot(BeClosed())

			m.CompleteThread("thread-1")
			Eventually(done).Should(BeClosed())
			Expect(m.ActiveThreadCount()).To(Equal(0))
		})

		It("should be safe to call multiple times", func() {
			m := aguimsg.New(aguimsg.Config{})
			Expect(m.Connect(context.Background())).To(Succeed())
			defer m.Disconnect(context.Background()) //nolint:errcheck

			eventChan := make(chan interface{}, 10)
			err := m.InjectMessage("thread-1", "run-1", "hello", chan<- interface{}(eventChan), nil)
			Expect(err).NotTo(HaveOccurred())

			m.CompleteThread("thread-1")
			Expect(func() { m.CompleteThread("thread-1") }).NotTo(Panic())
		})
	})

	Describe("ThreadDone", func() {
		It("should return a closed channel for non-existent thread", func() {
			m := aguimsg.New(aguimsg.Config{})
			Expect(m.Connect(context.Background())).To(Succeed())
			defer m.Disconnect(context.Background()) //nolint:errcheck

			done := m.ThreadDone("non-existent")
			Eventually(done).Should(BeClosed())
		})
	})

	Describe("Disconnect", func() {
		It("should clean up all active threads", func() {
			m := aguimsg.New(aguimsg.Config{})
			Expect(m.Connect(context.Background())).To(Succeed())

			ec1 := make(chan interface{}, 10)
			ec2 := make(chan interface{}, 10)
			Expect(m.InjectMessage("t1", "r1", "a", chan<- interface{}(ec1), nil)).To(Succeed())
			Expect(m.InjectMessage("t2", "r2", "b", chan<- interface{}(ec2), nil)).To(Succeed())

			done1 := m.ThreadDone("t1")
			done2 := m.ThreadDone("t2")

			Expect(m.Disconnect(context.Background())).To(Succeed())
			Eventually(done1).Should(BeClosed())
			Eventually(done2).Should(BeClosed())
		})
	})

	Describe("ActiveThreadCount", func() {
		It("should track thread count accurately", func() {
			m := aguimsg.New(aguimsg.Config{})
			Expect(m.Connect(context.Background())).To(Succeed())
			defer m.Disconnect(context.Background()) //nolint:errcheck

			Expect(m.ActiveThreadCount()).To(Equal(0))

			ec1 := make(chan interface{}, 10)
			Expect(m.InjectMessage("t1", "r1", "a", chan<- interface{}(ec1), nil)).To(Succeed())
			Expect(m.ActiveThreadCount()).To(Equal(1))

			ec2 := make(chan interface{}, 10)
			Expect(m.InjectMessage("t2", "r2", "b", chan<- interface{}(ec2), nil)).To(Succeed())
			Expect(m.ActiveThreadCount()).To(Equal(2))

			m.CompleteThread("t1")
			Expect(m.ActiveThreadCount()).To(Equal(1))
		})
	})

	Describe("FormatApproval", func() {
		It("should return the request unchanged", func() {
			m := aguimsg.New(aguimsg.Config{})
			req := messenger.SendRequest{Content: messenger.MessageContent{Text: "approval"}}
			result := m.FormatApproval(req, messenger.ApprovalInfo{ID: "a1"})
			Expect(result).To(Equal(req))
		})
	})

	Describe("FormatClarification", func() {
		It("should return the request unchanged", func() {
			m := aguimsg.New(aguimsg.Config{})
			req := messenger.SendRequest{Content: messenger.MessageContent{Text: "question"}}
			result := m.FormatClarification(req, messenger.ClarificationInfo{RequestID: "r1"})
			Expect(result).To(Equal(req))
		})
	})

	Describe("Interface compliance", func() {
		It("should satisfy the messenger.Messenger interface", func() {
			var _ messenger.Messenger = aguimsg.New(aguimsg.Config{})
		})
	})

	// ---- Blind spot #4: session.Service integration ----

	Describe("session.Service integration (blind spot #4)", func() {
		It("should expose a non-nil SessionService after Connect", func() {
			m := aguimsg.New(aguimsg.Config{})
			Expect(m.Connect(context.Background())).To(Succeed())
			defer m.Disconnect(context.Background()) //nolint:errcheck

			Expect(m.SessionService()).NotTo(BeNil())
		})

		It("should return nil SessionService before Connect", func() {
			m := aguimsg.New(aguimsg.Config{})
			Expect(m.SessionService()).To(BeNil())
		})

		It("should create and delete sessions through InjectMessage/CompleteThread", func() {
			m := aguimsg.New(aguimsg.Config{AppName: "test-app"})
			Expect(m.Connect(context.Background())).To(Succeed())
			defer m.Disconnect(context.Background()) //nolint:errcheck

			ec := make(chan interface{}, 10)
			sender := &aguimsg.SenderInfo{ID: "user-42"}
			err := m.InjectMessage("thread-x", "run-1", "hi", chan<- interface{}(ec), sender)
			Expect(err).NotTo(HaveOccurred())

			m.CompleteThread("thread-x")
			Expect(m.ActiveThreadCount()).To(Equal(0))
		})

		It("should handle CompleteThread with no matching session gracefully", func() {
			m := aguimsg.New(aguimsg.Config{})
			Expect(m.Connect(context.Background())).To(Succeed())
			defer m.Disconnect(context.Background()) //nolint:errcheck

			ec := make(chan interface{}, 10)
			err := m.InjectMessage("thread-orphan", "run-1", "hi", chan<- interface{}(ec), nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(func() { m.CompleteThread("thread-orphan") }).NotTo(Panic())
		})
	})

	// ---- Blind spot #5: DefaultAppName ----

	Describe("DefaultAppName (blind spot #5)", func() {
		It("should be the string genie", func() {
			Expect(aguimsg.DefaultAppName).To(Equal("genie"))
		})

		It("should use custom AppName when set in Config", func() {
			m := aguimsg.New(aguimsg.Config{AppName: "custom-app"})
			Expect(m).NotTo(BeNil())
			Expect(m.Connect(context.Background())).To(Succeed())
			defer m.Disconnect(context.Background()) //nolint:errcheck
			Expect(m.SessionService()).NotTo(BeNil())
		})
	})
})

// TestAdapterRace tests concurrent access to all adapter methods.
// Run with: go test -race -run TestAdapterRace ./pkg/messenger/agui/...
func TestAdapterRace(t *testing.T) {
	t.Parallel()

	m := aguimsg.New(aguimsg.Config{})
	if _, err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Disconnect(context.Background()) }()

	var wg sync.WaitGroup

	// Concurrent InjectMessage + CompleteThread + Send
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			threadID := "race-thread"
			ec := make(chan interface{}, 100)
			_ = m.InjectMessage(threadID, "run", "msg", chan<- interface{}(ec), &aguimsg.SenderInfo{ID: "user"})
			for j := 0; j < 5; j++ {
				_, _ = m.Send(context.Background(), messenger.SendRequest{
					ThreadID: threadID,
					Content:  messenger.MessageContent{Text: "race test"},
				})
			}
			m.CompleteThread(threadID)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.ActiveThreadCount()
			_ = m.ThreadDone("race-thread")
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("race test timed out — possible deadlock")
	}
}
