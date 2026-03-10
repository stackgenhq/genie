// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

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

// These tests cover GORMStore methods that were previously at 0% coverage:
// IsAllowed (store level), ReadOnlyTools, ListPending, ExpireStale, RecoverPending.
var _ = Describe("GORMStore extended coverage", func() {
	var (
		ctx    context.Context
		store  hitl.ApprovalStore
		gormDB *gorm.DB
		dbDir  string
	)

	BeforeEach(func() {
		cfg := hitl.Config{
			AlwaysAllowed: []string{"read_file", "list_file"},
		}
		ctx = context.Background()

		var err error
		dbDir, err = os.MkdirTemp("", "hitl-extended-test-*")
		Expect(err).NotTo(HaveOccurred())

		gormDB, err = db.Open(filepath.Join(dbDir, "hitl_ext.db"))
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

	Describe("IsAllowed (store-level)", func() {
		It("delegates to config's IsAllowed", func() {
			Expect(store.IsAllowed("read_file")).To(BeTrue())
			Expect(store.IsAllowed("run_shell")).To(BeFalse())
		})
	})

	Describe("ListPending", func() {
		It("returns empty list when no approvals exist", func() {
			pending, err := store.ListPending(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(BeEmpty())
		})

		It("returns pending approvals that have not expired", func() {
			_, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1",
				RunID:    "r1",
				ToolName: "write_file",
				Args:     `{"path":"/tmp/test"}`,
			})
			Expect(err).NotTo(HaveOccurred())

			pending, err := store.ListPending(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(HaveLen(1))
			Expect(pending[0].ToolName).To(Equal("write_file"))
			Expect(pending[0].Status).To(Equal(hitl.StatusPending))
		})

		It("does not return resolved approvals", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1",
				RunID:    "r1",
				ToolName: "run_shell",
				Args:     `{}`,
			})
			Expect(err).NotTo(HaveOccurred())

			// Resolve it
			err = store.Resolve(ctx, hitl.ResolveRequest{
				ApprovalID: approval.ID,
				Decision:   hitl.StatusApproved,
				ResolvedBy: "admin",
			})
			Expect(err).NotTo(HaveOccurred())

			pending, err := store.ListPending(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(BeEmpty())
		})

		It("returns multiple pending approvals ordered by created_at desc", func() {
			// Create first approval
			_, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1", RunID: "r1", ToolName: "tool_a", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			// Small delay to ensure ordering
			time.Sleep(10 * time.Millisecond)

			// Create second approval
			_, err = store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t2", RunID: "r2", ToolName: "tool_b", Args: "{}",
			})
			Expect(err).NotTo(HaveOccurred())

			pending, err := store.ListPending(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(HaveLen(2))
			// Most recent first
			Expect(pending[0].ToolName).To(Equal("tool_b"))
			Expect(pending[1].ToolName).To(Equal("tool_a"))
		})
	})

	Describe("ExpireStale", func() {
		It("returns 0 when no stale approvals exist", func() {
			count, err := store.ExpireStale(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(0)))
		})

		It("does not expire approvals without expiry time", func() {
			_, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1",
				RunID:    "r1",
				ToolName: "run_shell",
				Args:     `{}`,
			})
			Expect(err).NotTo(HaveOccurred())

			count, err := store.ExpireStale(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(0)))

			// Should still be pending
			pending, err := store.ListPending(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(HaveLen(1))
		})
	})

	Describe("RecoverPending", func() {
		It("returns empty result when no pending approvals exist", func() {
			result, err := store.RecoverPending(ctx, 10*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Expired).To(Equal(0))
			Expect(result.Recovered).To(Equal(0))
		})

		It("recovers recent pending approvals", func() {
			_, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1",
				RunID:    "r1",
				ToolName: "run_shell",
				Args:     `{}`,
				Question: "Can I run this?",
			})
			Expect(err).NotTo(HaveOccurred())

			// Recover with a generous maxAge — approval should be recovered, not expired.
			result, err := store.RecoverPending(ctx, 10*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Recovered).To(Equal(1))
			Expect(result.Expired).To(Equal(0))
			Expect(result.Replayable).To(HaveLen(1))
			Expect(result.Replayable[0].Question).To(Equal("Can I run this?"))
		})

		It("expires old pending approvals", func() {
			_, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1",
				RunID:    "r1",
				ToolName: "run_shell",
				Args:     `{}`,
			})
			Expect(err).NotTo(HaveOccurred())

			// Recover with 0 maxAge — everything should be expired immediately.
			result, err := store.RecoverPending(ctx, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Expired).To(Equal(1))
			Expect(result.Recovered).To(Equal(0))
		})

		It("recovers without replayable when question is empty", func() {
			_, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1",
				RunID:    "r1",
				ToolName: "run_shell",
				Args:     `{}`,
				Question: "", // No question
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := store.RecoverPending(ctx, 10*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Recovered).To(Equal(1))
			Expect(result.Replayable).To(BeEmpty())
		})
	})

	Describe("Get", func() {
		It("returns a created approval by ID", func() {
			approval, err := store.Create(ctx, hitl.CreateRequest{
				ThreadID: "t1",
				RunID:    "r1",
				ToolName: "write_file",
				Args:     `{"path":"/tmp/x"}`,
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := store.Get(ctx, approval.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ToolName).To(Equal("write_file"))
			Expect(got.Status).To(Equal(hitl.StatusPending))
		})

		It("returns error for non-existent ID", func() {
			_, err := store.Get(ctx, "nonexistent-id")
			Expect(err).To(HaveOccurred())
		})
	})
})
