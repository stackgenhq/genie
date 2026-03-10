// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package activityreport_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/audit/auditfakes"
	"github.com/stackgenhq/genie/pkg/memory/vector/vectorfakes"
	"github.com/stackgenhq/genie/pkg/report/activityreport"
)

func setEnv(key, value string) func() {
	old := os.Getenv(key)
	_ = os.Setenv(key, value)
	return func() { _ = os.Setenv(key, old) }
}

var _ = Describe("ReportsDir and ReportPath", func() {
	It("sanitizes agent name and builds path under .genie/reports", func() {
		dir := activityreport.ReportsDir("My Agent")
		Expect(dir).To(HaveSuffix(filepath.Join("reports", "my_agent")))
	})

	It("uses genie when agent name is empty", func() {
		dir := activityreport.ReportsDir("")
		Expect(dir).To(HaveSuffix(filepath.Join("reports", "genie")))
	})

	It("produces report filename with date and report name", func() {
		t := time.Date(2026, 2, 27, 0, 0, 0, 0, time.UTC)
		path := activityreport.ReportPath("agent", "daily", t)
		Expect(path).To(HaveSuffix("20260227_daily.md"))
	})
})

var _ = Describe("Generator", func() {
	var tmpDir string
	var restore func()

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "activityreport-test-*")
		Expect(err).NotTo(HaveOccurred())
		restore = setEnv("HOME", tmpDir)
	})

	AfterEach(func() {
		restore()
		_ = os.RemoveAll(tmpDir)
	})

	It("writes report file when no audit events exist", func(ctx context.Context) {
		at := time.Now()
		fakeAuditor := &auditfakes.FakeAuditor{}
		fakeAuditor.RecentReturns(nil, nil)
		g := activityreport.NewGenerator("test_agent", nil, 24*time.Hour, fakeAuditor)
		err := g.Generate(ctx, "daily", at)
		Expect(err).NotTo(HaveOccurred())

		path := activityreport.ReportPath("test_agent", "daily", at)
		Expect(path).To(BeARegularFile())
		data, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("# Activity Report"))
		Expect(string(data)).To(ContainSubstring("No activity in this window"))
	})

	It("creates reports directory when missing", func(ctx context.Context) {
		fakeAuditor := &auditfakes.FakeAuditor{}
		fakeAuditor.RecentReturns(nil, nil)
		g := activityreport.NewGenerator("new_agent", nil, 24*time.Hour, fakeAuditor)
		err := g.Generate(ctx, "report", time.Now())
		Expect(err).NotTo(HaveOccurred())

		dir := activityreport.ReportsDir("new_agent")
		Expect(dir).To(BeADirectory())
	})

	It("appends to file when it already exists", func(ctx context.Context) {
		at := time.Now()
		fakeAuditor := &auditfakes.FakeAuditor{}
		fakeAuditor.RecentReturns(nil, nil)
		g := activityreport.NewGenerator("append_agent", nil, 24*time.Hour, fakeAuditor)
		Expect(g.Generate(ctx, "daily", at)).NotTo(HaveOccurred())
		path := activityreport.ReportPath("append_agent", "daily", at)
		Expect(path).To(BeARegularFile())
		first, _ := os.ReadFile(path)
		Expect(string(first)).To(ContainSubstring("# Activity Report"))

		Expect(g.Generate(ctx, "daily", at)).NotTo(HaveOccurred())
		second, _ := os.ReadFile(path)
		Expect(string(second)).To(ContainSubstring("---"))
		Expect(string(second)).To(ContainSubstring("# Activity Report"))
		// Two report sections (first run + appended run)
		Expect(string(second)).To(HavePrefix(string(first)))
		Expect(len(second)).To(BeNumerically(">", len(first)))
	})

	It("upserts report into vector store with activity_report metadata", func(ctx context.Context) {
		// Arrange
		at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
		fakeAuditor := &auditfakes.FakeAuditor{}
		fakeAuditor.RecentReturns([]audit.Event{
			{EventType: "tool_call", Actor: "agent", Action: "read_file", Timestamp: at},
		}, nil)

		fakeStore := &vectorfakes.FakeIStore{}
		fakeStore.UpsertReturns(nil)

		g := activityreport.NewGenerator("vec_agent", fakeStore, 24*time.Hour, fakeAuditor)

		// Act
		err := g.Generate(ctx, "daily", at)

		// Assert
		Expect(err).NotTo(HaveOccurred())
		Expect(fakeStore.UpsertCallCount()).To(Equal(1))
		_, items := fakeStore.UpsertArgsForCall(0)
		Expect(items).To(HaveLen(1))
		Expect(items[0].ID).To(HavePrefix("activity_report:"))
		Expect(items[0].Metadata).To(HaveKeyWithValue("type", "activity_report"))
		Expect(items[0].Metadata).To(HaveKeyWithValue("source", "activity_report"))
		Expect(items[0].Text).To(ContainSubstring("# Activity Report"))
	})

	It("includes skill reference footer in generated report", func(ctx context.Context) {
		// Arrange
		at := time.Now()
		fakeAuditor := &auditfakes.FakeAuditor{}
		fakeAuditor.RecentReturns([]audit.Event{
			{EventType: "tool_call", Actor: "agent", Action: "read_file", Timestamp: at},
		}, nil)

		fakeStore := &vectorfakes.FakeIStore{}
		fakeStore.UpsertReturns(nil)

		g := activityreport.NewGenerator("footer_agent", fakeStore, 24*time.Hour, fakeAuditor)

		// Act
		err := g.Generate(ctx, "daily", at)

		// Assert
		Expect(err).NotTo(HaveOccurred())
		path := activityreport.ReportPath("footer_agent", "daily", at)
		data, readErr := os.ReadFile(path)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("Generated by Genie activity report"))
		Expect(string(data)).To(ContainSubstring("skill"))
	})

	It("succeeds even when vector store upsert fails", func(ctx context.Context) {
		// Arrange
		at := time.Now()
		fakeAuditor := &auditfakes.FakeAuditor{}
		fakeAuditor.RecentReturns(nil, nil)

		fakeStore := &vectorfakes.FakeIStore{}
		fakeStore.UpsertReturns(fmt.Errorf("upsert failed"))

		g := activityreport.NewGenerator("fail_agent", fakeStore, 24*time.Hour, fakeAuditor)

		// Act
		err := g.Generate(ctx, "daily", at)

		// Assert — Generate should still succeed; file was written
		Expect(err).NotTo(HaveOccurred())
		Expect(fakeStore.UpsertCallCount()).To(Equal(1))
	})
})
