// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package skills_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/skills"
)

var _ = Describe("MutableRepository", func() {
	var (
		baseDir string
		repo    *skills.MutableRepository
	)

	BeforeEach(func() {
		var err error
		baseDir, err = os.MkdirTemp("", "mutable-repo-*")
		Expect(err).NotTo(HaveOccurred())
		repo, err = skills.NewMutableRepository(baseDir, 5)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(baseDir)
	})

	Describe("NewMutableRepository", func() {
		It("creates the base directory if it does not exist", func() {
			newDir := filepath.Join(baseDir, "new-dir")
			r, err := skills.NewMutableRepository(newDir, 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(r).NotTo(BeNil())
			_, statErr := os.Stat(newDir)
			Expect(statErr).NotTo(HaveOccurred())
		})

		It("returns an error when baseDir is empty", func() {
			_, err := skills.NewMutableRepository("", 5)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("base directory is required"))
		})

		It("defaults maxSkills when zero", func() {
			r, err := skills.NewMutableRepository(baseDir, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(r).NotTo(BeNil())
		})

		It("picks up existing skills from a prior run", func() {
			// Pre-create a skill on disk
			skillDir := filepath.Join(baseDir, "existing-skill")
			Expect(os.MkdirAll(skillDir, 0o755)).To(Succeed())
			Expect(os.WriteFile(
				filepath.Join(skillDir, "SKILL.md"),
				[]byte("---\nname: existing-skill\ndescription: pre-existing\n---\nHello"),
				0o644,
			)).To(Succeed())

			// Create a new repo — should discover the existing skill
			r, err := skills.NewMutableRepository(baseDir, 5)
			Expect(err).NotTo(HaveOccurred())
			summaries := r.Summaries()
			Expect(summaries).To(HaveLen(1))
			Expect(summaries[0].Name).To(Equal("existing-skill"))
			Expect(summaries[0].Description).To(Equal("pre-existing"))
		})
	})

	Describe("Add", func() {
		It("creates a skill successfully", func() {
			err := repo.Add(skills.AddSkillRequest{
				Name:         "my-skill",
				Description:  "A test skill",
				Instructions: "# Steps\n1. Do stuff",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(repo.Count()).To(Equal(1))

			// Verify file exists
			skillPath := filepath.Join(baseDir, "my-skill", "SKILL.md")
			data, err := os.ReadFile(skillPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("name: my-skill"))
			Expect(string(data)).To(ContainSubstring("description: A test skill"))
			Expect(string(data)).To(ContainSubstring("# Steps"))
		})

		It("creates scripts with executable permissions", func() {
			err := repo.Add(skills.AddSkillRequest{
				Name:         "scripted-skill",
				Description:  "Has scripts",
				Instructions: "Run the script",
				Scripts: map[string]string{
					"analyze.py": "#!/usr/bin/env python3\nprint('hello')",
					"run.sh":     "#!/bin/bash\necho hello",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Check scripts dir
			pyPath := filepath.Join(baseDir, "scripted-skill", "scripts", "analyze.py")
			info, err := os.Stat(pyPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()&0o100).NotTo(BeZero(), "script should be executable")

			content, err := os.ReadFile(pyPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(ContainSubstring("print('hello')"))
		})

		It("creates docs from base64 content", func() {
			pdfContent := []byte("fake-pdf-content")
			b64 := base64.StdEncoding.EncodeToString(pdfContent)

			err := repo.Add(skills.AddSkillRequest{
				Name:         "doc-skill",
				Description:  "Has docs",
				Instructions: "Refer to the guidelines",
				Docs: map[string]string{
					"guidelines.pdf": b64,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			docPath := filepath.Join(baseDir, "doc-skill", "docs", "guidelines.pdf")
			data, err := os.ReadFile(docPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(Equal(pdfContent))
		})

		It("returns an error for duplicate skill names", func() {
			err := repo.Add(skills.AddSkillRequest{
				Name: "dup-skill", Description: "first", Instructions: "one",
			})
			Expect(err).NotTo(HaveOccurred())

			err = repo.Add(skills.AddSkillRequest{
				Name: "dup-skill", Description: "second", Instructions: "two",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("already exists"))
		})

		It("enforces maximum skill count", func() {
			for i := 0; i < 5; i++ {
				err := repo.Add(skills.AddSkillRequest{
					Name:         strings.ReplaceAll("skill-"+string(rune('a'+i)), "", ""),
					Description:  "test",
					Instructions: "test",
				})
				Expect(err).NotTo(HaveOccurred())
			}

			err := repo.Add(skills.AddSkillRequest{
				Name: "skill-overflow", Description: "test", Instructions: "test",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("maximum number of skills"))
		})

		It("rejects empty name", func() {
			err := repo.Add(skills.AddSkillRequest{
				Name: "", Description: "test", Instructions: "test",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("name is required"))
		})

		It("rejects invalid name characters", func() {
			err := repo.Add(skills.AddSkillRequest{
				Name: "My Skill!", Description: "test", Instructions: "test",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid"))
		})

		It("rejects names exceeding max length", func() {
			longName := strings.Repeat("a", 65)
			err := repo.Add(skills.AddSkillRequest{
				Name: longName, Description: "test", Instructions: "test",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("maximum length"))
		})

		It("rejects empty description", func() {
			err := repo.Add(skills.AddSkillRequest{
				Name: "ok-name", Description: "", Instructions: "test",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("description is required"))
		})

		It("rejects empty instructions", func() {
			err := repo.Add(skills.AddSkillRequest{
				Name: "ok-name", Description: "test", Instructions: "",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("instructions are required"))
		})

		It("rejects script filenames with path traversal", func() {
			err := repo.Add(skills.AddSkillRequest{
				Name: "bad-script", Description: "test", Instructions: "test",
				Scripts: map[string]string{
					"../escape.sh": "evil",
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("path traversal"))
		})

		It("rejects doc filenames with absolute paths", func() {
			err := repo.Add(skills.AddSkillRequest{
				Name: "bad-doc", Description: "test", Instructions: "test",
				Docs: map[string]string{
					"/etc/passwd": base64.StdEncoding.EncodeToString([]byte("nope")),
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("absolute paths"))
		})

		It("rejects invalid base64 in docs", func() {
			err := repo.Add(skills.AddSkillRequest{
				Name: "bad-b64", Description: "test", Instructions: "test",
				Docs: map[string]string{
					"file.pdf": "not-valid-base64!!!",
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("decode doc"))
		})
	})

	Describe("Summaries", func() {
		It("returns sorted summaries", func() {
			Expect(repo.Add(skills.AddSkillRequest{
				Name: "zebra-skill", Description: "z desc", Instructions: "z",
			})).To(Succeed())
			Expect(repo.Add(skills.AddSkillRequest{
				Name: "alpha-skill", Description: "a desc", Instructions: "a",
			})).To(Succeed())

			summaries := repo.Summaries()
			Expect(summaries).To(HaveLen(2))
			Expect(summaries[0].Name).To(Equal("alpha-skill"))
			Expect(summaries[1].Name).To(Equal("zebra-skill"))
		})

		It("returns empty slice when no skills exist", func() {
			Expect(repo.Summaries()).To(BeEmpty())
		})
	})

	Describe("Get", func() {
		It("returns the full skill data", func() {
			Expect(repo.Add(skills.AddSkillRequest{
				Name: "full-skill", Description: "full desc", Instructions: "# Full\nDo things",
			})).To(Succeed())

			s, err := repo.Get("full-skill")
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Summary.Name).To(Equal("full-skill"))
			Expect(s.Summary.Description).To(Equal("full desc"))
			Expect(s.Body).To(ContainSubstring("# Full"))
		})

		It("returns an error for missing skills", func() {
			_, err := repo.Get("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Describe("Path", func() {
		It("returns the skill directory", func() {
			Expect(repo.Add(skills.AddSkillRequest{
				Name: "path-skill", Description: "p", Instructions: "p",
			})).To(Succeed())

			p, err := repo.Path("path-skill")
			Expect(err).NotTo(HaveOccurred())
			Expect(p).To(Equal(filepath.Join(baseDir, "path-skill")))
		})

		It("returns an error for missing skills", func() {
			_, err := repo.Path("nonexistent")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Update", func() {
		BeforeEach(func() {
			Expect(repo.Add(skills.AddSkillRequest{
				Name:         "updatable",
				Description:  "original desc",
				Instructions: "original instructions",
				Scripts: map[string]string{
					"old.py": "print('old')",
				},
			})).To(Succeed())
		})

		It("overwrites an existing skill's content", func() {
			err := repo.Update(skills.AddSkillRequest{
				Name:         "updatable",
				Description:  "new desc",
				Instructions: "new instructions",
				Scripts: map[string]string{
					"new.py": "print('new')",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify new content
			s, err := repo.Get("updatable")
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Summary.Description).To(Equal("new desc"))
			Expect(s.Body).To(ContainSubstring("new instructions"))

			// Old script should be gone
			_, err = os.Stat(filepath.Join(baseDir, "updatable", "scripts", "old.py"))
			Expect(os.IsNotExist(err)).To(BeTrue())

			// New script should exist
			data, err := os.ReadFile(filepath.Join(baseDir, "updatable", "scripts", "new.py"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("print('new')"))
		})

		It("returns an error for non-existent skills", func() {
			err := repo.Update(skills.AddSkillRequest{
				Name: "ghost", Description: "x", Instructions: "x",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})

		It("validates the request the same as Add", func() {
			err := repo.Update(skills.AddSkillRequest{
				Name: "updatable", Description: "", Instructions: "x",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("description is required"))
		})

		It("preserves skill count after update", func() {
			Expect(repo.Count()).To(Equal(1))
			Expect(repo.Update(skills.AddSkillRequest{
				Name: "updatable", Description: "v2", Instructions: "v2",
			})).To(Succeed())
			Expect(repo.Count()).To(Equal(1))
		})
	})

	Describe("Delete", func() {
		BeforeEach(func() {
			Expect(repo.Add(skills.AddSkillRequest{
				Name: "deletable", Description: "to be removed", Instructions: "bye",
			})).To(Succeed())
		})

		It("removes a skill from disk and index", func() {
			Expect(repo.Count()).To(Equal(1))

			err := repo.Delete("deletable")
			Expect(err).NotTo(HaveOccurred())
			Expect(repo.Count()).To(Equal(0))

			// Directory should be gone
			_, err = os.Stat(filepath.Join(baseDir, "deletable"))
			Expect(os.IsNotExist(err)).To(BeTrue())

			// Summaries should be empty
			Expect(repo.Summaries()).To(BeEmpty())
		})

		It("returns an error for non-existent skills", func() {
			err := repo.Delete("ghost")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})

		It("returns an error for empty name", func() {
			err := repo.Delete("")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("name is required"))
		})

		It("allows re-adding after deletion", func() {
			Expect(repo.Delete("deletable")).To(Succeed())
			err := repo.Add(skills.AddSkillRequest{
				Name: "deletable", Description: "reborn", Instructions: "back",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(repo.Count()).To(Equal(1))
		})
	})
})
