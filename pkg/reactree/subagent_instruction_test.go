// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("buildSubAgentInstruction", func() {
	Describe("tool-use enforcement", func() {
		It("front-loads the tool-use mandate as the very first sentence", func() {
			instruction := buildSubAgentInstruction([]string{"run_shell"})
			Expect(instruction).To(HavePrefix("MANDATORY: You MUST call your tools"))
		})

		It("prohibits 'I don't know' refusals", func() {
			instruction := buildSubAgentInstruction([]string{"run_shell"})
			Expect(instruction).To(ContainSubstring("NEVER say 'I don't know'"))
			Expect(instruction).To(ContainSubstring("I don't have access"))
			Expect(instruction).To(ContainSubstring("I cannot"))
		})

		It("prohibits outputting commands as text", func() {
			instruction := buildSubAgentInstruction([]string{"run_shell"})
			Expect(instruction).To(ContainSubstring("NEVER output commands, scripts, or code as text"))
		})

		It("requires executing embedded scripts via run_shell", func() {
			instruction := buildSubAgentInstruction([]string{"run_shell"})
			Expect(instruction).To(ContainSubstring("call run_shell to EXECUTE it"))
			Expect(instruction).To(ContainSubstring("Do NOT echo, display, or render scripts as markdown"))
		})

		It("includes SCRIPT EXECUTION directive for bash blocks", func() {
			instruction := buildSubAgentInstruction([]string{"run_shell"})
			Expect(instruction).To(ContainSubstring("SCRIPT EXECUTION"))
			Expect(instruction).To(ContainSubstring("extract the script content and pass it to run_shell"))
			Expect(instruction).To(ContainSubstring("Responding with the script as text is a failure"))
		})
	})

	Describe("tool allowlist", func() {
		It("includes tool names when provided", func() {
			instruction := buildSubAgentInstruction([]string{"run_shell", "read_file", "save_file"})
			Expect(instruction).To(ContainSubstring("AVAILABLE TOOLS"))
			Expect(instruction).To(ContainSubstring("run_shell"))
			Expect(instruction).To(ContainSubstring("read_file"))
			Expect(instruction).To(ContainSubstring("save_file"))
		})

		It("omits tool allowlist when no tools given", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).NotTo(ContainSubstring("AVAILABLE TOOLS (you MUST ONLY call these)"))
		})

		It("omits tool allowlist when empty slice given", func() {
			instruction := buildSubAgentInstruction([]string{})
			Expect(instruction).NotTo(ContainSubstring("AVAILABLE TOOLS (you MUST ONLY call these)"))
		})
	})

	Describe("core directives", func() {
		It("includes the focused sub-agent identity", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("You are a focused sub-agent"))
		})

		It("prohibits send_message", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("Do NOT try to call send_message"))
		})

		It("includes HITL rejection guidance", func() {
			instruction := buildSubAgentInstruction([]string{"run_shell"})
			Expect(instruction).To(ContainSubstring("HITL REJECTION"))
		})

		It("includes anti-loop directive", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("ANTI-LOOP"))
		})

		It("includes error budget directive", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("ERROR BUDGET"))
		})

		It("includes grounding directive", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("GROUNDING"))
			Expect(instruction).To(ContainSubstring("HALLUCINATION DETECTED"))
		})

		It("includes justification requirement", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("JUSTIFICATION"))
			Expect(instruction).To(ContainSubstring("_justification"))
		})

		It("includes working memory guidance", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("Working Memory"))
			Expect(instruction).To(ContainSubstring("do NOT re-fetch"))
		})
	})

	Describe("incremental reporting and shared memory", func() {
		It("includes INCREMENTAL REPORTING directive", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("INCREMENTAL REPORTING"))
		})

		It("instructs per-item reporting for batch operations", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("per-item results"))
			Expect(instruction).To(ContainSubstring("Do NOT wait until all items are processed"))
		})

		It("warns about timeout data loss for partial results", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("if you time out"))
			Expect(instruction).To(ContainSubstring("only the items already reported will be captured"))
		})

		It("includes SHARED MEMORY directive", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("SHARED MEMORY"))
			Expect(instruction).To(ContainSubstring("shared working memory"))
		})

		It("informs about sibling agent visibility", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("Sibling agents running in parallel"))
		})

		It("recommends structured reporting for multi-agent workflows", func() {
			instruction := buildSubAgentInstruction(nil)
			Expect(instruction).To(ContainSubstring("report findings clearly"))
			Expect(instruction).To(ContainSubstring("structured"))
		})
	})
})
