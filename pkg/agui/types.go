// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui

import (
	"time"
)

// LogEntry represents a single log entry with timestamp.
type LogEntry struct {
	Timestamp time.Time
	Level     LogLevel
	Message   string
	Source    string
}
