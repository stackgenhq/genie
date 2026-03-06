package calendar

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/api/calendar/v3"
)

var _ = Describe("Calendar Internal", func() {
	Describe("parseDuration", func() {
		It("parses hours", func() {
			d, err := parseDuration("2h")
			Expect(err).To(Not(HaveOccurred()))
			Expect(d).To(Equal(2 * time.Hour))
		})
		It("errors on empty string", func() {
			_, err := parseDuration("")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("parseTimeRange", func() {
		It("uses default duration and current time", func() {
			t1, t2, err := parseTimeRange("", "", time.Hour)
			Expect(err).To(Not(HaveOccurred()))
			Expect(t2.Sub(t1)).To(Equal(time.Hour))
		})

		It("parses valid RFC3339", func() {
			now := time.Now().Format(time.RFC3339)
			_, _, err := parseTimeRange(now, now, time.Hour)
			Expect(err).To(Not(HaveOccurred()))
		})
	})

	Describe("mergeIntervals", func() {
		It("merges overlapping", func() {
			now := time.Now()
			is := intervals{
				interval{start: now, end: now.Add(2 * time.Hour)},
				interval{start: now.Add(time.Hour), end: now.Add(3 * time.Hour)},
			}
			merged := is.mergeIntervals()
			Expect(merged).To(HaveLen(1))
			Expect(merged[0].end).To(Equal(now.Add(3 * time.Hour)))
		})
	})

	Describe("computeFreeBlocks", func() {
		It("computes gaps", func() {
			now := time.Now()
			is := intervals{
				interval{start: now.Add(time.Hour), end: now.Add(2 * time.Hour)},
			}
			free := is.computeFreeBlocks(now, now.Add(3*time.Hour))
			// Expect free blocks before and after
			Expect(free).To(HaveLen(2))
			Expect(free[0].start).To(Equal(now))
			Expect(free[0].end).To(Equal(now.Add(time.Hour)))
			Expect(free[1].start).To(Equal(now.Add(2 * time.Hour)))
			Expect(free[1].end).To(Equal(now.Add(3 * time.Hour)))
		})
	})

	Describe("formatDuration", func() {
		It("formats correctly", func() {
			s := formatDuration(time.Hour)
			Expect(s).To(Equal("1h"))

			s2 := formatDuration(90 * time.Minute)
			Expect(s2).To(Equal("1h30m"))
		})
	})

	Describe("gcalEventToCalendarEvent", func() {
		It("maps correctly", func() {
			g := &calendar.Event{
				Id:          "123",
				Summary:     "Test",
				Description: "Desc",
				Location:    "Loc",
				Start:       &calendar.EventDateTime{DateTime: "2023-01-01T10:00:00Z"},
				End:         &calendar.EventDateTime{DateTime: "2023-01-01T11:00:00Z"},
				Attendees: []*calendar.EventAttendee{
					{Email: "alice@example.com"},
					{Email: "bob@example.com"},
				},
				Creator: &calendar.EventCreator{Email: "test@test"},
			}
			evt := gcalEventToCalendarEvent(g)
			Expect(evt.ID).To(Equal("123"))
			Expect(evt.Title).To(Equal("Test"))
			Expect(evt.Description).To(Equal("Desc"))
			Expect(evt.Location).To(Equal("Loc"))
			Expect(evt.StartTime).To(Equal("2023-01-01T10:00:00Z"))
			Expect(evt.EndTime).To(Equal("2023-01-01T11:00:00Z"))
			Expect(evt.Attendees).To(ConsistOf("alice@example.com", "bob@example.com"))
		})
	})

	Describe("formatFreeBusyForLLM", func() {
		It("formats free and busy blocks", func() {
			now := time.Now()
			is := intervals{
				interval{start: now.Add(time.Hour), end: now.Add(2 * time.Hour)},
			}
			s := is.formatFreeBusyForLLM("test@test.com", now, now.Add(3*time.Hour))
			Expect(s).To(ContainSubstring("FREE_BLOCKS"))
			Expect(s).To(ContainSubstring("BUSY_PERIODS"))
			Expect(s).To(ContainSubstring("test@test.com"))
		})
	})
})
