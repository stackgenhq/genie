// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package calendar_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/tools/google/calendar"
)

var _ = Describe("CalendarConnector", func() {
	Describe("Name", func() {
		It("returns calendar", func() {
			conn := calendar.NewCalendarConnector(nil)
			Expect(conn.Name()).To(Equal("calendar"))
		})
	})

	Describe("ListItems", func() {
		It("returns nil when scope has no calendar IDs", func(ctx context.Context) {
			conn := calendar.NewCalendarConnector(nil)
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
		})
	})
})
