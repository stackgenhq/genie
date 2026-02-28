package activityreport_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/audit/auditfakes"
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
})
