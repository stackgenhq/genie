// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package skills_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/skills"
	"github.com/stackgenhq/genie/pkg/skills/skillsfakes"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// fakeRepository implements skill.Repository for testing.
type fakeRepository struct {
	summaries []skill.Summary
	skills    map[string]*skill.Skill
	paths     map[string]string
	getErr    error
	pathErr   error
}

func (r *fakeRepository) Summaries() []skill.Summary {
	return r.summaries
}

func (r *fakeRepository) Get(name string) (*skill.Skill, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if s, ok := r.skills[name]; ok {
		return s, nil
	}
	return nil, errors.New("skill not found: " + name)
}

func (r *fakeRepository) Path(name string) (string, error) {
	if r.pathErr != nil {
		return "", r.pathErr
	}
	if p, ok := r.paths[name]; ok {
		return p, nil
	}
	return "", errors.New("skill path not found: " + name)
}

var _ = Describe("Skill Tools", func() {
	Describe("NewSkillLoadTool", func() {
		It("should create a skill_load tool with correct declaration", func() {
			repo := &fakeRepository{}
			t := skills.NewSkillLoadTool(repo)
			Expect(t.Declaration().Name).To(Equal("skill_load"))
		})
	})

	Describe("SkillLoadTool.execute", func() {
		It("should load skill instructions successfully", func(ctx context.Context) {
			repo := &fakeRepository{
				skills: map[string]*skill.Skill{
					"test-skill": {
						Summary: skill.Summary{
							Name:        "test-skill",
							Description: "A test skill",
						},
						Body: "## Instructions\nDo the thing.",
						Docs: []skill.Doc{
							{Path: "README.md", Content: "# README"},
						},
					},
				},
			}
			t := skills.NewSkillLoadTool(repo)
			ct := t.(tool.CallableTool)

			result, err := ct.Call(ctx, []byte(`{"skill_name":"test-skill"}`))
			Expect(err).NotTo(HaveOccurred())

			// Marshal and check
			data, err := json.Marshal(result)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("test-skill"))
			Expect(string(data)).To(ContainSubstring("Instructions"))
		})

		It("should return error response for missing skill", func(ctx context.Context) {
			repo := &fakeRepository{
				skills: map[string]*skill.Skill{},
			}
			t := skills.NewSkillLoadTool(repo)
			ct := t.(tool.CallableTool)

			result, err := ct.Call(ctx, []byte(`{"skill_name":"nonexistent"}`))
			Expect(err).NotTo(HaveOccurred()) // returns error in response, not Go error

			data, err := json.Marshal(result)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("Failed to load skill"))
		})
	})

	Describe("NewSkillRunTool", func() {
		It("should create a skill_run tool with correct declaration", func() {
			repo := &fakeRepository{}
			executor := &skillsfakes.FakeExecutor{}
			t := skills.NewSkillRunTool(repo, executor)
			Expect(t.Declaration().Name).To(Equal("skill_run"))
		})
	})

	Describe("SkillRunTool.execute", func() {
		It("should execute skill successfully", func(ctx context.Context) {
			repo := &fakeRepository{
				paths: map[string]string{
					"test-skill": "/tmp/test-skill",
				},
			}
			executor := &skillsfakes.FakeExecutor{}
			executor.ExecuteReturns(skills.ExecuteResponse{
				Output:   "script executed",
				ExitCode: 0,
			}, nil)

			t := skills.NewSkillRunTool(repo, executor)
			ct := t.(tool.CallableTool)

			result, err := ct.Call(ctx, []byte(`{"skill_name":"test-skill","script_path":"scripts/run.sh"}`))
			Expect(err).NotTo(HaveOccurred())

			data, err := json.Marshal(result)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("script executed"))

			Expect(executor.ExecuteCallCount()).To(Equal(1))
		})

		It("should return error response when skill not found", func(ctx context.Context) {
			repo := &fakeRepository{
				paths: map[string]string{},
			}
			executor := &skillsfakes.FakeExecutor{}

			t := skills.NewSkillRunTool(repo, executor)
			ct := t.(tool.CallableTool)

			result, err := ct.Call(ctx, []byte(`{"skill_name":"nonexistent","script_path":"scripts/run.sh"}`))
			Expect(err).NotTo(HaveOccurred()) // error in response, not Go error

			data, err := json.Marshal(result)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("Failed to find skill"))
		})

		It("should return error response when executor fails", func(ctx context.Context) {
			repo := &fakeRepository{
				paths: map[string]string{
					"test-skill": "/tmp/test-skill",
				},
			}
			executor := &skillsfakes.FakeExecutor{}
			executor.ExecuteReturns(skills.ExecuteResponse{}, errors.New("execution failed"))

			t := skills.NewSkillRunTool(repo, executor)
			ct := t.(tool.CallableTool)

			result, err := ct.Call(ctx, []byte(`{"skill_name":"test-skill","script_path":"scripts/run.sh"}`))
			Expect(err).NotTo(HaveOccurred())

			data, err := json.Marshal(result)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("Failed to execute skill"))
		})
	})

	Describe("CreateSkillTools", func() {
		It("should create both skill_load and skill_run tools", func() {
			repo := &fakeRepository{}
			executor := &skillsfakes.FakeExecutor{}
			tools := skills.CreateSkillTools(repo, executor)
			Expect(tools).To(HaveLen(2))
			Expect(tools[0].Declaration().Name).To(Equal("skill_load"))
			Expect(tools[1].Declaration().Name).To(Equal("skill_run"))
		})
	})

	Describe("SkillLoadResponse.MarshalJSON", func() {
		It("should marshal to valid JSON", func() {
			resp := skills.SkillLoadResponse{
				Name:         "test",
				Description:  "A test skill",
				Instructions: "Do this",
				Documents:    []string{"doc1", "doc2"},
			}
			data, err := resp.MarshalJSON()
			Expect(err).NotTo(HaveOccurred())

			var result map[string]interface{}
			err = json.Unmarshal(data, &result)
			Expect(err).NotTo(HaveOccurred())
			Expect(result["name"]).To(Equal("test"))
			Expect(result["description"]).To(Equal("A test skill"))
		})
	})

	Describe("SkillRunResponse.MarshalJSON", func() {
		It("should marshal to valid JSON", func() {
			resp := skills.SkillRunResponse{
				Output:   "output text",
				ExitCode: 0,
			}
			data, err := resp.MarshalJSON()
			Expect(err).NotTo(HaveOccurred())

			var result map[string]interface{}
			err = json.Unmarshal(data, &result)
			Expect(err).NotTo(HaveOccurred())
			Expect(result["output"]).To(Equal("output text"))
		})
	})
})

