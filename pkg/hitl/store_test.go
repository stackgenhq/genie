package hitl_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/db"
	"github.com/stackgenhq/genie/pkg/hitl"
	"gorm.io/gorm"
)

var _ = Describe("GORMStore", func() {
	var (
		ctx    context.Context
		store  hitl.ApprovalStore
		gormDB *gorm.DB
		dbDir  string
		cfg    hitl.Config
	)

	BeforeEach(func() {
		cfg = hitl.Config{
			AlwaysAllowed: []string{"read_file", "list_file"},
		}
		ctx = context.Background()
		var err error
		dbDir, err = os.MkdirTemp("", "hitl-test-*")
		Expect(err).NotTo(HaveOccurred())

		dbPath := filepath.Join(dbDir, "test.db")
		gormDB, err = db.Open(dbPath)
		Expect(err).NotTo(HaveOccurred())

		Expect(db.AutoMigrate(gormDB)).To(Succeed())
		store = cfg.NewStore(gormDB)
	})

	AfterEach(func() {
		if store != nil {
			Expect(store.Close()).To(Succeed())
		}
		if gormDB != nil {
			db.Close(gormDB) //nolint:errcheck
		}
		os.RemoveAll(dbDir)
	})
	Describe("IsAllowed", func() {
		It("should allow for all if * is present in cfg", func() {
			cfg := hitl.Config{
				AlwaysAllowed: []string{"*"},
			}
			Expect(cfg.IsAllowed("read_file")).To(BeTrue())
			Expect(cfg.IsAllowed("fix_cancer")).To(BeTrue())
		})
		It("should return true for allowed tools", func() {
			Expect(cfg.IsAllowed("read_file")).To(BeTrue())
			Expect(cfg.IsAllowed("list_file")).To(BeTrue())
		})
		It("should return false for disallowed tools", func() {
			Expect(cfg.IsAllowed("write_file")).To(BeFalse())
			Expect(cfg.IsAllowed("execute_code")).To(BeFalse())
		})
		It("should match prefix wildcards like browser_*", func() {
			wcCfg := hitl.Config{
				AlwaysAllowed: []string{"browser_*"},
			}
			Expect(wcCfg.IsAllowed("browser_navigate")).To(BeTrue())
			Expect(wcCfg.IsAllowed("browser_read_text")).To(BeTrue())
			Expect(wcCfg.IsAllowed("browser_click")).To(BeTrue())
			// Should NOT match tools that don't start with the prefix
			Expect(wcCfg.IsAllowed("run_shell")).To(BeFalse())
			Expect(wcCfg.IsAllowed("write_file")).To(BeFalse())
		})
		It("should be case-insensitive for prefix wildcards", func() {
			wcCfg := hitl.Config{
				AlwaysAllowed: []string{"Browser_*"},
			}
			Expect(wcCfg.IsAllowed("browser_navigate")).To(BeTrue())
			Expect(wcCfg.IsAllowed("BROWSER_READ_TEXT")).To(BeTrue())
		})
		It("should support mixing exact names and wildcards", func() {
			mixCfg := hitl.Config{
				AlwaysAllowed: []string{"run_shell", "browser_*"},
			}
			// Exact match
			Expect(mixCfg.IsAllowed("run_shell")).To(BeTrue())
			// Prefix wildcard match
			Expect(mixCfg.IsAllowed("browser_navigate")).To(BeTrue())
			// Neither
			Expect(mixCfg.IsAllowed("write_file")).To(BeFalse())
		})
		It("should handle wildcard-only prefix (just the star suffix)", func() {
			// "web_*" should match web_search but not websearch (no underscore)
			wcCfg := hitl.Config{
				AlwaysAllowed: []string{"web_*"},
			}
			Expect(wcCfg.IsAllowed("web_search")).To(BeTrue())
			Expect(wcCfg.IsAllowed("web_browse")).To(BeTrue())
			Expect(wcCfg.IsAllowed("websearch")).To(BeFalse())
		})
	})

	Describe("Create", func() {
		It("should create a pending approval with a generated ID", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "thread-1",
				RunID:    "run-1",
				ToolName: "write_file",
				Args:     `{"path":"test.txt","content":"hello"}`,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(approval.ID).NotTo(BeEmpty())
			Expect(approval.Status).To(Equal(hitl.StatusPending))
			Expect(approval.ToolName).To(Equal("write_file"))
			Expect(approval.ThreadID).To(Equal("thread-1"))
			Expect(approval.RunID).To(Equal("run-1"))
			Expect(approval.CreatedAt).NotTo(BeZero())
		})
	})

	Describe("Resolve", func() {
		It("should approve a pending request", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "execute_code", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusApproved,
				ResolvedBy: "user@example.com",
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reject a pending request", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "execute_code", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusRejected,
				ResolvedBy: "user@example.com",
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail for already resolved requests", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "execute_code", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusApproved,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second resolve should fail
			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusRejected,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("already resolved"))
		})

		It("should fail for invalid decision", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "execute_code", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.ApprovalStatus("invalid"),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid decision"))
		})

		It("should fail for nonexistent approval ID", func() {
			err := store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: "nonexistent-id",
				Decision:   hitl.StatusApproved,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Describe("WaitForResolution", func() {
		It("should return immediately if already resolved", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "write_file", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			// Resolve before waiting
			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusApproved,
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait should return immediately with approved status
			result, err := store.WaitForResolution(ctx, approval.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(hitl.StatusApproved))
		})

		It("should block until resolved in another goroutine", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "execute_code", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			resultChan := make(chan hitl.ApprovalRequest, 1)
			errChan := make(chan error, 1)

			// Wait in a goroutine
			go func() {
				res, err := store.WaitForResolution(ctx, approval.ID)
				errChan <- err
				resultChan <- res
			}()

			// Small delay then resolve
			time.Sleep(50 * time.Millisecond)
			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusRejected,
				ResolvedBy: "tester",
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(errChan, 2*time.Second).Should(Receive(BeNil()))
			Eventually(resultChan, 2*time.Second).Should(Receive(Satisfy(func(r hitl.ApprovalRequest) bool {
				return r.Status == hitl.StatusRejected && r.ResolvedBy == "tester"
			})))
		})

		It("should respect context cancellation", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "write_file", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			cancelCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()

			_, err = store.WaitForResolution(cancelCtx, approval.ID)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(context.DeadlineExceeded))
		})
	})

	Describe("Resolve with feedback", func() {
		It("should persist feedback when approving", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "run_shell", Args: `{"cmd":"echo hi"}`,
			})
			Expect(err).NotTo(HaveOccurred())

			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusApproved,
				ResolvedBy: "user@test.com",
				Feedback:   "please use a safer command",
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := store.WaitForResolution(ctx, approval.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(hitl.StatusApproved))
			Expect(result.Feedback).To(Equal("please use a safer command"))
		})

		It("should persist feedback when rejecting", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "write_file", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusRejected,
				ResolvedBy: "reviewer",
				Feedback:   "wrong file path, use /tmp instead",
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := store.WaitForResolution(ctx, approval.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(hitl.StatusRejected))
			Expect(result.Feedback).To(Equal("wrong file path, use /tmp instead"))
			Expect(result.ResolvedBy).To(Equal("reviewer"))
		})

		It("should handle empty feedback gracefully", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "execute_code", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusApproved,
				ResolvedBy: "user",
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := store.WaitForResolution(ctx, approval.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(hitl.StatusApproved))
			Expect(result.Feedback).To(BeEmpty())
		})

		It("should deliver feedback through async WaitForResolution", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "run_shell", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			resultChan := make(chan hitl.ApprovalRequest, 1)
			errChan := make(chan error, 1)

			go func() {
				res, err := store.WaitForResolution(ctx, approval.ID)
				errChan <- err
				resultChan <- res
			}()

			time.Sleep(50 * time.Millisecond)
			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusApproved,
				ResolvedBy: "tester",
				Feedback:   "add error handling",
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(errChan, 2*time.Second).Should(Receive(BeNil()))
			Eventually(resultChan, 2*time.Second).Should(Receive(Satisfy(func(r hitl.ApprovalRequest) bool {
				return r.Status == hitl.StatusApproved &&
					r.Feedback == "add error handling" &&
					r.ResolvedBy == "tester"
			})))
		})
	})

	Describe("CreatedBy", func() {
		It("should persist CreatedBy when creating an approval", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID:  "t1",
				RunID:     "r1",
				ToolName:  "write_file",
				Args:      `{"path":"test.txt"}`,
				CreatedBy: "alice@example.com",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(approval.CreatedBy).To(Equal("alice@example.com"))

			// Verify it round-trips through Get
			fetched, err := store.Get(ctx, approval.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(fetched.CreatedBy).To(Equal("alice@example.com"))
		})

		It("should default to empty CreatedBy for backward compatibility", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "write_file", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(approval.CreatedBy).To(BeEmpty())
		})

		It("should preserve CreatedBy through WaitForResolution", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID:  "t1",
				RunID:     "r1",
				ToolName:  "deploy",
				Args:      "{}",
				CreatedBy: "bob@example.com",
			})
			Expect(err).NotTo(HaveOccurred())

			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusApproved,
				ResolvedBy: "bob@example.com",
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := store.WaitForResolution(ctx, approval.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.CreatedBy).To(Equal("bob@example.com"))
		})
	})

	Describe("CanResolve", func() {
		It("should allow creator to resolve their own approval", func() {
			approval := hitl.ApprovalRequest{CreatedBy: "alice@example.com"}
			Expect(hitl.CanResolve(approval, "alice@example.com", "user")).To(BeTrue())
		})

		It("should allow admin to resolve any approval", func() {
			approval := hitl.ApprovalRequest{CreatedBy: "alice@example.com"}
			Expect(hitl.CanResolve(approval, "admin@example.com", "admin")).To(BeTrue())
		})

		It("should deny non-creator non-admin from resolving", func() {
			approval := hitl.ApprovalRequest{CreatedBy: "alice@example.com"}
			Expect(hitl.CanResolve(approval, "eve@example.com", "user")).To(BeFalse())
		})

		It("should allow anyone to resolve legacy approvals without CreatedBy", func() {
			approval := hitl.ApprovalRequest{CreatedBy: ""}
			Expect(hitl.CanResolve(approval, "anyone@example.com", "user")).To(BeTrue())
		})

		It("should allow demo user to resolve legacy approvals", func() {
			approval := hitl.ApprovalRequest{CreatedBy: ""}
			Expect(hitl.CanResolve(approval, "demo-user", "demo")).To(BeTrue())
		})

		It("should deny agent role from resolving other user's approval", func() {
			approval := hitl.ApprovalRequest{CreatedBy: "alice@example.com"}
			Expect(hitl.CanResolve(approval, "agent-bot", "agent")).To(BeFalse())
		})
	})
})
