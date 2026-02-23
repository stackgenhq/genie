package calendar

import (
	"context"

	"github.com/appcd-dev/genie/pkg/security/securityfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Calendar Tools", func() {
	var (
		c                  *calendarTools
		fakeSecretProvider *securityfakes.FakeSecretProvider
	)

	BeforeEach(func() {
		fakeSecretProvider = &securityfakes.FakeSecretProvider{}
		c = newCalendarTools("test_cal", fakeSecretProvider)
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
			Expect(err.Error()).To(ContainSubstring("Google Calendar not configured"))
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
			Expect(err.Error()).To(ContainSubstring("Google Calendar not configured"))
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
			Expect(err.Error()).To(ContainSubstring("Google Calendar not configured"))
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
			Expect(err.Error()).To(ContainSubstring("Google Calendar not configured"))
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
			Expect(err.Error()).To(ContainSubstring("Google Calendar not configured"))
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
			Expect(err.Error()).To(ContainSubstring("Google Calendar not configured"))
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
			intervals := mergeIntervals([]interval{
				{start: mustParseTime("2025-01-01T10:00:00Z"), end: mustParseTime("2025-01-01T11:00:00Z")},
				{start: mustParseTime("2025-01-01T10:30:00Z"), end: mustParseTime("2025-01-01T12:00:00Z")},
			})
			Expect(intervals).To(HaveLen(1))
			Expect(intervals[0].end).To(Equal(mustParseTime("2025-01-01T12:00:00Z")))
		})

		It("keeps non-overlapping intervals separate", func() {
			intervals := mergeIntervals([]interval{
				{start: mustParseTime("2025-01-01T10:00:00Z"), end: mustParseTime("2025-01-01T11:00:00Z")},
				{start: mustParseTime("2025-01-01T13:00:00Z"), end: mustParseTime("2025-01-01T14:00:00Z")},
			})
			Expect(intervals).To(HaveLen(2))
		})
	})
})
