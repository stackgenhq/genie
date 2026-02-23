package datetime

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DateTime Tool (util_datetime)", func() {
	var d *datetimeTools

	BeforeEach(func() {
		d = newDatetimeTools()
	})

	Describe("now", func() {
		It("returns current time in UTC by default", func() {
			resp, err := d.datetime(context.Background(), datetimeRequest{Operation: "now"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Operation).To(Equal("now"))
			Expect(resp.Timezone).To(Equal("UTC"))
			Expect(resp.Unix).To(BeNumerically(">", 0))
			Expect(resp.Weekday).NotTo(BeEmpty())
		})

		It("returns current time in a specified timezone", func() {
			resp, err := d.datetime(context.Background(), datetimeRequest{Operation: "now", Timezone: "America/New_York"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Timezone).To(Equal("America/New_York"))
		})

		It("returns error for invalid timezone", func() {
			_, err := d.datetime(context.Background(), datetimeRequest{Operation: "now", Timezone: "Invalid/Zone"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown timezone"))
		})
	})

	Describe("parse", func() {
		DescribeTable("parses common date formats (via dateparse)",
			func(input string) {
				resp, err := d.datetime(context.Background(), datetimeRequest{Operation: "parse", DateTime: input})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Result).NotTo(BeEmpty())
				Expect(resp.Weekday).NotTo(BeEmpty())
				Expect(resp.Unix).NotTo(BeZero())
			},
			Entry("RFC3339", "2024-01-15T10:30:00Z"),
			Entry("date only", "2024-01-15"),
			Entry("datetime space", "2024-01-15 10:30:00"),
			Entry("US date", "01/15/2024"),
			Entry("natural date", "Jan 15, 2024"),
			Entry("full natural date", "January 15, 2024"),
			Entry("RFC1123", "Mon, 15 Jan 2024 10:30:00 UTC"),
		)

		It("returns error for empty datetime", func() {
			_, err := d.datetime(context.Background(), datetimeRequest{Operation: "parse"})
			Expect(err).To(HaveOccurred())
		})

		It("returns error for unparseable datetime", func() {
			_, err := d.datetime(context.Background(), datetimeRequest{Operation: "parse", DateTime: "not-a-date"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("format", func() {
		DescribeTable("formats datetime using presets",
			func(format, expected string) {
				resp, err := d.datetime(context.Background(), datetimeRequest{
					Operation: "format",
					DateTime:  "2024-01-15T10:30:00Z",
					Format:    format,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Result).To(Equal(expected))
			},
			Entry("date preset", "date", "2024-01-15"),
			Entry("time preset", "time", "10:30:00"),
			Entry("datetime preset", "datetime", "2024-01-15 10:30:00"),
		)
	})

	Describe("add", func() {
		It("adds a positive duration", func() {
			resp, err := d.datetime(context.Background(), datetimeRequest{
				Operation: "add",
				DateTime:  "2024-01-15T10:00:00Z",
				Duration:  "2h30m",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("2024-01-15T12:30:00Z"))
		})

		It("subtracts a negative duration", func() {
			resp, err := d.datetime(context.Background(), datetimeRequest{
				Operation: "add",
				DateTime:  "2024-01-15T10:00:00Z",
				Duration:  "-24h",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("2024-01-14T10:00:00Z"))
		})

		It("returns error for missing duration", func() {
			_, err := d.datetime(context.Background(), datetimeRequest{Operation: "add", DateTime: "2024-01-15T10:00:00Z"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duration is required"))
		})

		It("returns error for invalid duration", func() {
			_, err := d.datetime(context.Background(), datetimeRequest{
				Operation: "add",
				DateTime:  "2024-01-15T10:00:00Z",
				Duration:  "bad",
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("diff", func() {
		It("computes difference between two datetimes", func() {
			resp, err := d.datetime(context.Background(), datetimeRequest{
				Operation: "diff",
				DateTime:  "2024-01-15T00:00:00Z",
				DateTime2: "2024-01-17T12:00:00Z",
			})
			Expect(err).NotTo(HaveOccurred())
			dur, _ := time.ParseDuration(resp.Result)
			Expect(dur.Hours()).To(Equal(60.0))
		})

		It("returns error when datetime2 is missing", func() {
			_, err := d.datetime(context.Background(), datetimeRequest{
				Operation: "diff",
				DateTime:  "2024-01-15T00:00:00Z",
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("convert_tz", func() {
		It("converts UTC to another timezone", func() {
			resp, err := d.datetime(context.Background(), datetimeRequest{
				Operation: "convert_tz",
				DateTime:  "2024-01-15T10:00:00Z",
				Timezone:  "America/New_York",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("2024-01-15T05:00:00-05:00"))
			Expect(resp.Timezone).To(Equal("America/New_York"))
		})

		It("returns error for missing timezone", func() {
			_, err := d.datetime(context.Background(), datetimeRequest{
				Operation: "convert_tz",
				DateTime:  "2024-01-15T10:00:00Z",
			})
			Expect(err).To(HaveOccurred())
		})
	})

	It("returns error for unsupported operation", func() {
		_, err := d.datetime(context.Background(), datetimeRequest{Operation: "invalid"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported operation"))
	})
})

var _ = Describe("DateTime ToolProvider", func() {
	It("returns the expected tool", func() {
		p := NewToolProvider()
		tools := p.GetTools()
		Expect(tools).To(HaveLen(1))
		Expect(tools[0].Declaration().Name).To(Equal("util_datetime"))
	})
})
