package hitl_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/hitl"
)

var _ = Describe("DefaultConfig", func() {
	It("should return a config with empty allowed and denied lists", func() {
		cfg := hitl.DefaultConfig()
		Expect(cfg.AlwaysAllowed).To(BeEmpty())
		Expect(cfg.DeniedTools).To(BeEmpty())
	})
})

var _ = Describe("Config.IsAllowed", func() {
	It("should allow exact match (case-insensitive)", func() {
		cfg := hitl.Config{AlwaysAllowed: []string{"web_search"}}
		Expect(cfg.IsAllowed("web_search")).To(BeTrue())
		Expect(cfg.IsAllowed("WEB_SEARCH")).To(BeTrue())
	})

	It("should allow wildcard prefix match", func() {
		cfg := hitl.Config{AlwaysAllowed: []string{"browser_*"}}
		Expect(cfg.IsAllowed("browser_navigate")).To(BeTrue())
		Expect(cfg.IsAllowed("browser_read_text")).To(BeTrue())
		Expect(cfg.IsAllowed("write_file")).To(BeFalse())
	})

	It("should allow all tools with * wildcard", func() {
		cfg := hitl.Config{AlwaysAllowed: []string{"*"}}
		Expect(cfg.IsAllowed("any_tool")).To(BeTrue())
		Expect(cfg.IsAllowed("dangerous_tool")).To(BeTrue())
	})

	It("should reject tools not in the allowed list", func() {
		cfg := hitl.Config{AlwaysAllowed: []string{"read_file"}}
		Expect(cfg.IsAllowed("write_file")).To(BeFalse())
	})

	It("should allow default read-only tools without config", func() {
		cfg := hitl.DefaultConfig()
		Expect(cfg.IsAllowed("read_file")).To(BeTrue())
		Expect(cfg.IsAllowed("web_search")).To(BeTrue())
		Expect(cfg.IsAllowed("memory_search")).To(BeTrue())
	})

	It("should require approval for mutating tools by default", func() {
		cfg := hitl.DefaultConfig()
		Expect(cfg.IsAllowed("run_shell")).To(BeFalse())
		Expect(cfg.IsAllowed("save_file")).To(BeFalse())
	})
})

var _ = Describe("Config.IsDenied", func() {
	It("should deny exact match (case-insensitive)", func() {
		cfg := hitl.Config{DeniedTools: []string{"bash"}}
		Expect(cfg.IsDenied("bash")).To(BeTrue())
		Expect(cfg.IsDenied("BASH")).To(BeTrue())
	})

	It("should deny wildcard prefix match", func() {
		cfg := hitl.Config{DeniedTools: []string{"shell_*"}}
		Expect(cfg.IsDenied("shell_exec")).To(BeTrue())
		Expect(cfg.IsDenied("shell_run")).To(BeTrue())
		Expect(cfg.IsDenied("web_search")).To(BeFalse())
	})

	It("should deny all tools with * wildcard", func() {
		cfg := hitl.Config{DeniedTools: []string{"*"}}
		Expect(cfg.IsDenied("any_tool")).To(BeTrue())
	})

	It("should not deny tools not in the denied list", func() {
		cfg := hitl.Config{DeniedTools: []string{"bash"}}
		Expect(cfg.IsDenied("read_file")).To(BeFalse())
	})

	It("should handle empty denied list", func() {
		cfg := hitl.Config{DeniedTools: []string{}}
		Expect(cfg.IsDenied("bash")).To(BeFalse())
	})
})

var _ = Describe("ApprovalStatus.String", func() {
	It("should return the string representation", func() {
		Expect(hitl.StatusPending.String()).To(Equal("pending"))
		Expect(hitl.StatusApproved.String()).To(Equal("approved"))
		Expect(hitl.StatusRejected.String()).To(Equal("rejected"))
	})
})
