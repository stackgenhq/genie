// Copyright (C) 2026 StackGen, Inc. All rights reserved.
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

var _ = Describe("Scope.Get", func() {
	It("returns values for configured source", func() {
		s := datasource.NewScope("github", []string{"owner/repo"})
		Expect(s.Get("github")).To(Equal([]string{"owner/repo"}))
	})
	It("returns nil for unconfigured source", func() {
		s := datasource.NewScope("github", []string{"a/b"})
		Expect(s.Get("gitlab")).To(BeNil())
	})
	It("returns nil for empty Scope", func() {
		s := datasource.Scope{}
		Expect(s.Get("anything")).To(BeNil())
	})
})
