package app

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/clarify"
	"github.com/stackgenhq/genie/pkg/config"
	geniedb "github.com/stackgenhq/genie/pkg/db"
	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/hitl/hitlfakes"
	"github.com/stackgenhq/genie/pkg/messenger"
	messengerhitl "github.com/stackgenhq/genie/pkg/messenger/hitl"
	"github.com/stackgenhq/genie/pkg/messenger/messengerfakes"
	"github.com/stackgenhq/genie/pkg/orchestrator/orchestratorfakes"
)

var _ = Describe("Application handleMessengerInput", func() {
	var (
		fakeStore     *hitlfakes.FakeApprovalStore
		fakeMessenger *messengerfakes.FakeMessenger
		fakeCodeOwner *orchestratorfakes.FakeOrchestrator
		notifierStore *messengerhitl.NotifierStore
		application   *Application
		ctx           context.Context
	)

	BeforeEach(func() {
		fakeStore = &hitlfakes.FakeApprovalStore{}
		fakeMessenger = &messengerfakes.FakeMessenger{}
		fakeCodeOwner = &orchestratorfakes.FakeOrchestrator{}
		notifierStore = messengerhitl.NewNotifierStore(fakeStore, fakeMessenger)
		ctx = context.Background()

		// Create a temp DB-backed shortMemory for tests.
		tmpDir := GinkgoT().TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")
		gormDB, err := geniedb.Open(dbPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(geniedb.AutoMigrate(gormDB)).To(Succeed())
		DeferCleanup(func() {
			_ = geniedb.Close(gormDB)
			_ = os.RemoveAll(tmpDir)
		})

		application = &Application{
			notifierStore: notifierStore,
			msgr:          fakeMessenger,
			codeOwner:     fakeCodeOwner,
			clarifyStore:  clarify.NewStore(gormDB),
			shortMemory:   geniedb.NewShortMemoryStore(gormDB),
			workingDir:    "/tmp/genie-test",
		}
	})

	Context("when a pending approval exists", func() {
		var approvalID string
		var realMsg messenger.IncomingMessage

		BeforeEach(func() {
			approvalID = "app-123"

			realMsg = messenger.IncomingMessage{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "user1"},
				Channel:  messenger.Channel{ID: "C123"},
			}

			// Setup: Create a pending approval
			fakeStore.CreateReturns(hitl.ApprovalRequest{ID: approvalID, ToolName: "test_tool"}, nil)

			origin := messenger.MessageOrigin{
				Platform: realMsg.Platform,
				Sender:   realMsg.Sender,
				Channel:  realMsg.Channel,
			}
			ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)
			_, err := notifierStore.Create(ctxWithOrigin, hitl.CreateRequest{ToolName: "test_tool"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("approves when user says 'Yes'", func() {
			realMsg.Content.Text = "Yes"

			application.handleMessengerInput(ctx, realMsg)

			// Verify Resolve called
			Expect(fakeStore.ResolveCallCount()).To(Equal(1))
			_, resolveReq := fakeStore.ResolveArgsForCall(0)
			Expect(resolveReq.ApprovalID).To(Equal(approvalID))
			Expect(resolveReq.Decision).To(Equal(hitl.StatusApproved))

			// Verify Confirmation sent (1 for "Approval Required" during Create, 1 for "Approved")
			Expect(fakeMessenger.SendCallCount()).To(Equal(2))
			_, sendReq := fakeMessenger.SendArgsForCall(1)
			Expect(sendReq.Content.Text).To(ContainSubstring("Approved"))

			// Verify it did NOT chat
			Expect(fakeCodeOwner.ChatCallCount()).To(Equal(0))
		})

		It("rejects when user says 'No'", func() {
			realMsg.Content.Text = "No"

			application.handleMessengerInput(ctx, realMsg)

			Expect(fakeStore.ResolveCallCount()).To(Equal(1))
			_, resolveReq := fakeStore.ResolveArgsForCall(0)
			Expect(resolveReq.ApprovalID).To(Equal(approvalID))
			Expect(resolveReq.Decision).To(Equal(hitl.StatusRejected))

			Expect(fakeMessenger.SendCallCount()).To(Equal(2))
			_, sendReq := fakeMessenger.SendArgsForCall(1)
			Expect(sendReq.Content.Text).To(ContainSubstring("Rejected"))

			Expect(fakeCodeOwner.ChatCallCount()).To(Equal(0))
		})
	})

	Context("when no pending approval exists", func() {
		It("treats 'Yes' as regular chat message", func() {
			msg := messenger.IncomingMessage{
				Content:  messenger.MessageContent{Text: "Yes"},
				Platform: "slack",
				Sender:   messenger.Sender{ID: "user1"},
				Channel:  messenger.Channel{ID: "C123"},
			}
			application.handleMessengerInput(ctx, msg)

			Eventually(fakeCodeOwner.ChatCallCount).Should(Equal(1))
		})
	})

	Context("when a pending clarification exists", func() {
		var senderCtx string
		var requestID string
		var respCh <-chan clarify.Response
		var realMsg messenger.IncomingMessage

		BeforeEach(func() {
			realMsg = messenger.IncomingMessage{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "user1"},
				Channel:  messenger.Channel{ID: "C123"},
			}
			senderCtx = realMsg.String()

			// Setup: Create a pending clarification in the store
			var err error
			requestID, respCh, err = application.clarifyStore.Ask(ctx, "What is the target environment?", "", senderCtx)
			Expect(err).NotTo(HaveOccurred())
			_ = application.shortMemory.Set(ctx, "pending_clarification", senderCtx, requestID, 10*time.Minute)
		})

		It("delivers answer and sends confirmation", func() {
			realMsg.Content.Text = "production"

			// Start waiting for the response in a goroutine
			type waitResult struct {
				resp clarify.Response
				err  error
			}
			resultCh := make(chan waitResult, 10)
			waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
			defer waitCancel()
			go func() {
				resp, err := application.clarifyStore.WaitForResponse(waitCtx, requestID, respCh)
				resultCh <- waitResult{resp, err}
			}()

			application.handleMessengerInput(ctx, realMsg)

			// Verify answer was delivered
			Eventually(resultCh, 1*time.Second).Should(Receive(WithTransform(
				func(r waitResult) string { return r.resp.Answer },
				Equal("production"),
			)))

			// Verify confirmation reaction sent (👍 on the user's message)
			Expect(fakeMessenger.SendCallCount()).To(Equal(1))
			_, sendReq := fakeMessenger.SendArgsForCall(0)
			Expect(sendReq.Type).To(Equal(messenger.SendTypeReaction))
			Expect(sendReq.Emoji).To(Equal("👍"))

			// Verify the entry was cleaned up
			_, loaded, _ := application.shortMemory.Get(ctx, "pending_clarification", senderCtx)
			Expect(loaded).To(BeFalse())

			// Verify it did NOT reach approval or chat
			Expect(fakeStore.ResolveCallCount()).To(Equal(0))
			Expect(fakeCodeOwner.ChatCallCount()).To(Equal(0))
		})

		It("takes priority over pending approval", func() {
			// Also set up a pending approval for the same senderCtx
			fakeStore.CreateReturns(hitl.ApprovalRequest{ID: "app-456", ToolName: "test_tool"}, nil)
			origin := messenger.MessageOrigin{
				Platform: realMsg.Platform,
				Sender:   realMsg.Sender,
				Channel:  realMsg.Channel,
			}
			ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)
			_, err := notifierStore.Create(ctxWithOrigin, hitl.CreateRequest{ToolName: "test_tool"})
			Expect(err).NotTo(HaveOccurred())

			realMsg.Content.Text = "staging"

			// Start waiting for the response in a goroutine
			type waitResult struct {
				resp clarify.Response
				err  error
			}
			resultCh := make(chan waitResult, 1)
			waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
			defer waitCancel()
			go func() {
				resp, err := application.clarifyStore.WaitForResponse(waitCtx, requestID, respCh)
				resultCh <- waitResult{resp, err}
			}()

			application.handleMessengerInput(ctx, realMsg)

			// Should resolve clarification, NOT approval
			Eventually(resultCh, 10*time.Second).Should(Receive(WithTransform(
				func(r waitResult) string { return r.resp.Answer },
				Equal("staging"),
			)))
			Expect(fakeStore.ResolveCallCount()).To(Equal(0))
		})
	})

	Context("when clarification request was already answered", func() {
		It("sends error message for expired request", func() {
			msg := messenger.IncomingMessage{
				Content:  messenger.MessageContent{Text: "my answer"},
				Platform: "slack",
				Sender:   messenger.Sender{ID: "user1"},
				Channel:  messenger.Channel{ID: "C123"},
			}
			senderCtx := msg.String()

			// Store a request ID that doesn't exist in the clarify store
			_ = application.shortMemory.Set(ctx, "pending_clarification", senderCtx, "non-existent-id", 10*time.Minute)

			application.handleMessengerInput(ctx, msg)

			// Should send error message
			Expect(fakeMessenger.SendCallCount()).To(Equal(1))
			_, sendReq := fakeMessenger.SendArgsForCall(0)
			Expect(sendReq.Content.Text).To(ContainSubstring("Failed to submit answer"))

			// Should NOT reach chat
			Expect(fakeCodeOwner.ChatCallCount()).To(Equal(0))
		})
	})
})

var _ = Describe("truncateForLog", func() {
	It("should return short strings unchanged", func() {
		Expect(truncateForLog("hello", 10)).To(Equal("hello"))
	})

	It("should return strings at exact max length unchanged", func() {
		Expect(truncateForLog("12345", 5)).To(Equal("12345"))
	})

	It("should truncate long strings with ellipsis", func() {
		Expect(truncateForLog("hello world", 5)).To(Equal("hello..."))
	})

	It("should handle empty string", func() {
		Expect(truncateForLog("", 10)).To(Equal(""))
	})
})

var _ = Describe("loadAgentsGuide", func() {
	It("should load from custom persona file (relative path)", func() {
		tmpDir := GinkgoT().TempDir()
		content := "# Custom Standards"
		err := os.WriteFile(filepath.Join(tmpDir, "STANDARDS.md"), []byte(content), 0644)
		Expect(err).NotTo(HaveOccurred())
		a := &Application{
			cfg: config.GenieConfig{
				PersonaFile: "STANDARDS.md",
			},
			workingDir: tmpDir,
		}

		result := a.persona()
		Expect(result).To(Equal(content))
	})

	It("should load from custom persona file (absolute path)", func() {
		tmpDir := GinkgoT().TempDir()
		content := "# Absolute Custom Standards"
		absPath := filepath.Join(tmpDir, "custom.md")
		err := os.WriteFile(absPath, []byte(content), 0644)
		Expect(err).NotTo(HaveOccurred())
		a := &Application{
			cfg: config.GenieConfig{
				PersonaFile: absPath,
			},
		}

		result := a.persona()
		Expect(result).To(Equal(content))
	})

	It("should return empty string when custom persona file does not exist", func() {
		a := &Application{}
		result := a.persona()
		Expect(result).To(BeEmpty())
	})
})

var _ = Describe("startHTTPServer", func() {
	It("returns error when address is already in use", func(ctx context.Context) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		defer listener.Close()
		addr := listener.Addr().String()

		err = startHTTPServer(ctx, addr, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("address already in use"))
	})
})

