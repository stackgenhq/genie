// Package cron re-exports model types from pkg/db for convenience.
// The actual GORM models live in pkg/db to avoid import cycles
// (pkg/cron → pkg/agui → pkg/db).
package cron

import "github.com/appcd-dev/genie/pkg/db"

// CronTask is a type alias for the GORM model defined in pkg/db.
type CronTask = db.CronTask

// CronHistory is a type alias for the GORM model defined in pkg/db.
type CronHistory = db.CronHistory
