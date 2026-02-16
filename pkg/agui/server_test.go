package agui_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/agui/aguifakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AG-UI Server", func() {

	Describe("MapEvent", func() {
		threadID := "thread-1"
		runID := "run-1"

		It("should map AgentThinkingMsg to RUN_STARTED", func() {
			event := agui.AgentThinkingMsg{
				Type:      agui.EventRunStarted,
				AgentName: "Genie",
				Message:   "Analyzing...",
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventRunStarted))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["type"]).To(Equal("RUN_STARTED"))
			Expect(parsed["threadId"]).To(Equal(threadID))
			Expect(parsed["runId"]).To(Equal(runID))
		})

		It("should map TextMessageStartMsg to TEXT_MESSAGE_START", func() {
			event := agui.TextMessageStartMsg{
				Type:      agui.EventTextMessageStart,
				MessageID: "msg-1",
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventTextMessageStart))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["messageId"]).To(Equal("msg-1"))
			Expect(parsed["role"]).To(Equal("assistant"))
		})

		It("should map AgentStreamChunkMsg to TEXT_MESSAGE_CONTENT", func() {
			event := agui.AgentStreamChunkMsg{
				Type:      agui.EventTextMessageContent,
				MessageID: "msg-1",
				Content:   "Hello world",
				Delta:     true,
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventTextMessageContent))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["delta"]).To(Equal("Hello world"))
			Expect(parsed["messageId"]).To(Equal("msg-1"))
		})

		It("should map TextMessageEndMsg to TEXT_MESSAGE_END", func() {
			event := agui.TextMessageEndMsg{
				Type:      agui.EventTextMessageEnd,
				MessageID: "msg-1",
			}
			_, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventTextMessageEnd))
		})

		It("should map AgentReasoningMsg to REASONING_MESSAGE_CONTENT", func() {
			event := agui.AgentReasoningMsg{
				Type:    agui.EventReasoningMessageContent,
				Content: "thinking...",
				Delta:   true,
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventReasoningMessageContent))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["delta"]).To(Equal("thinking..."))
		})

		It("should map AgentToolCallMsg to TOOL_CALL_START", func() {
			event := agui.AgentToolCallMsg{
				Type:       agui.EventToolCallStart,
				ToolName:   "read_file",
				ToolCallID: "tc-1",
				Arguments:  `{"path":"/tmp"}`,
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventToolCallStart))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["toolCallId"]).To(Equal("tc-1"))
			Expect(parsed["toolCallName"]).To(Equal("read_file"))
		})

		It("should map ToolCallArgsMsg to TOOL_CALL_ARGS", func() {
			event := agui.ToolCallArgsMsg{
				Type:       agui.EventToolCallArgs,
				ToolCallID: "tc-1",
				Delta:      `{"path":"/tmp"}`,
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventToolCallArgs))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["toolCallId"]).To(Equal("tc-1"))
			Expect(parsed["delta"]).To(Equal(`{"path":"/tmp"}`))
		})

		It("should map ToolCallEndMsg to TOOL_CALL_END", func() {
			event := agui.ToolCallEndMsg{
				Type:       agui.EventToolCallEnd,
				ToolCallID: "tc-1",
			}
			_, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventToolCallEnd))
		})

		It("should map AgentToolResponseMsg to TOOL_CALL_RESULT", func() {
			event := agui.AgentToolResponseMsg{
				Type:       agui.EventToolCallResult,
				ToolCallID: "tc-1",
				ToolName:   "read_file",
				Response:   "file contents here",
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventToolCallResult))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["toolCallId"]).To(Equal("tc-1"))
			Expect(parsed["content"]).To(Equal("file contents here"))
			Expect(parsed["role"]).To(Equal("tool"))
		})

		It("should map AgentCompleteMsg to RUN_FINISHED", func() {
			event := agui.AgentCompleteMsg{
				Type:    agui.EventRunFinished,
				Success: true,
				Message: "Done!",
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventRunFinished))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["threadId"]).To(Equal(threadID))
			Expect(parsed["runId"]).To(Equal(runID))
		})

		It("should map AgentErrorMsg to RUN_ERROR", func() {
			event := agui.AgentErrorMsg{
				Type:    agui.EventRunError,
				Error:   fmt.Errorf("something broke"),
				Context: "during generation",
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventRunError))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["message"]).To(Equal("something broke"))
			Expect(parsed["code"]).To(Equal("during generation"))
		})

		It("should map StageProgressMsg to STEP_STARTED", func() {
			event := agui.StageProgressMsg{
				Type:  agui.EventStepStarted,
				Stage: "Generating",
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventStepStarted))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["stepName"]).To(Equal("Generating"))
		})

		It("should map LogMsg to CUSTOM", func() {
			event := agui.LogMsg{
				Type:    agui.EventCustom,
				Level:   agui.LogInfo,
				Message: "hello world",
				Source:  "test",
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventCustom))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["name"]).To(Equal("log"))
			value := parsed["value"].(map[string]interface{})
			Expect(value["message"]).To(Equal("hello world"))
			Expect(value["source"]).To(Equal("test"))
		})

		It("should map AgentChatMessage to TEXT_MESSAGE_CONTENT", func() {
			event := agui.AgentChatMessage{
				Type:      agui.EventTextMessageContent,
				MessageID: "msg-2",
				Sender:    "bot",
				Message:   "hi there",
			}
			data, eventType, err := agui.MapEvent(event, threadID, runID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventType).To(Equal(agui.EventTextMessageContent))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["delta"]).To(Equal("hi there"))
		})

		It("should return error for unsupported event types", func() {
			_, _, err := agui.MapEvent("not an event", threadID, runID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported event type"))
		})
	})

	Describe("SSEWriter", func() {
		It("should write properly formatted SSE events", func() {
			recorder := httptest.NewRecorder()
			sse, err := agui.NewSSEWriter(recorder)
			Expect(err).NotTo(HaveOccurred())

			err = sse.WriteEvent("RUN_STARTED", []byte(`{"type":"RUN_STARTED"}`))
			Expect(err).NotTo(HaveOccurred())

			body := recorder.Body.String()
			Expect(body).To(ContainSubstring("event: RUN_STARTED\n"))
			Expect(body).To(ContainSubstring(`data: {"type":"RUN_STARTED"}`))
			Expect(body).To(ContainSubstring("\n\n"))
		})

		It("should set correct SSE headers", func() {
			recorder := httptest.NewRecorder()
			_, err := agui.NewSSEWriter(recorder)
			Expect(err).NotTo(HaveOccurred())

			Expect(recorder.Header().Get("Content-Type")).To(Equal("text/event-stream"))
			Expect(recorder.Header().Get("Cache-Control")).To(Equal("no-cache"))
			Expect(recorder.Header().Get("Connection")).To(Equal("keep-alive"))
		})

		It("should write comments for keep-alive", func() {
			recorder := httptest.NewRecorder()
			sse, err := agui.NewSSEWriter(recorder)
			Expect(err).NotTo(HaveOccurred())

			err = sse.WriteComment("ping")
			Expect(err).NotTo(HaveOccurred())

			body := recorder.Body.String()
			Expect(body).To(ContainSubstring(": ping\n\n"))
		})
	})

	Describe("HTTP Endpoint", func() {
		var server *agui.Server

		BeforeEach(func() {
			// Create a server with a simple chat handler that emits a few events
			// Create a server with a simple chat handler that emits a few events
			handler := &aguifakes.FakeExpert{}
			handler.HandleStub = func(ctx context.Context, req agui.ChatRequest) {
				req.EventChan <- agui.TextMessageStartMsg{
					Type:      agui.EventTextMessageStart,
					MessageID: "msg-1",
				}
				req.EventChan <- agui.AgentStreamChunkMsg{
					Type:      agui.EventTextMessageContent,
					MessageID: "msg-1",
					Content:   "Hello, " + req.Message,
					Delta:     true,
				}
				req.EventChan <- agui.TextMessageEndMsg{
					Type:      agui.EventTextMessageEnd,
					MessageID: "msg-1",
				}
			}
			server = agui.ServerConfig{}.NewServer(handler, nil)
		})

		It("should stream SSE events for a valid POST", func() {
			reqBody := `{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"world"}]}`
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get("Content-Type")).To(Equal("text/event-stream"))

			body := recorder.Body.String()
			Expect(body).To(ContainSubstring("event: TEXT_MESSAGE_START"))
			Expect(body).To(ContainSubstring("event: TEXT_MESSAGE_CONTENT"))
			Expect(body).To(ContainSubstring("event: TEXT_MESSAGE_END"))
			Expect(body).To(ContainSubstring("Hello, world"))
		})

		It("should return 400 for malformed JSON", func() {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{not json`))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, req)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 when no user message is provided", func() {
			reqBody := `{"threadId":"t1","runId":"r1","messages":[{"role":"assistant","content":"hi"}]}`
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, req)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})

		It("should generate IDs when not provided", func() {
			reqBody := `{"messages":[{"role":"user","content":"hello"}]}`
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, req)
			// Should succeed even without threadId/runId
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("should respond to health check", func() {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, req)
			Expect(recorder.Code).To(Equal(http.StatusOK))

			var result map[string]string
			Expect(json.NewDecoder(recorder.Body).Decode(&result)).To(Succeed())
			Expect(result["status"]).To(Equal("ok"))
		})

		It("should accept valid events at /api/v1/events", func() {
			reqBody := `{"type":"webhook", "source":"test", "payload":{}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, req)
			Expect(recorder.Code).To(Equal(http.StatusAccepted))

			var result map[string]string
			Expect(json.NewDecoder(recorder.Body).Decode(&result)).To(Succeed())
			Expect(result["status"]).To(Equal("accepted"))
			Expect(result["run_id"]).NotTo(BeEmpty())
		})

		It("should reject invalid events at /api/v1/events", func() {
			reqBody := `{"type":"", "source":""}` // Missing required fields
			req := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, req)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("CORS", func() {
		It("should add CORS headers when origin matches", func() {
			handler := &aguifakes.FakeExpert{}
			server := agui.ServerConfig{CORSOrigins: []string{"http://localhost:3000"}}.NewServer(
				handler, nil,
			)

			req := httptest.NewRequest(http.MethodOptions, "/", nil)
			req.Header.Set("Origin", "http://localhost:3000")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, req)
			Expect(recorder.Header().Get("Access-Control-Allow-Origin")).To(Equal("http://localhost:3000"))
			Expect(recorder.Header().Get("Access-Control-Allow-Methods")).To(ContainSubstring("POST"))
		})

		It("should not add CORS headers when no origins configured", func() {
			handler := &aguifakes.FakeExpert{}
			server := agui.ServerConfig{}.NewServer(
				handler, nil,
			)

			reqBody := `{"messages":[{"role":"user","content":"hello"}]}`
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://evil.example.com")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, req)
			Expect(recorder.Header().Get("Access-Control-Allow-Origin")).To(BeEmpty())
		})
	})

	Describe("Client disconnect", func() {
		It("should stop streaming when context is cancelled", func() {
			// Create a handler that blocks until context is cancelled
			// Create a handler that blocks until context is cancelled
			handler := &aguifakes.FakeExpert{}
			handler.HandleStub = func(ctx context.Context, req agui.ChatRequest) {
				// Send one event, then wait for context cancellation
				req.EventChan <- agui.TextMessageStartMsg{
					Type:      agui.EventTextMessageStart,
					MessageID: "msg-1",
				}
				<-ctx.Done()
			}
			server := agui.ServerConfig{}.NewServer(handler, nil)

			reqBody := `{"messages":[{"role":"user","content":"hello"}]}`
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody)).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			// This should return after context timeout
			done := make(chan struct{})
			go func() {
				server.Handler().ServeHTTP(recorder, req)
				close(done)
			}()

			Eventually(done, 2*time.Second).Should(BeClosed())
		})
	})

	Describe("Full SSE stream parsing", func() {
		It("should produce valid SSE format that can be parsed line by line", func() {
			handler := &aguifakes.FakeExpert{}
			handler.HandleStub = func(ctx context.Context, req agui.ChatRequest) {
				req.EventChan <- agui.AgentStreamChunkMsg{
					Type:      agui.EventTextMessageContent,
					MessageID: "msg-1",
					Content:   "chunk1",
					Delta:     true,
				}
				req.EventChan <- agui.AgentStreamChunkMsg{
					Type:      agui.EventTextMessageContent,
					MessageID: "msg-1",
					Content:   "chunk2",
					Delta:     true,
				}
			}
			server := agui.ServerConfig{}.NewServer(handler, nil)

			reqBody := `{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"test"}]}`
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, req)

			// Parse SSE events from the body
			body := recorder.Body.String()
			events := parseSSEEvents(body)

			// Should have at least 2 TEXT_MESSAGE_CONTENT events
			contentEvents := 0
			for _, evt := range events {
				if evt.eventType == "TEXT_MESSAGE_CONTENT" {
					contentEvents++
				}
			}
			Expect(contentEvents).To(Equal(2))
		})
	})
})

// sseEvent represents a parsed SSE event.
type sseEvent struct {
	eventType string
	data      string
}

// parseSSEEvents parses a raw SSE stream into individual events.
func parseSSEEvents(body string) []sseEvent {
	var events []sseEvent
	var current sseEvent

	reader := strings.NewReader(body)
	buf := make([]byte, 0, 4096)
	readByte := func() (byte, error) {
		b := make([]byte, 1)
		_, err := reader.Read(b)
		return b[0], err
	}

	// Simple line-by-line parser
	for {
		b, err := readByte()
		if err == io.EOF {
			break
		}
		if b == '\n' {
			line := string(buf)
			buf = buf[:0]

			if line == "" {
				// Empty line = end of event
				if current.eventType != "" || current.data != "" {
					events = append(events, current)
					current = sseEvent{}
				}
			} else if strings.HasPrefix(line, "event: ") {
				current.eventType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				current.data = strings.TrimPrefix(line, "data: ")
			}
			// Skip comments (lines starting with :)
		} else {
			buf = append(buf, b)
		}
	}

	return events
}
