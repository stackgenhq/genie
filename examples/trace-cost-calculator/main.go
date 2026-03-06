// trace-cost-calculator connects to the Langfuse API and fetches usage
// statistics for the "devops-copilot" agent using the langfuse.Client from
// pkg/langfuse. It reads credentials from environment variables.
//
// Usage:
//
//	# From repo root:
//	source .env && go run -mod=mod ./examples/trace-cost-calculator/main.go
//
//	# Or with a custom agent name:
//	source .env && go run -mod=mod ./examples/trace-cost-calculator/main.go reactree.adaptive_loop
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/langfuse"
)

func main() {

	cfg := langfuse.Config{
		PublicKey: os.Getenv("LANGFUSE_PUBLIC_KEY"),
		SecretKey: os.Getenv("LANGFUSE_SECRET_KEY"),
		Host:      os.Getenv("LANGFUSE_HOST"),
	}

	if cfg.PublicKey == "" || cfg.SecretKey == "" || cfg.Host == "" {
		fmt.Fprintln(os.Stderr, "❌ LANGFUSE_PUBLIC_KEY, LANGFUSE_SECRET_KEY, and LANGFUSE_HOST must be set.")
		fmt.Fprintln(os.Stderr, "   Either export them or create a .env file in the repo root.")
		os.Exit(1)
	}

	client := cfg.NewClient()
	ctx := context.Background()

	agentName := "devops-copilot"
	if len(os.Args) > 1 {
		agentName = os.Args[1]
	}

	// Query windows to show.
	windows := []struct {
		Label    string
		Duration time.Duration
	}{
		{"Last 1 hour", 1 * time.Hour},
		{"Last 24 hours", 24 * time.Hour},
		{"Last 7 days", 7 * 24 * time.Hour},
		{"Last 30 days", 30 * 24 * time.Hour},
	}

	fmt.Println(strings.Repeat("═", 70))
	fmt.Printf("  🤖 Agent Usage Stats: %s\n", agentName)
	fmt.Printf("  🔗 Langfuse Host:     %s\n", cfg.Host)
	fmt.Printf("  ⏰ Queried at:        %s\n", time.Now().Format(time.RFC3339))
	fmt.Println(strings.Repeat("═", 70))

	for _, w := range windows {
		stats, err := client.GetAgentStats(ctx, langfuse.GetAgentStatsRequest{
			Duration:  w.Duration,
			AgentName: agentName,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n❌ Error fetching stats for %s: %v\n", w.Label, err)
			continue
		}

		fmt.Printf("\n📊 %s\n", w.Label)
		fmt.Println(strings.Repeat("─", 70))

		if len(stats) == 0 {
			fmt.Println("  (no data)")
			continue
		}

		fmt.Printf("  %-30s %12s %12s %12s\n", "AGENT", "TOTAL COST", "TOKENS", "CALLS")
		fmt.Println(strings.Repeat("─", 70))

		for _, s := range stats {
			fmt.Printf("  %-30s $%11.6f %12.0f %12.0f\n",
				truncate(s.AgentName, 30), s.TotalCost, s.TotalTokens, s.Count)

			fmt.Printf("    ├─ Input tokens:  %12.0f\n", s.InputTokens)
			fmt.Printf("    └─ Output tokens: %12.0f\n", s.OutputTokens)
		}
	}

	// Also show all agents for context (last 24h).
	fmt.Printf("\n\n")
	fmt.Println(strings.Repeat("═", 70))
	fmt.Println("  📋 All Agents (Last 24 hours)")
	fmt.Println(strings.Repeat("═", 70))

	allStats, err := client.GetAgentStats(ctx, langfuse.GetAgentStatsRequest{
		Duration: 24 * time.Hour,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error fetching all agent stats: %v\n", err)
		os.Exit(1)
	}

	if len(allStats) == 0 {
		fmt.Println("  (no data)")
		return
	}

	fmt.Printf("  %-35s %12s %12s %8s\n", "AGENT", "TOTAL COST", "TOKENS", "CALLS")
	fmt.Println(strings.Repeat("─", 70))

	var grandTotal float64
	for _, s := range allStats {
		fmt.Printf("  %-35s $%11.6f %12.0f %8.0f\n",
			truncate(s.AgentName, 35), s.TotalCost, s.TotalTokens, s.Count)
		grandTotal += s.TotalCost
	}
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("  %-35s $%11.6f\n", "GRAND TOTAL", grandTotal)
	fmt.Println(strings.Repeat("═", 70))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
