package tools_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/tools/unix"
	"trpc.group/trpc-go/trpc-agent-go/tool"
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
			provider := tools.NewShellToolProvider("/tmp", unix.ShellToolConfig{})

			// Act
			got := provider.GetTools()

			// Assert
			Expect(got).To(HaveLen(1))
		})

		It("returns a tool named run_shell", func() {
			// Arrange
			provider := tools.NewShellToolProvider("/tmp", unix.ShellToolConfig{})

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
				provider, err := tools.NewSkillToolProvider("/tmp", tools.SkillLoadConfig{
					MaxLoadedSkills: 3,
					SkillsRoots:     []string{bogusPath},
				})

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
				provider, err := tools.NewSkillToolProvider("/tmp", tools.SkillLoadConfig{
					MaxLoadedSkills: 3,
					SkillsRoots:     []string{skillDir},
				})

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).NotTo(BeNil())
			})

			It("returns exactly three skill tools", func() {
				// Arrange
				provider, err := tools.NewSkillToolProvider("/tmp", tools.SkillLoadConfig{
					MaxLoadedSkills: 3,
					SkillsRoots:     []string{skillDir},
				})
				Expect(err).NotTo(HaveOccurred())

				// Act
				got := provider.GetTools()

				// Assert — discover_skills, load_skill, unload_skill
				Expect(got).To(HaveLen(3))
			})

			It("includes the expected skill tool names", func() {
				// Arrange
				provider, err := tools.NewSkillToolProvider("/tmp", tools.SkillLoadConfig{
					MaxLoadedSkills: 3,
					SkillsRoots:     []string{skillDir},
				})
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

			It("supports Search to find dynamic skills", func() {
				provider, err := tools.NewSkillToolProvider("/tmp", tools.SkillLoadConfig{
					MaxLoadedSkills: 3,
					SkillsRoots:     []string{skillDir},
				})
				Expect(err).NotTo(HaveOccurred())

				// Empty query
				skills := provider.Search("")
				Expect(skills).To(HaveLen(1))
				Expect(skills[0].Name).To(Equal("test_skill"))

				// Match query
				skills = provider.Search("test")
				Expect(skills).To(HaveLen(1))

				// No match query
				skills = provider.Search("bogus")
				Expect(skills).To(HaveLen(0))
			})

			It("supports Get to retrieve a dynamic skill", func() {
				provider, err := tools.NewSkillToolProvider("/tmp", tools.SkillLoadConfig{
					MaxLoadedSkills: 3,
					SkillsRoots:     []string{skillDir},
				})
				Expect(err).NotTo(HaveOccurred())

				skill, found := provider.Get("test_skill")
				Expect(found).To(BeTrue())
				Expect(skill.Name).To(Equal("test_skill"))
				Expect(skill.Tools).To(HaveLen(1)) // skill_run tool

				_, found = provider.Get("missing_skill")
				Expect(found).To(BeFalse())
			})

			It("restrictedSkillRunTool prevents calling unloaded skills", func() {
				provider, err := tools.NewSkillToolProvider("/tmp", tools.SkillLoadConfig{
					MaxLoadedSkills: 3,
					SkillsRoots:     []string{skillDir},
				})
				Expect(err).NotTo(HaveOccurred())

				skill, found := provider.Get("test_skill")
				Expect(found).To(BeTrue())

				runTool := skill.Tools[0].(tool.CallableTool)

				// By default not loaded
				_, err = runTool.Call(context.Background(), []byte(`{"skill_name":"test_skill","input":""}`))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("is not currently loaded"))

				// We can't automatically test the loader here easily without using LoadSkillTool,
				// but we have unit tests on dynamic_skills loader so it's fine.
			})

			It("Clone returns a fresh provider with empty loader state", func() {
				provider, err := tools.NewSkillToolProvider("/tmp", tools.SkillLoadConfig{
					MaxLoadedSkills: 3,
					SkillsRoots:     []string{skillDir},
				})
				Expect(err).NotTo(HaveOccurred())

				// Verify that SkillToolProvider implements CloneableToolProvider
				var cloneable tools.CloneableToolProvider = provider
				cloned := cloneable.Clone()

				Expect(cloned).NotTo(BeNil())
				Expect(cloned).NotTo(BeIdenticalTo(provider))

				// Cloned provider should return the same base tools (discover_skills, load_skill, unload_skill)
				clonedTools := cloned.GetTools()
				Expect(clonedTools).To(HaveLen(3))
			})
		})
	})
})
