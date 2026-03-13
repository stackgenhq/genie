// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package createskill_test

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/skills"
	"github.com/stackgenhq/genie/pkg/tools/skills/createskill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestCreateSkill(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CreateSkill Suite")
}

var _ = Describe("SkillManagementTools", func() {
	var (
		baseDir string
		repo    *skills.MutableRepository
	)

	BeforeEach(func() {
		var err error
		baseDir, err = os.MkdirTemp("", "skill-mgmt-*")
		Expect(err).NotTo(HaveOccurred())
		repo, err = skills.NewMutableRepository(baseDir, 10)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(baseDir)
	})

	callTool := func(t tool.Tool, args string) (any, error) {
		ct, ok := t.(tool.CallableTool)
		Expect(ok).To(BeTrue())
		return ct.Call(context.Background(), []byte(args))
	}

	Describe("create_skill", func() {
		var createTool tool.Tool

		BeforeEach(func() {
			createTool = createskill.NewCreateSkillTool(repo)
			Expect(createTool.Declaration().Name).To(Equal("create_skill"))
		})

		It("creates a basic skill", func() {
			res, err := callTool(createTool, `{
				"name": "test-skill",
				"description": "A test skill",
				"instructions": "# Steps\n1. Do things"
			}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("Successfully created"))
			Expect(repo.Count()).To(Equal(1))
		})

		It("creates a skill with scripts", func() {
			res, err := callTool(createTool, `{
				"name": "scripted",
				"description": "Has scripts",
				"instructions": "Run analyze.py",
				"scripts": {"analyze.py": "print('hello')"}
			}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("Scripts: 1"))
		})

		It("creates a skill with docs", func() {
			b64 := base64.StdEncoding.EncodeToString([]byte("fake-pdf"))
			res, err := callTool(createTool, `{
				"name": "doc-skill",
				"description": "Has docs",
				"instructions": "See guidelines",
				"docs": {"guidelines.pdf": "`+b64+`"}
			}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("Reference docs: 1"))
		})

		It("returns error for duplicate", func() {
			_, err := callTool(createTool, `{"name":"dup","description":"a","instructions":"b"}`)
			Expect(err).NotTo(HaveOccurred())
			_, err = callTool(createTool, `{"name":"dup","description":"a","instructions":"b"}`)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("already exists"))
		})
	})

	Describe("update_skill", func() {
		var updateTool tool.Tool

		BeforeEach(func() {
			updateTool = createskill.NewUpdateSkillTool(repo)
			Expect(updateTool.Declaration().Name).To(Equal("update_skill"))
			// Pre-create a skill to update
			Expect(repo.Add(skills.AddSkillRequest{
				Name: "updatable", Description: "v1", Instructions: "old",
			})).To(Succeed())
		})

		It("updates an existing skill", func() {
			res, err := callTool(updateTool, `{
				"name": "updatable",
				"description": "v2",
				"instructions": "new instructions"
			}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("Successfully updated"))
			Expect(res.(string)).To(ContainSubstring("updatable"))

			s, err := repo.Get("updatable")
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Summary.Description).To(Equal("v2"))
		})

		It("returns error for non-existent skill", func() {
			_, err := callTool(updateTool, `{
				"name": "ghost",
				"description": "x",
				"instructions": "x"
			}`)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})
	})

	Describe("delete_skill", func() {
		var deleteTool tool.Tool

		BeforeEach(func() {
			deleteTool = createskill.NewDeleteSkillTool(repo)
			Expect(deleteTool.Declaration().Name).To(Equal("delete_skill"))
			// Pre-create a skill to delete
			Expect(repo.Add(skills.AddSkillRequest{
				Name: "deletable", Description: "bye", Instructions: "gone",
			})).To(Succeed())
		})

		It("deletes an existing skill", func() {
			res, err := callTool(deleteTool, `{"name": "deletable"}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("Successfully deleted"))
			Expect(repo.Count()).To(Equal(0))
		})

		It("returns error for non-existent skill", func() {
			_, err := callTool(deleteTool, `{"name": "ghost"}`)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})
	})
})
