package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/osutils"
	"github.com/spf13/cobra"
)

type connectCmd struct {
	rootOpts *rootCmdOption
	url      string
}

func newConnectCommand(rootOpts *rootCmdOption) *cobra.Command {
	c := connectCmd{rootOpts: rootOpts}
	return c.command()
}

func (c *connectCmd) command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to a running Genie AG-UI server",
		Long: `Connect to a running Genie AG-UI HTTP/SSE server and start an
interactive chat session. The server must be started with
"genie grant" before connecting.

Examples:
  # Default (localhost:8080)
  genie connect

  # Custom URL
  genie connect --url http://genie.internal:8080`,
		RunE: c.run,
	}

	cmd.Flags().StringVar(&c.url, "url", osutils.Getenv("GENIE_AGUI_URL", "http://localhost:8080"), "URL of the Genie AG-UI server")

	return cmd
}

func (c *connectCmd) run(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	// Health check — verify the daemon is reachable.
	healthURL := strings.TrimRight(c.url, "/") + "/health"
	resp, err := http.Get(healthURL) //nolint:gosec,noctx
	if err != nil {
		return fmt.Errorf("cannot reach Genie at %s: %w", c.url, err)
	}
	_ = resp.Body.Close()

	baseURL := strings.TrimRight(c.url, "/") + "/"

	fmt.Printf("🧞 Connected to Genie AG-UI server at %s\n", c.url)
	fmt.Println("   Type a message and press Enter. Ctrl+C to quit.")

	threadID := ""
	inputScanner := bufio.NewScanner(os.Stdin)

	for inputScanner.Scan() {
		select {
		case <-ctx.Done():
			fmt.Println("\n🧞 Disconnected.")
			return nil
		default:
		}

		line := strings.TrimSpace(inputScanner.Text())
		if line == "" {
			continue
		}

		input := agui.RunAgentInput{
			ThreadID: threadID,
			Messages: []agui.Message{{Role: "user", Content: line}},
		}
		body, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31mError sending message: %v\033[0m\n", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			fmt.Fprintf(os.Stderr, "\033[31mServer error (%d): %s\033[0m\n", resp.StatusCode, string(respBody))
			continue
		}

		// Read SSE events from the response stream and capture thread ID.
		newThreadID, err := readSSEStream(resp.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31mStream error: %v\033[0m\n", err)
		}
		_ = resp.Body.Close()

		// Preserve thread ID from the first response for conversation continuity.
		if threadID == "" && newThreadID != "" {
			threadID = newThreadID
		}

		fmt.Println() // blank line between responses
	}

	fmt.Println("\n🧞 Input closed.")
	return nil
}

// readSSEStream reads an SSE response body and prints text content deltas
// and run lifecycle events to stdout. Returns the thread ID if found.
func readSSEStream(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	// Increase buffer size to handle reasonably large SSE payloads (e.g. tool outputs) without excessive memory use.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 256*1024)

	var eventType string
	var threadID string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			switch eventType {
			case "TEXT_MESSAGE_CONTENT":
				var payload struct {
					Delta string `json:"delta"`
				}
				if err := json.Unmarshal([]byte(data), &payload); err == nil {
					fmt.Print(payload.Delta)
				}
			case "RUN_STARTED":
				// Attempt to extract thread ID from metadata if available.
				var payload struct {
					ThreadID string `json:"thread_id"`
					ThreadId string `json:"threadId"` // try both casings
				}
				if err := json.Unmarshal([]byte(data), &payload); err == nil {
					if payload.ThreadID != "" {
						threadID = payload.ThreadID
					} else if payload.ThreadId != "" {
						threadID = payload.ThreadId
					}
				}

			case "RUN_FINISHED":
				// Run complete — done streaming.
				return threadID, nil
			case "RUN_ERROR":
				var payload struct {
					Message string `json:"message"`
				}
				if err := json.Unmarshal([]byte(data), &payload); err == nil {
					return threadID, fmt.Errorf("agent error: %s", payload.Message)
				}
			}
		}
	}
	return threadID, scanner.Err()
}