var _ = Describe("parseApprovalActionFromInteraction", func() {
	It("returns approve outcome for approve_ prefix", func() {
		approvalID, outcome, feedback, ok := parseApprovalActionFromInteraction("approve_abc", "abc", "Alice")
		Expect(ok).To(BeTrue())
		Expect(approvalID).To(Equal("abc"))
		Expect(outcome.Status).To(Equal(hitl.StatusApproved))
		Expect(outcome.ReplyText).To(ContainSubstring("Approved"))
		Expect(outcome.ReplyText).To(ContainSubstring("Alice"))
		Expect(feedback).To(BeEmpty())
	})

	It("returns reject outcome for reject_ prefix", func() {
		approvalID, outcome, feedback, ok := parseApprovalActionFromInteraction("reject_xyz", "xyz", "Bob")
		Expect(ok).To(BeTrue())
		Expect(approvalID).To(Equal("xyz"))
		Expect(outcome.Status).To(Equal(hitl.StatusRejected))
		Expect(outcome.ReplyText).To(ContainSubstring("Rejected"))
		Expect(outcome.ReplyText).To(ContainSubstring("Bob"))
		Expect(feedback).To(BeEmpty())
	})

	It("returns revisit outcome and feedback for revisit_ prefix", func() {
		approvalID, outcome, feedback, ok := parseApprovalActionFromInteraction("revisit_123", "123", "Carol")
		Expect(ok).To(BeTrue())
		Expect(approvalID).To(Equal("123"))
		Expect(outcome.Status).To(Equal(hitl.StatusRejected))
		Expect(outcome.ReplyText).To(ContainSubstring("Sent back for revision"))
		Expect(outcome.ReplyText).To(ContainSubstring("Carol"))
		Expect(feedback).To(Equal("Sent back for revision via button click"))
	})

	It("returns ok false when actionValue is empty", func() {
		_, _, _, ok := parseApprovalActionFromInteraction("approve_abc", "", "Alice")
		Expect(ok).To(BeFalse())
	})

	It("returns ok false for unrecognized actionID prefix", func() {
		_, _, _, ok := parseApprovalActionFromInteraction("unknown_abc", "abc", "Alice")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("reactionEmojiToOutcome", func() {
	It("returns approved for thumbs up and checkmark", func() {
		o, ok := reactionEmojiToOutcome("👍")
		Expect(ok).To(BeTrue())
		Expect(o.Status).To(Equal(hitl.StatusApproved))
		o, ok = reactionEmojiToOutcome("✅")
		Expect(ok).To(BeTrue())
		Expect(o.Status).To(Equal(hitl.StatusApproved))
	})

	It("returns rejected for thumbs down", func() {
		o, ok := reactionEmojiToOutcome("👎")
		Expect(ok).To(BeTrue())
		Expect(o.Status).To(Equal(hitl.StatusRejected))
	})

	It("returns ok false for unknown emoji", func() {
		_, ok := reactionEmojiToOutcome("❤️")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("textReplyToOutcome", func() {
	It("returns approved when isApproval is true", func() {
		o := textReplyToOutcome(true, false, "")
		Expect(o.Status).To(Equal(hitl.StatusApproved))
	})

	It("returns rejected when isRejection is true", func() {
		o := textReplyToOutcome(false, true, "")
		Expect(o.Status).To(Equal(hitl.StatusRejected))
	})

	It("returns revision outcome with feedback when neither yes nor no", func() {
		o := textReplyToOutcome(false, false, "use staging first")
		Expect(o.Status).To(Equal(hitl.StatusRejected))
		Expect(o.ReplyText).To(ContainSubstring("revision"))
		Expect(o.ReplyText).To(ContainSubstring("use staging first"))
	})
})

var _ = Describe("parseApprovalTextReply", func() {
	It("recognizes exact yes/y/approve as approval", func() {
		for _, t := range []string{"yes", "y", "approve"} {
			p := parseApprovalTextReply(t)
			Expect(p.IsApproval).To(BeTrue())
			Expect(p.IsRejection).To(BeFalse())
			Expect(p.AllowForMins).To(Equal(0))
			Expect(p.AllowWhenArgsContain).To(BeEmpty())
		}
	})

	It("recognizes exact no/n/reject as rejection", func() {
		for _, t := range []string{"no", "n", "reject"} {
			p := parseApprovalTextReply(t)
			Expect(p.IsRejection).To(BeTrue())
			Expect(p.IsApproval).To(BeFalse())
		}
	})

	It("parses approval with allow for X mins", func() {
		p := parseApprovalTextReply("yes, allow for 10 mins")
		Expect(p.IsApproval).To(BeTrue())
		Expect(p.AllowForMins).To(Equal(10))

		p = parseApprovalTextReply("y allow for 30 min")
		Expect(p.IsApproval).To(BeTrue())
		Expect(p.AllowForMins).To(Equal(30))
	})

	It("parses approval with when args contain", func() {
		p := parseApprovalTextReply("yes, when args contain /tmp")
		Expect(p.IsApproval).To(BeTrue())
		Expect(p.AllowWhenArgsContain).To(Equal([]string{"/tmp"}))

		p = parseApprovalTextReply("approve, only when args contain /docs, /backup")
		Expect(p.IsApproval).To(BeTrue())
		Expect(p.AllowWhenArgsContain).To(Equal([]string{"/docs", "/backup"}))
	})

	It("treats other text as feedback (revisit)", func() {
		p := parseApprovalTextReply("use staging first")
		Expect(p.IsApproval).To(BeFalse())
		Expect(p.IsRejection).To(BeFalse())
		Expect(p.Feedback).To(Equal("use staging first"))
	})

	It("recognizes yes with trailing comma as approval with no options", func() {
		p := parseApprovalTextReply("yes,")
		Expect(p.IsApproval).To(BeTrue())
		Expect(p.AllowForMins).To(Equal(0))
		Expect(p.AllowWhenArgsContain).To(BeEmpty())
	})

	It("parses combined allow for and when args contain", func() {
		p := parseApprovalTextReply("yes, allow for 10 mins, when args contain /tmp")
		Expect(p.IsApproval).To(BeTrue())
		Expect(p.AllowForMins).To(Equal(10))
		Expect(p.AllowWhenArgsContain).To(Equal([]string{"/tmp"}))
	})

	It("is case-insensitive for approval prefix", func() {
		p := parseApprovalTextReply("YES, allow for 5 min")
		Expect(p.IsApproval).To(BeTrue())
		Expect(p.AllowForMins).To(Equal(5))
	})
})
