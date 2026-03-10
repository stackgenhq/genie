// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package calendar

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/security/securityfakes"
)

// errCalendarNotConfigured is the error returned by the fake credentials getter
// so "requires credentials" tests pass without real oauth/keyring.
var errCalendarNotConfigured = fmt.Errorf(
	"google Calendar not configured: set CredentialsFile (path or JSON) in your integration, " +
		"or build with -X to inject GoogleClientID and GoogleClientSecret",
)

var _ = Describe("Calendar Tools", func() {
	var (
		c                  *calendarTools
		fakeSecretProvider *securityfakes.FakeSecretProvider
	)

	BeforeEach(func() {
		fakeSecretProvider = &securityfakes.FakeSecretProvider{}
		// Use a fake credentials getter so tests never hit real oauth/keyring.
		c = newCalendarToolsWithCredsGetter("test_cal", fakeSecretProvider, func(string) ([]byte, error) {
			return nil, errCalendarNotConfigured
		})
	})

	Describe("calendar_list_events", func() {
		It("rejects invalid time_min format", func() {
			_, err := c.handleListEvents(context.Background(), listEventsRequest{
				TimeMin: "not-a-date",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid time_min"))
		})

		It("rejects invalid time_max format", func() {
			_, err := c.handleListEvents(context.Background(), listEventsRequest{
				TimeMax: "not-a-date",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid time_max"))
		})

		It("requires credentials", func() {
			_, err := c.handleListEvents(context.Background(), listEventsRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("google Calendar not configured"))
		})
	})

	Describe("calendar_next_events", func() {
		It("rejects invalid duration", func() {
			_, err := c.handleNextEvents(context.Background(), nextEventsRequest{
				Duration: "xyz",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid duration"))
		})

		It("requires credentials", func() {
			_, err := c.handleNextEvents(context.Background(), nextEventsRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("google Calendar not configured"))
		})
	})

	Describe("calendar_create_event", func() {
		It("requires title", func() {
			_, err := c.handleCreateEvent(context.Background(), createEventRequest{
				StartTime: "2025-01-01T10:00:00Z",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("title is required"))
		})

		It("requires start_time", func() {
			_, err := c.handleCreateEvent(context.Background(), createEventRequest{
				Title: "Team Standup",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("start_time is required"))
		})

		It("rejects invalid start_time", func() {
			_, err := c.handleCreateEvent(context.Background(), createEventRequest{
				Title:     "Team Standup",
				StartTime: "bad-time",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid start_time"))
		})

		It("rejects invalid end_time", func() {
			_, err := c.handleCreateEvent(context.Background(), createEventRequest{
				Title:     "Team Standup",
				StartTime: "2025-01-01T10:00:00Z",
				EndTime:   "bad-time",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid end_time"))
		})
	})

	Describe("calendar_update_event", func() {
		It("requires event_id", func() {
			_, err := c.handleUpdateEvent(context.Background(), updateEventRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("event_id is required"))
		})

		It("requires credentials", func() {
			_, err := c.handleUpdateEvent(context.Background(), updateEventRequest{
				EventID: "abc123",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("google Calendar not configured"))
		})
	})

	Describe("calendar_delete_event", func() {
		It("requires event_id", func() {
			_, err := c.handleDeleteEvent(context.Background(), deleteEventRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("event_id is required"))
		})

		It("requires credentials", func() {
			_, err := c.handleDeleteEvent(context.Background(), deleteEventRequest{
				EventID: "abc123",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("google Calendar not configured"))
		})
	})

	Describe("calendar_free_busy", func() {
		It("rejects invalid time_min", func() {
			_, err := c.handleFreeBusy(context.Background(), freeBusyRequest{
				TimeMin: "not-a-date",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid time_min"))
		})

		It("requires credentials", func() {
			_, err := c.handleFreeBusy(context.Background(), freeBusyRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("google Calendar not configured"))
		})
	})

	Describe("free/busy formatting for LLM", func() {
		Describe("computeFreeBlocks", func() {
			It("returns full range when no busy periods", func() {
				start := mustParseTime("2026-02-26T09:00:00-08:00")
				end := mustParseTime("2026-02-26T18:00:00-08:00")
				free := intervals{}.computeFreeBlocks(start, end)
				Expect(free).To(HaveLen(1))
				Expect(free[0].start).To(Equal(start))
				Expect(free[0].end).To(Equal(end))
			})

			It("returns one free block before and one after a single busy period", func() {
				start := mustParseTime("2026-02-26T09:00:00-08:00")
				end := mustParseTime("2026-02-26T18:00:00-08:00")
				busy := intervals{
					{start: mustParseTime("2026-02-26T10:30:00-08:00"), end: mustParseTime("2026-02-26T12:00:00-08:00")},
				}
				free := busy.computeFreeBlocks(start, end)
				Expect(free).To(HaveLen(2))
				Expect(free[0].start).To(Equal(start))
				Expect(free[0].end).To(Equal(busy[0].start))
				Expect(free[1].start).To(Equal(busy[0].end))
				Expect(free[1].end).To(Equal(end))
			})

			It("returns gaps between merged busy periods", func() {
				start := mustParseTime("2026-02-26T09:00:00-08:00")
				end := mustParseTime("2026-02-26T18:00:00-08:00")
				busy := intervals{
					{start: mustParseTime("2026-02-26T10:30:00-08:00"), end: mustParseTime("2026-02-26T11:00:00-08:00")},
					{start: mustParseTime("2026-02-26T11:00:00-08:00"), end: mustParseTime("2026-02-26T12:15:00-08:00")},
					{start: mustParseTime("2026-02-26T12:00:00-08:00"), end: mustParseTime("2026-02-26T12:45:00-08:00")},
				}
				busy.sort()
				merged := busy.mergeIntervals()
				Expect(merged).To(HaveLen(1))
				Expect(merged[0].start).To(Equal(mustParseTime("2026-02-26T10:30:00-08:00")))
				Expect(merged[0].end).To(Equal(mustParseTime("2026-02-26T12:45:00-08:00")))
				free := merged.computeFreeBlocks(start, end)
				Expect(free).To(HaveLen(2))
				Expect(free[0].start).To(Equal(start))
				Expect(free[0].end).To(Equal(merged[0].start))
				Expect(free[1].start).To(Equal(merged[0].end))
				Expect(free[1].end).To(Equal(end))
			})
		})

		Describe("formatFreeBusyForLLM", func() {
			It("includes FREE_BUSY_SUMMARY, BUSY_PERIODS, FREE_BLOCKS section headers", func() {
				start := mustParseTime("2026-02-26T09:00:00-08:00")
				end := mustParseTime("2026-02-26T18:00:00-08:00")
				out := intervals{}.formatFreeBusyForLLM("primary", start, end)
				Expect(out).To(ContainSubstring("FREE_BUSY_SUMMARY:"))
				Expect(out).To(ContainSubstring("BUSY_PERIODS:"))
				Expect(out).To(ContainSubstring("FREE_BLOCKS:"))
			})

			It("reports full range as single free block when no busy periods", func() {
				start := mustParseTime("2026-02-26T09:00:00-08:00")
				end := mustParseTime("2026-02-26T18:00:00-08:00")
				out := intervals{}.formatFreeBusyForLLM("primary", start, end)
				Expect(out).To(ContainSubstring("0 busy periods | 1 free blocks (9h total)"))
				Expect(out).To(ContainSubstring("(none)"))
				Expect(out).To(ContainSubstring("2026-02-26T09:00:00-08:00 → 2026-02-26T18:00:00-08:00 (9h)"))
			})

			It("lists busy periods and free blocks with durations", func() {
				start := mustParseTime("2026-02-26T09:00:00-08:00")
				end := mustParseTime("2026-02-26T18:00:00-08:00")
				busy := intervals{
					{start: mustParseTime("2026-02-26T10:30:00-08:00"), end: mustParseTime("2026-02-26T12:45:00-08:00")},
				}
				out := busy.formatFreeBusyForLLM("primary", start, end)
				Expect(out).To(ContainSubstring("1 busy periods | 2 free blocks"))
				Expect(out).To(ContainSubstring("2026-02-26T10:30:00-08:00 → 2026-02-26T12:45:00-08:00 (2h15m)"))
				Expect(out).To(ContainSubstring("2026-02-26T09:00:00-08:00 → 2026-02-26T10:30:00-08:00 (1h30m)"))
				Expect(out).To(ContainSubstring("2026-02-26T12:45:00-08:00 → 2026-02-26T18:00:00-08:00 (5h15m)"))
			})

			It("includes calendar ID in output", func() {
				start := mustParseTime("2026-02-26T09:00:00-08:00")
				end := mustParseTime("2026-02-26T18:00:00-08:00")
				out := intervals{}.formatFreeBusyForLLM("primary", start, end)
				Expect(out).To(HavePrefix("Calendar \"primary\":\n"))
			})

			It("is parseable: FREE_BLOCKS section has one line per block", func() {
				start := mustParseTime("2026-02-26T09:00:00-08:00")
				end := mustParseTime("2026-02-26T18:00:00-08:00")
				busy := intervals{
					{start: mustParseTime("2026-02-26T10:00:00-08:00"), end: mustParseTime("2026-02-26T11:00:00-08:00")},
					{start: mustParseTime("2026-02-26T14:00:00-08:00"), end: mustParseTime("2026-02-26T15:00:00-08:00")},
				}
				out := busy.formatFreeBusyForLLM("primary", start, end)
				freeBlocksSection := out[strings.Index(out, "FREE_BLOCKS:")+12:]
				if idx := strings.Index(freeBlocksSection, "\n\n"); idx > 0 {
					freeBlocksSection = freeBlocksSection[:idx]
				}
				lines := strings.Split(strings.TrimSpace(freeBlocksSection), "\n")
				// 3 free blocks: 09-10, 11-14, 15-18
				Expect(lines).To(HaveLen(3))
			})
		})
	})

	Describe("calendar_quick_add", func() {
		It("requires text", func() {
			_, err := c.handleQuickAdd(context.Background(), quickAddRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("text is required"))
		})

		It("requires credentials", func() {
			_, err := c.handleQuickAdd(context.Background(), quickAddRequest{
				Text: "Lunch tomorrow at noon",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("google Calendar not configured"))
		})
	})

	Describe("calendar_find_time", func() {
		It("requires attendees", func() {
			_, err := c.handleFindTime(context.Background(), findTimeRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("attendees is required"))
		})

		It("rejects invalid duration", func() {
			_, err := c.handleFindTime(context.Background(), findTimeRequest{
				Attendees: []string{"alice@example.com"},
				Duration:  "xyz",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid duration"))
		})

		It("rejects invalid slot_duration", func() {
			_, err := c.handleFindTime(context.Background(), findTimeRequest{
				Attendees:    []string{"alice@example.com"},
				SlotDuration: "xyz",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid slot_duration"))
		})
	})

	Describe("provider", func() {
		It("creates all tools via provider", func() {
			p := NewToolProvider(fakeSecretProvider)
			tools := p.GetTools("test_cal")
			Expect(tools).To(HaveLen(8))
		})
	})

	Describe("helpers", func() {
		It("parses standard Go durations", func() {
			d, err := parseDuration("30m")
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Minutes()).To(Equal(30.0))
		})

		It("parses day durations", func() {
			d, err := parseDuration("3d")
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Hours()).To(Equal(72.0))
		})

		It("parses week durations", func() {
			d, err := parseDuration("1w")
			Expect(err).NotTo(HaveOccurred())
			Expect(d.Hours()).To(Equal(168.0))
		})

		It("rejects empty duration", func() {
			_, err := parseDuration("")
			Expect(err).To(HaveOccurred())
		})

		It("rejects unknown suffix", func() {
			_, err := parseDuration("5x")
			Expect(err).To(HaveOccurred())
		})

		It("merges overlapping intervals", func() {
			intervals := intervals{
				{start: mustParseTime("2025-01-01T10:00:00Z"), end: mustParseTime("2025-01-01T11:00:00Z")},
				{start: mustParseTime("2025-01-01T10:30:00Z"), end: mustParseTime("2025-01-01T12:00:00Z")},
			}.mergeIntervals()
			Expect(intervals).To(HaveLen(1))
			Expect(intervals[0].end).To(Equal(mustParseTime("2025-01-01T12:00:00Z")))
		})

		It("keeps non-overlapping intervals separate", func() {
			intervals := intervals{
				{start: mustParseTime("2025-01-01T10:00:00Z"), end: mustParseTime("2025-01-01T11:00:00Z")},
				{start: mustParseTime("2025-01-01T13:00:00Z"), end: mustParseTime("2025-01-01T14:00:00Z")},
			}.mergeIntervals()
			Expect(intervals).To(HaveLen(2))
		})
	})
})
