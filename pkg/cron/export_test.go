package cron

import "time"

// SetTickerIntervalForTest overrides the ticker interval for testing.
// This allows tests to use a very short interval instead of waiting a full minute.
func (s *Scheduler) SetTickerIntervalForTest(d time.Duration) {
	s.tickerInterval = d
}
