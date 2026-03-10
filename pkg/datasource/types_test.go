// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package datasource_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/datasource"
)

var _ = Describe("SourceRefID", func() {
	It("returns empty when item is nil", func() {
		var item *datasource.NormalizedItem
		Expect(item.SourceRefID()).To(Equal(""))
	})
	It("returns SourceRef.RefID when set", func() {
		item := &datasource.NormalizedItem{
			ID:        "gmail:msg123",
			SourceRef: &datasource.SourceRef{Type: "gmail", RefID: "msg123"},
		}
		Expect(item.SourceRefID()).To(Equal("msg123"))
	})
	It("derives from ID when SourceRef is nil", func() {
		item := &datasource.NormalizedItem{ID: "gmail:msg456", Source: "gmail"}
		Expect(item.SourceRefID()).To(Equal("msg456"))
	})
	It("derives from ID when SourceRef.RefID is empty", func() {
		item := &datasource.NormalizedItem{
			ID:        "gdrive:file789",
			SourceRef: &datasource.SourceRef{Type: "gdrive", RefID: ""},
		}
		Expect(item.SourceRefID()).To(Equal("file789"))
	})
	It("returns full ID when ID has no colon", func() {
		item := &datasource.NormalizedItem{ID: "standalone"}
		Expect(item.SourceRefID()).To(Equal("standalone"))
	})
})

var _ = Describe("Scope.ReposForSCM", func() {
	It("returns GitHubRepos for github", func() {
		s := datasource.Scope{GitHubRepos: []string{"owner/repo"}}
		Expect(s.ReposForSCM("github")).To(Equal([]string{"owner/repo"}))
	})
	It("returns GitLabRepos for gitlab", func() {
		s := datasource.Scope{GitLabRepos: []string{"group/project"}}
		Expect(s.ReposForSCM("gitlab")).To(Equal([]string{"group/project"}))
	})
	It("returns nil for unknown source", func() {
		s := datasource.Scope{GitHubRepos: []string{"a/b"}}
		Expect(s.ReposForSCM("bitbucket")).To(BeNil())
	})
})
