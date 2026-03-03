package tools_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/tools"
)

var _ = Describe("Providers", func() {
	// --- Tools ([]tool.Tool adapter) ---
	Describe("Tools", func() {
		It("returns the underlying slice unchanged", func() {
			// Arrange
			inner := tools.Tools(nil)

			// Act
			got := inner.GetTools()

			// Assert
			Expect(got).To(BeNil())
		})
	})

	// --- FileToolProvider ---
	Describe("FileToolProvider", func() {
		Context("when the working directory is valid", func() {
			It("returns a non-nil provider with at least one tool", func(ctx context.Context) {
				// Arrange
				dir, err := os.MkdirTemp("", "filetool-*")
				Expect(err).NotTo(HaveOccurred())
				defer os.RemoveAll(dir)

				// Act
				provider := tools.NewFileToolProvider(ctx, dir)

				// Assert
				Expect(provider).NotTo(BeNil())
				got := provider.GetTools()
				Expect(got).NotTo(BeEmpty(), "file tools should be populated for a valid directory")
			})
		})

		Context("when GetTools is called", func() {
			It("returns the same tools each call without re-initialization", func(ctx context.Context) {
				// Arrange
				dir, err := os.MkdirTemp("", "filetool-stable-*")
				Expect(err).NotTo(HaveOccurred())
				defer os.RemoveAll(dir)
				provider := tools.NewFileToolProvider(ctx, dir)
				Expect(provider).NotTo(BeNil())

				// Act
				first := provider.GetTools()
				second := provider.GetTools()

				// Assert
				Expect(first).To(HaveLen(len(second)))
			})
		})
	})

	// --- ShellToolProvider ---
	Describe("ShellToolProvider", func() {
		It("returns exactly one tool", func() {
			// Arrange
			provider := tools.NewShellToolProvider("/tmp")

			// Act
			got := provider.GetTools()

			// Assert
			Expect(got).To(HaveLen(1))
		})

		It("returns a tool named run_shell", func() {
			// Arrange
			provider := tools.NewShellToolProvider("/tmp")

			// Act
			got := provider.GetTools()

			// Assert
			Expect(got[0].Declaration().Name).To(Equal("run_shell"))
		})
	})

	// --- PensieveToolProvider ---
	Describe("PensieveToolProvider", func() {
		It("returns at least one context management tool", func() {
			// Arrange
			provider := tools.NewPensieveToolProvider()

			// Act
			got := provider.GetTools()

			// Assert
			Expect(got).NotTo(BeEmpty())
		})

		It("includes the check_budget tool", func() {
			// Arrange
			provider := tools.NewPensieveToolProvider()

			// Act
			got := provider.GetTools()

			// Assert — check_budget is one of the Pensieve tools
			names := make([]string, 0, len(got))
			for _, t := range got {
				names = append(names, t.Declaration().Name)
			}
			Expect(names).To(ContainElement("check_budget"))
		})
	})

	// --- SkillToolProvider ---
	Describe("SkillToolProvider", func() {
		Context("when the skill root does not exist", func() {
			It("creates a provider with no discoverable skills", func() {
				// Arrange — FSRepository does not error on missing directories;
				// it simply yields an empty repo.
				bogusPath := "/tmp/nonexistent-skill-root-abc123"

				// Act
				provider, err := tools.NewSkillToolProvider("/tmp", 3, bogusPath)

				// Assert — no error; tools are still returned (meta-tools exist)
				// but skill_list_docs will yield an empty list at runtime.
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).NotTo(BeNil())
				Expect(provider.GetTools()).NotTo(BeEmpty())
			})
		})

		Context("when the skill root is a valid directory", func() {
			var (
				skillDir string
			)

			BeforeEach(func() {
				// Create a minimal skill directory structure expected by FSRepository.
				var err error
				skillDir, err = os.MkdirTemp("", "skill-root-*")
				Expect(err).NotTo(HaveOccurred())

				// FSRepository expects at least one valid skill directory with a
				// SKILL.md file inside it.
				subSkill := filepath.Join(skillDir, "test_skill")
				Expect(os.MkdirAll(subSkill, 0o755)).To(Succeed())
				Expect(os.WriteFile(
					filepath.Join(subSkill, "SKILL.md"),
					[]byte("---\nname: test_skill\ndescription: a test skill\n---\nHello"),
					0o644,
				)).To(Succeed())
			})

			AfterEach(func() {
				os.RemoveAll(skillDir)
			})

			It("creates a provider successfully", func() {
				// Act
				provider, err := tools.NewSkillToolProvider("/tmp", 3, skillDir)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).NotTo(BeNil())
			})

			It("returns exactly three skill tools", func() {
				// Arrange
				provider, err := tools.NewSkillToolProvider("/tmp", 3, skillDir)
				Expect(err).NotTo(HaveOccurred())

				// Act
				got := provider.GetTools()

				// Assert — discover_skills, load_skill, unload_skill
				Expect(got).To(HaveLen(3))
			})

			It("includes the expected skill tool names", func() {
				// Arrange
				provider, err := tools.NewSkillToolProvider("/tmp", 3, skillDir)
				Expect(err).NotTo(HaveOccurred())

				// Act
				got := provider.GetTools()

				// Assert
				names := make([]string, 0, len(got))
				for _, t := range got {
					names = append(names, t.Declaration().Name)
				}
				Expect(names).To(ConsistOf(
					"discover_skills",
					"load_skill",
					"unload_skill",
				))
			})
		})
	})
})
