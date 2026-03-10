// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dynamicskills_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"testing"

	"github.com/stackgenhq/genie/pkg/tools/skills/dynamicskills"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

func TestDynamicSkills(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DynamicSkills Suite")
}

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

	It("should return MaxSkills", func() {
		Expect(loader.MaxSkills()).To(Equal(2))
	})

	It("should correctly identify loaded skills with IsLoaded", func() {
		err := loader.LoadSkill("skillA")
		Expect(err).NotTo(HaveOccurred())
		Expect(loader.IsLoaded("skillA")).To(BeTrue())
		Expect(loader.IsLoaded("skillB")).To(BeFalse())
	})

	Describe("Tools", func() {
		It("DiscoverSkillsTool should search registry", func() {
			tl := dynamicskills.DiscoverSkillsTool(reg)
			callable := tl.(tool.CallableTool)

			// Empty query matches all
			res, err := callable.Call(context.Background(), []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("skillA"))
			Expect(res.(string)).To(ContainSubstring("skillB"))
			Expect(res.(string)).To(ContainSubstring("skillC"))

			// Query "skillA" matches only skillA
			res, err = callable.Call(context.Background(), []byte(`{"query": "skillA"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("skillA"))
			Expect(res.(string)).NotTo(ContainSubstring("skillB"))

			// Query "xyz" matches none
			res, err = callable.Call(context.Background(), []byte(`{"query": "xyz"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("No skills found"))
		})

		It("LoadSkillTool should load a skill", func() {
			tl := dynamicskills.LoadSkillTool(loader)
			callable := tl.(tool.CallableTool)

			res, err := callable.Call(context.Background(), []byte(`{"name": "skillA"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("Successfully loaded skill \"skillA\""))
			Expect(loader.IsLoaded("skillA")).To(BeTrue())

			// Invalid skill
			_, err = callable.Call(context.Background(), []byte(`{"name": "skillX"}`))
			Expect(err).To(HaveOccurred())
			Expect(loader.IsLoaded("skillX")).To(BeFalse())
		})

		It("UnloadSkillTool should unload a skill", func() {
			Expect(loader.LoadSkill("skillA")).To(Succeed())

			tl := dynamicskills.UnloadSkillTool(loader)
			callable := tl.(tool.CallableTool)

			res, err := callable.Call(context.Background(), []byte(`{"name": "skillA"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("Successfully unloaded skill \"skillA\""))
			Expect(loader.IsLoaded("skillA")).To(BeFalse())

			// Invalid skill
			_, err = callable.Call(context.Background(), []byte(`{"name": "skillX"}`))
			Expect(err).To(HaveOccurred())
		})
	})
})
