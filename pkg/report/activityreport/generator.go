// Package activityreport generates markdown reports from recent audit
// activities and writes them to ~/.genie/reports/<agent_name>/<YYYYMMDD>_<report_name>.md,
// then stores the summary in the vector store so the agent can use it as
// skill for future runs.
package activityreport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/osutils"
)

const (
	// ActionReport is the cron task action value that triggers the built-in
	// activity report instead of running the agent.
	ActionReport = "genie:report"

	// defaultLookback is the default time window for "recent" activities.
	defaultLookback = 24 * time.Hour
)

// Generator produces activity reports from audit log events and persists
// them to disk and vector memory.
type Generator struct {
	agentName   string
	vectorStore vector.IStore
	lookback    time.Duration
	auditor     audit.Auditor
}

// NewGenerator creates a Generator for the given agent, vector store, and
// auditor. If vectorStore is nil, report content is still written to disk
// but not stored in memory. lookback defines how far back to read
// activities; zero means defaultLookback. auditor is used to read recent
// activities via Recent; if nil, Generate will return an error when building.
func NewGenerator(agentName string, vectorStore vector.IStore, lookback time.Duration, auditor audit.Auditor) *Generator {
	if lookback <= 0 {
		lookback = defaultLookback
	}
	return &Generator{
		agentName:   agentName,
		vectorStore: vectorStore,
		lookback:    lookback,
		auditor:     auditor,
	}
}

// ReportsDir returns the reports directory for the agent:
// ~/.genie/reports/<sanitized_agent_name>/.
func ReportsDir(agentName string) string {
	safe := osutils.SanitizeForFilename(agentName)
	if safe == "" {
		safe = "genie"
	}
	return filepath.Join(osutils.GenieDir(), "reports", safe)
}

// ReportPath returns the path for a report file:
// ~/.genie/reports/<agent_name>/<YYYYMMDD>_<report_name>.md.
func ReportPath(agentName, reportName string, t time.Time) string {
	dir := ReportsDir(agentName)
	safeName := osutils.SanitizeForFilename(reportName)
	if safeName == "" {
		safeName = "report"
	}
	return filepath.Join(dir, t.Format("20060102")+"_"+safeName+".md")
}

// Generate reads recent activities, builds a deterministic summary, writes
// the report to ReportPath(agentName, reportName, at), and upserts the
// summary into the vector store so it is searchable via memory_search.
// Without this method, cron-triggered activity reports would not be produced.
func (g *Generator) Generate(ctx context.Context, reportName string, at time.Time) error {
	logr := logger.GetLogger(ctx).With("fn", "activityreport.Generate", "report", reportName)

	since := at.Add(-g.lookback)
	if g.auditor == nil {
		return fmt.Errorf("read recent activities: no auditor")
	}
	events, err := g.auditor.Recent(ctx, audit.LookupRequest{AgentName: g.agentName, Since: since})
	if err != nil {
		return fmt.Errorf("read recent activities: %w", err)
	}

	summary := buildSummary(events, since, at)
	path := ReportPath(g.agentName, reportName, at)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create reports dir %s: %w", dir, err)
	}
	if err := writeReport(path, summary); err != nil {
		return fmt.Errorf("write report %s: %w", path, err)
	}
	logr.Info("Activity report written", "path", path, "events", len(events))

	if g.vectorStore != nil {
		id := fmt.Sprintf("activity_report:%s_%s", at.Format("20060102"), osutils.SanitizeForFilename(reportName))
		if err := g.vectorStore.Upsert(ctx, vector.BatchItem{
			ID:   id,
			Text: summary,
			Metadata: map[string]string{
				"type":   "activity_report",
				"source": "activity_report",
			},
		}); err != nil {
			logr.Warn("Failed to store report in vector memory", "error", err)
			// Do not fail the whole run; file was written.
		}
	}
	return nil
}

// writeReport writes summary to path. If the file already exists, appends a
// separator and the new summary; otherwise creates the file. This allows
// multiple cron runs per day to accumulate sections in one file.
func writeReport(path, summary string) error {
	content := summary
	_, err := os.Stat(path)
	if err == nil {
		content = "\n\n---\n\n" + summary
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if err == nil {
		flag = os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(content)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// buildSummary produces a markdown summary of events for the given window.
func buildSummary(events []audit.Event, since, until time.Time) string {
	var sb strings.Builder
	sb.WriteString("# Activity Report\n\n")
	_, _ = fmt.Fprintf(&sb, "**Period:** %s to %s (UTC)\n\n", since.Format(time.RFC3339), until.Format(time.RFC3339))
	_, _ = fmt.Fprintf(&sb, "**Total events:** %d\n\n", len(events))

	if len(events) == 0 {
		sb.WriteString("No activity in this window.\n")
		return sb.String()
	}

	// Count by event type.
	countByType := make(map[string]int)
	toolCalls := make(map[string]int)
	var conversationSnippets []string
	for _, e := range events {
		countByType[e.EventType]++
		if e.EventType == "tool_call" {
			toolCalls[e.Action]++
		}
		if e.EventType == "conversation" && e.Action != "" {
			conversationSnippets = append(conversationSnippets, e.Action)
		}
	}

	sb.WriteString("## By event type\n\n")
	for _, k := range sortedKeys(countByType) {
		_, _ = fmt.Fprintf(&sb, "- %s: %d\n", k, countByType[k])
	}

	if len(toolCalls) > 0 {
		sb.WriteString("\n## Tool usage\n\n")
		for _, k := range sortedKeys(toolCalls) {
			_, _ = fmt.Fprintf(&sb, "- %s: %d\n", k, toolCalls[k])
		}
	}

	if len(conversationSnippets) > 0 {
		sb.WriteString("\n## Conversation activity (sample)\n\n")
		max := 10
		if len(conversationSnippets) < max {
			max = len(conversationSnippets)
		}
		for i := 0; i < max; i++ {
			snip := conversationSnippets[i]
			if len(snip) > 200 {
				snip = snip[:197] + "..."
			}
			_, _ = fmt.Fprintf(&sb, "- %s\n", snip)
		}
	}

	sb.WriteString("\n---\n*Generated by Genie activity report. Use this as a skill for future reference.*\n")
	return sb.String()
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
