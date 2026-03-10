// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cron

import "time"

// SetTickerIntervalForTest overrides the ticker interval for tests only. Not part of the public API.
func (s *Scheduler) SetTickerIntervalForTest(d time.Duration) {
	s.tickerInterval = d
}
