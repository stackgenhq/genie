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
type CronEntry struct {
	Name       string `yaml:"name" toml:"name"`
	Expression string `yaml:"expression" toml:"expression"`
	Action     string `yaml:"action" toml:"action"` // prompt sent to the agent
}

// Config holds the cron scheduler configuration. The Enabled flag controls
// whether the scheduler starts at all. Tasks lists the statically configured
// cron jobs; additional tasks can be created at runtime via the
// create_recurring_task tool.
type Config struct {
	Tasks []CronEntry `yaml:"tasks" toml:"tasks"`
}
