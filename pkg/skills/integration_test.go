package skills_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/skill"
)

var _ = Describe("trpc-agent-go skill.FSRepository Integration", func() {
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

	Describe("Using trpc-agent-go skill package", func() {
		It("should create repository and discover skills", func() {
			repo, err := skill.NewFSRepository(skillsDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(repo).NotTo(BeNil())

			summaries := repo.Summaries()
			Expect(summaries).NotTo(BeEmpty())
			Expect(len(summaries)).To(BeNumerically(">=", 1))

			// Check that example-skill is present
			found := false
			for _, s := range summaries {
				if s.Name == "example-skill" {
					found = true
					Expect(s.Description).To(ContainSubstring("example skill"))
					break
				}
			}
			Expect(found).To(BeTrue(), "example-skill should be in summaries")
		})

		It("should load full skill content", func() {
			repo, err := skill.NewFSRepository(skillsDir)
			Expect(err).NotTo(HaveOccurred())

			skillData, err := repo.Get("example-skill")
			Expect(err).NotTo(HaveOccurred())
			Expect(skillData).NotTo(BeNil())
			Expect(skillData.Summary.Name).To(Equal("example-skill"))
			Expect(skillData.Summary.Description).NotTo(BeEmpty())
			Expect(skillData.Body).NotTo(BeEmpty())
			Expect(skillData.Body).To(ContainSubstring("Example Skill"))
		})

		It("should get skill path", func() {
			repo, err := skill.NewFSRepository(skillsDir)
			Expect(err).NotTo(HaveOccurred())

			path, err := repo.Path("example-skill")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).NotTo(BeEmpty())
			Expect(path).To(ContainSubstring("example-skill"))

			// Verify path exists
			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.IsDir()).To(BeTrue())
		})
	})
})
