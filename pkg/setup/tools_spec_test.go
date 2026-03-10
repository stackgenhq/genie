// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/

package setup

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LoadSetupTools", func() {
	It("loads tools from embedded tools.json", func() {
		tools, err := LoadSetupTools()
		Expect(err).NotTo(HaveOccurred())
		Expect(tools).NotTo(BeEmpty())
		var ids []string
		for _, t := range tools {
			ids = append(ids, t.ID)
			Expect(t.Name).NotTo(BeEmpty())
			Expect(t.Questions).NotTo(BeNil())
		}
		Expect(ids).To(ContainElement("email"))
		Expect(ids).To(ContainElement("web_search"))
		Expect(ids).To(ContainElement("google_drive"))
	})
})

var _ = Describe("ApplyToolAnswers", func() {
	It("applies web_search answers into GenieConfig", func() {
		in := DefaultWizardInputs()
		cfg := BuildGenieConfig(in, nil, map[string]map[string]string{
			"web_search": {
				"provider":       "google",
				"google_api_key": "key123",
				"google_cx":      "cx456",
			},
		})
		Expect(cfg.WebSearch.Provider).To(Equal("google"))
		Expect(cfg.WebSearch.GoogleAPIKey).To(Equal("key123"))
		Expect(cfg.WebSearch.GoogleCX).To(Equal("cx456"))
	})

	It("applies email answers and encodes in TOML", func() {
		in := DefaultWizardInputs()
		toolAnswers := map[string]map[string]string{
			"email": {
				"provider":  "smtp",
				"host":      "smtp.example.com",
				"port":      "587",
				"username":  "user@example.com",
				"password":  "secret",
				"imap_host": "imap.example.com",
				"imap_port": "993",
			},
		}
		cfg := BuildGenieConfig(in, nil, toolAnswers)
		Expect(cfg.Email.Provider).To(Equal("smtp"))
		Expect(cfg.Email.Host).To(Equal("smtp.example.com"))
		Expect(cfg.Email.Port).To(Equal(587))
		Expect(cfg.Email.IMAPPort).To(Equal(993))
		var buf bytes.Buffer
		Expect(EncodeTOML(&buf, cfg)).NotTo(HaveOccurred())
		tomlStr := buf.String()
		Expect(tomlStr).To(ContainSubstring("[email]"))
		Expect(tomlStr).To(ContainSubstring("smtp"))
	})
})
