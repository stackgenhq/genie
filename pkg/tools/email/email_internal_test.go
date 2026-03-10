// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Email Internal", func() {
	Describe("smtpIMAPService Validate", func() {
		It("returns error on missing Host/Port", func() {
			srv := &smtpIMAPService{cfg: Config{Host: ""}}
			err := srv.Validate(context.Background())
			Expect(err).To(HaveOccurred())
		})
		It("passes with correct auth", func() {
			srv := &smtpIMAPService{cfg: Config{Host: "smtp.example.com", Port: 587}}
			err := srv.Validate(context.Background())
			Expect(err).To(Not(HaveOccurred()))
		})
	})

	Describe("parseContentType", func() {
		It("returns correctly", func() {
			mT, params, err := parseContentType("text/html; charset=UTF-8")
			Expect(err).To(Not(HaveOccurred()))
			Expect(mT).To(Equal("text/html"))
			Expect(params["charset"]).To(Equal("UTF-8"))
		})
		It("returns plain text on empty", func() {
			mT, _, err := parseContentType("")
			Expect(err).To(Not(HaveOccurred()))
			Expect(mT).To(Equal("text/plain"))
		})
	})
})
