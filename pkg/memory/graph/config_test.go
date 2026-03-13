// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package graph_test

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/memory/graph"
)

var _ = Describe("Config", func() {
	Describe("IsVectorStoreBackend", func() {
		It("returns true for 'vectorstore'", func() {
			cfg := graph.Config{Backend: "vectorstore"}
			Expect(cfg.IsVectorStoreBackend()).To(BeTrue())
		})

		It("returns true for 'VectorStore' (case-insensitive)", func() {
			cfg := graph.Config{Backend: "VectorStore"}
			Expect(cfg.IsVectorStoreBackend()).To(BeTrue())
		})

		It("returns true for ' vectorstore ' (with whitespace)", func() {
			cfg := graph.Config{Backend: "  vectorstore  "}
			Expect(cfg.IsVectorStoreBackend()).To(BeTrue())
		})

		It("returns false for 'inmemory'", func() {
			cfg := graph.Config{Backend: "inmemory"}
			Expect(cfg.IsVectorStoreBackend()).To(BeFalse())
		})

		It("returns false for empty string", func() {
			cfg := graph.Config{Backend: ""}
			Expect(cfg.IsVectorStoreBackend()).To(BeFalse())
		})
	})

	Describe("DefaultConfig", func() {
		It("returns a non-disabled config with empty DataDir", func() {
			cfg := graph.DefaultConfig()
			Expect(cfg.Disabled).To(BeFalse())
			Expect(cfg.DataDir).To(Equal(""))
		})
	})

	Describe("DataDirForAgent", func() {
		It("returns a path under genie dir for a given agent name", func() {
			dir := graph.DataDirForAgent("my-agent")
			// The sanitizer may replace hyphens with underscores.
			Expect(dir).To(SatisfyAny(ContainSubstring("my-agent"), ContainSubstring("my_agent")))
			Expect(filepath.IsAbs(dir)).To(BeTrue())
		})

		It("uses 'genie' as default when agent name is empty", func() {
			dir := graph.DataDirForAgent("")
			Expect(filepath.Base(dir)).To(Equal("genie"))
		})

		It("sanitizes agent names with special characters", func() {
			dir := graph.DataDirForAgent("my/agent:with*chars")
			// The path should not contain forward slashes in the filename portion
			base := filepath.Base(dir)
			Expect(base).NotTo(ContainSubstring("/"))
			Expect(base).NotTo(ContainSubstring(":"))
		})
	})

	Describe("StopGraphLearnPath", func() {
		It("returns a path ending with graph_learn_stop filename", func() {
			path := graph.StopGraphLearnPath("test-agent")
			Expect(filepath.Base(path)).To(Equal(graph.GraphLearnStopFilename))
			Expect(path).To(SatisfyAny(ContainSubstring("test-agent"), ContainSubstring("test_agent")))
		})

		It("uses default agent dir when name is empty", func() {
			path := graph.StopGraphLearnPath("")
			Expect(filepath.Base(path)).To(Equal(graph.GraphLearnStopFilename))
		})
	})
})
