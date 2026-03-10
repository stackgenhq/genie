// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package cron provides a recurring task scheduler whose task definitions and
// execution history are persisted in a GORM-backed database. Instead of relying
// on an in-memory runner such as robfig/cron/v3, it polls the database for due
// tasks and uses adhocore/gronx for cron expression parsing and next-run
// calculation. Users define cron jobs through configuration files or
// dynamically via the create_recurring_task LLM tool. Every execution is
// recorded in the cron_history table for audit purposes, giving Genie a
// mechanism for scheduling and auditing recurring automated tasks.
package cron

// CronEntry represents a single cron task definition from the configuration file.
// Users add entries under [cron.tasks] in their .genie.toml or .genie.yaml.
// When Action is "genie:report", the built-in activity report runs instead of the agent:
// it reads recent audit log events, writes a markdown report to ~/.genie/reports/<agent_name>/<YYYYMMDD>_<name>.md,
// and stores the summary in vector memory for future reference.
type CronEntry struct {
	Name       string `yaml:"name,omitempty" toml:"name,omitempty"`
	Expression string `yaml:"expression,omitempty" toml:"expression,omitempty"`
	Action     string `yaml:"action,omitempty" toml:"action,omitempty"` // prompt sent to the agent, or "genie:report" for activity report
}

// Config holds the cron scheduler configuration. The Enabled flag controls
// whether the scheduler starts at all. Tasks lists the statically configured
// cron jobs; additional tasks can be created at runtime via the
// create_recurring_task tool.
type Config struct {
	Tasks []CronEntry `yaml:"tasks,omitempty" toml:"tasks,omitempty"`
}