var _ = Describe("determineInterpreter", func() {
	It("should return python3 for .py files", func() {
		interp, args := skills.DetermineInterpreterForTest("script.py")
		Expect(interp).To(Equal("python3"))
		Expect(args).To(BeNil())
	})

	It("should return bash for .sh files", func() {
		interp, args := skills.DetermineInterpreterForTest("script.sh")
		Expect(interp).To(Equal("bash"))
		Expect(args).To(BeNil())
	})

	It("should return node for .js files", func() {
		interp, args := skills.DetermineInterpreterForTest("script.js")
		Expect(interp).To(Equal("node"))
		Expect(args).To(BeNil())
	})

	It("should return ruby for .rb files", func() {
		interp, args := skills.DetermineInterpreterForTest("script.rb")
		Expect(interp).To(Equal("ruby"))
		Expect(args).To(BeNil())
	})

	It("should return the script path itself for unknown extensions", func() {
		// Create a temp file to test chmod
		tmpFile := filepath.Join(GinkgoT().TempDir(), "custom_script")
		err := os.WriteFile(tmpFile, []byte("#!/bin/sh\necho hi"), 0644)
		Expect(err).NotTo(HaveOccurred())

		interp, args := skills.DetermineInterpreterForTest(tmpFile)
		Expect(interp).To(Equal(tmpFile))
		Expect(args).To(BeNil())
	})

	It("should handle uppercase extensions", func() {
		interp, _ := skills.DetermineInterpreterForTest("script.PY")
		Expect(interp).To(Equal("python3"))
	})
})
