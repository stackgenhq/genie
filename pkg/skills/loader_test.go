package skills_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/appcd-dev/genie/pkg/config"
	"github.com/appcd-dev/genie/pkg/skills"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLoader(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Skills Loader Suite")
}

var _ = Describe("LoadSkillsFromConfig", func() {
	var (
		testDataDir string
		skillsDir   string
	)

	BeforeEach(func() {
		// Get absolute path to testdata
		cwd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		testDataDir = filepath.Join(cwd, "testdata")
		skillsDir = filepath.Join(testDataDir, "skills")
	})

	Context("with valid skills path", func() {
		It("should load skills and create tools", func() {
			cfg := config.GenieConfig{
				SkillsRoots: []string{skillsDir},
			}

			tools, err := skills.LoadSkillsFromConfig(context.Background(), cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).NotTo(BeEmpty())
			Expect(len(tools)).To(Equal(3)) // list_skills, skill_load, skill_run

			// Verify tool names
			toolNames := make(map[string]bool)
			for _, tool := range tools {
				decl := tool.Declaration()
				toolNames[decl.Name] = true
			}
			Expect(toolNames).To(HaveKey("list_skills"))
			Expect(toolNames).To(HaveKey("skill_load"))
			Expect(toolNames).To(HaveKey("skill_run"))
		})
	})

	Context("with empty skills path", func() {
		It("should return nil tools without error", func() {
			cfg := config.GenieConfig{
				SkillsRoots: []string{},
			}

			tools, err := skills.LoadSkillsFromConfig(context.Background(), cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).To(BeNil())
		})
	})

	Context("with non-existent skills path", func() {
		It("should return nil tools without error", func() {
			cfg := config.GenieConfig{
				SkillsRoots: []string{"/nonexistent/path"},
			}

			tools, err := skills.LoadSkillsFromConfig(context.Background(), cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).To(BeNil())
		})
	})

	Context("with file instead of directory", func() {
		It("should skip invalid path and return nil tools", func() {
			// Create a temporary file
			tempFile := filepath.Join(GinkgoT().TempDir(), "notadir")
			err := os.WriteFile(tempFile, []byte("test"), 0644)
			Expect(err).NotTo(HaveOccurred())

			cfg := config.GenieConfig{
				SkillsRoots: []string{tempFile},
			}

			tools, err := skills.LoadSkillsFromConfig(context.Background(), cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).To(BeNil()) // Invalid paths are skipped gracefully
		})
	})

	Context("with environment variable expansion", func() {
		It("should expand environment variables in path", func() {
			// Set a test environment variable
			testEnvVar := "TEST_SKILLS_PATH"
			os.Setenv(testEnvVar, skillsDir)
			defer os.Unsetenv(testEnvVar)

			cfg := config.GenieConfig{
				SkillsRoots: []string{"${" + testEnvVar + "}"},
			}

			tools, err := skills.LoadSkillsFromConfig(context.Background(), cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).NotTo(BeEmpty())
		})
	})
})
