// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ApproveList", func() {
	Describe("AddBlind and IsApproved", func() {
		It("returns true for a tool added blindly within duration", func() {
			l := NewApproveList()
			l.AddBlind("write_file", 10*time.Minute)
			Expect(l.IsApproved("write_file", `{"path":"/any"}`)).To(BeTrue())
			Expect(l.IsApproved("write_file", "")).To(BeTrue())
		})

		It("is case-insensitive for tool name", func() {
			l := NewApproveList()
			l.AddBlind("Write_File", 10*time.Minute)
			Expect(l.IsApproved("write_file", "{}")).To(BeTrue())
		})

		It("returns false for a different tool", func() {
			l := NewApproveList()
			l.AddBlind("write_file", 10*time.Minute)
			Expect(l.IsApproved("run_shell", "{}")).To(BeFalse())
		})
	})

	Describe("AddWithArgsFilter and IsApproved", func() {
		It("returns true when args contain one of the substrings", func() {
			l := NewApproveList()
			l.AddWithArgsFilter("run_shell", []string{"/tmp", "docs"}, 10*time.Minute)
			Expect(l.IsApproved("run_shell", `{"cmd":"ls /tmp"}`)).To(BeTrue())
			Expect(l.IsApproved("run_shell", `{"cmd":"cat docs/readme"}`)).To(BeTrue())
		})

		It("returns false when args do not contain any substring", func() {
			l := NewApproveList()
			l.AddWithArgsFilter("run_shell", []string{"/tmp"}, 10*time.Minute)
			Expect(l.IsApproved("run_shell", `{"cmd":"ls /home"}`)).To(BeFalse())
		})

		It("is case-insensitive for substring match", func() {
			l := NewApproveList()
			l.AddWithArgsFilter("run_shell", []string{"Docs"}, 10*time.Minute)
			Expect(l.IsApproved("run_shell", `{"path":"/home/docs"}`)).To(BeTrue())
		})

		It("with empty substrings behaves like blind", func() {
			l := NewApproveList()
			l.AddWithArgsFilter("write_file", nil, 10*time.Minute)
			Expect(l.IsApproved("write_file", "{}")).To(BeTrue())
		})
	})

	Describe("expiration", func() {
		It("returns false after duration has elapsed for blind entry", func() {
			l := NewApproveList()
			l.AddBlind("write_file", 2*time.Millisecond)
			Expect(l.IsApproved("write_file", "{}")).To(BeTrue())
			time.Sleep(5 * time.Millisecond)
			Expect(l.IsApproved("write_file", "{}")).To(BeFalse())
		})

		It("returns false after duration has elapsed for filter entry", func() {
			l := NewApproveList()
			l.AddWithArgsFilter("run_shell", []string{"/tmp"}, 2*time.Millisecond)
			Expect(l.IsApproved("run_shell", `{"cmd":"ls /tmp"}`)).To(BeTrue())
			time.Sleep(5 * time.Millisecond)
			Expect(l.IsApproved("run_shell", `{"cmd":"ls /tmp"}`)).To(BeFalse())
		})
	})

	Describe("zero or negative duration", func() {
		It("AddBlind with zero duration does not add entry", func() {
			l := NewApproveList()
			l.AddBlind("write_file", 0)
			Expect(l.IsApproved("write_file", "{}")).To(BeFalse())
		})

		It("AddWithArgsFilter with zero duration does not add entry", func() {
			l := NewApproveList()
			l.AddWithArgsFilter("run_shell", []string{"/tmp"}, 0)
			Expect(l.IsApproved("run_shell", `{"cmd":"ls /tmp"}`)).To(BeFalse())
		})
	})
})
