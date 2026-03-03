package dynamicskills_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/skills/dynamicskills"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// mockRegistry implements SkillRegistry for testing.
type mockRegistry struct {
	skills map[string]dynamicskills.Skill
}

func (m *mockRegistry) Search(query string) []dynamicskills.Skill {
	var results []dynamicskills.Skill
	for _, s := range m.skills {
		if query == "" || s.Name == query || s.Description == query {
			results = append(results, s)
		}
	}
	return results
}

func (m *mockRegistry) Get(name string) (dynamicskills.Skill, bool) {
	s, ok := m.skills[name]
	return s, ok
}

// dummyTool creates a simple tool for testing.
func dummyTool(name string) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, input struct{}) (string, error) {
			return name + " executed", nil
		},
		function.WithName(name),
		function.WithDescription("Dummy tool "+name),
	)
}

var _ = Describe("DynamicSkillLoader", func() {
	var (
		reg    *mockRegistry
		loader *dynamicskills.DynamicSkillLoader
	)

	BeforeEach(func() {
		reg = &mockRegistry{
			skills: map[string]dynamicskills.Skill{
				"skillA": {
					Name:        "skillA",
					Description: "Skill A desc",
					Tools:       []tool.Tool{dummyTool("toolA1"), dummyTool("toolA2")},
				},
				"skillB": {
					Name:        "skillB",
					Description: "Skill B desc",
					Tools:       []tool.Tool{dummyTool("toolB1")},
				},
				"skillC": {
					Name:        "skillC",
					Description: "Skill C desc",
					Tools:       []tool.Tool{dummyTool("toolC1")},
				},
			},
		}
		loader = dynamicskills.NewDynamicSkillLoader(reg, 2)
	})

	It("should initialize correctly", func() {
		Expect(loader).NotTo(BeNil())
		Expect(loader.Registry()).To(Equal(reg))
		Expect(loader.GetLoadedTools()).To(BeEmpty())
	})

	It("should load a valid skill", func() {
		err := loader.LoadSkill("skillA")
		Expect(err).NotTo(HaveOccurred())

		tools := loader.GetLoadedTools()
		Expect(tools).To(HaveLen(2))
		Expect(tools[0].Declaration().Name).To(Equal("toolA1"))
	})

	It("should return an error when loading a duplicate skill", func() {
		err := loader.LoadSkill("skillA")
		Expect(err).NotTo(HaveOccurred())

		err = loader.LoadSkill("skillA")
		Expect(err).To(MatchError(ContainSubstring("already loaded")))
	})

	It("should return an error when loading an invalid skill", func() {
		err := loader.LoadSkill("skillX")
		Expect(err).To(MatchError(ContainSubstring("not found in registry")))
	})

	It("should enforce max active skill capacity", func() {
		err := loader.LoadSkill("skillA")
		Expect(err).NotTo(HaveOccurred())

		err = loader.LoadSkill("skillB")
		Expect(err).NotTo(HaveOccurred())

		err = loader.LoadSkill("skillC")
		Expect(err).To(MatchError(ContainSubstring("max active skills")))
	})

	It("should allow unloading skills and freeing capacity", func() {
		err := loader.LoadSkill("skillA")
		Expect(err).NotTo(HaveOccurred())
		err = loader.LoadSkill("skillB")
		Expect(err).NotTo(HaveOccurred())

		// Unload valid and reload
		err = loader.UnloadSkill("skillA")
		Expect(err).NotTo(HaveOccurred())

		tools := loader.GetLoadedTools()
		Expect(tools).To(HaveLen(1)) // skillB's 1 tool left
		Expect(tools[0].Declaration().Name).To(Equal("toolB1"))

		err = loader.LoadSkill("skillC")
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return an error when unloading a missing skill", func() {
		err := loader.UnloadSkill("skillC")
		Expect(err).To(MatchError(ContainSubstring("not currently loaded")))
	})
})
