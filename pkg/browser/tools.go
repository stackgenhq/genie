package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ── Request / Response types ────────────────────────────────────────────

// NavigateRequest is the input for the browser_navigate tool.
type NavigateRequest struct {
	URL string `json:"url" jsonschema:"description=The URL to navigate to,required"`
}

// NavigateResponse is the output for the browser_navigate tool.
type NavigateResponse struct {
	Status string `json:"status"`
	URL    string `json:"url"`
}

// ClickRequest is the input for the browser_click tool.
type ClickRequest struct {
	Selector string `json:"selector" jsonschema:"description=CSS selector of the element to click,required"`
}

// ClickResponse is the output for the browser_click tool.
type ClickResponse struct {
	Status string `json:"status"`
}

// TypeRequest is the input for the browser_type tool.
type TypeRequest struct {
	Selector string `json:"selector" jsonschema:"description=CSS selector of the input element,required"`
	Text     string `json:"text" jsonschema:"description=Text to type into the element,required"`
}

// TypeResponse is the output for the browser_type tool.
type TypeResponse struct {
	Status string `json:"status"`
}

// ReadTextRequest is the input for the browser_read_text tool.
type ReadTextRequest struct {
	Selector string `json:"selector" jsonschema:"description=CSS selector of the element whose visible text to read,required"`
}

// ReadTextResponse is the output for the browser_read_text tool.
type ReadTextResponse struct {
	Text string `json:"text"`
}

// ReadHTMLRequest is the input for the browser_read_html tool.
type ReadHTMLRequest struct {
	Selector string `json:"selector" jsonschema:"description=CSS selector of the element whose outer HTML to read,required"`
}

// ReadHTMLResponse is the output for the browser_read_html tool.
type ReadHTMLResponse struct {
	HTML string `json:"html"`
}

// ScreenshotRequest is the input for the browser_screenshot tool.
type ScreenshotRequest struct {
	Selector string `json:"selector,omitempty" jsonschema:"description=Optional CSS selector to screenshot a specific element. If empty the full viewport is captured."`
}

// ScreenshotResponse is the output for the browser_screenshot tool.
type ScreenshotResponse struct {
	ImageBase64 string `json:"image_base64"`
}

// EvalJSRequest is the input for the browser_eval_js tool.
type EvalJSRequest struct {
	Expression string `json:"expression" jsonschema:"description=JavaScript expression to evaluate in the page context,required"`
}

// EvalJSResponse is the output for the browser_eval_js tool.
type EvalJSResponse struct {
	Result string `json:"result"`
}

// WaitRequest is the input for the browser_wait tool.
type WaitRequest struct {
	Selector    string `json:"selector,omitempty" jsonschema:"description=CSS selector to wait for visibility."`
	Duration    string `json:"duration,omitempty" jsonschema:"description=Duration to wait (e.g. '2s', '500ms')."`
	NetworkIdle bool   `json:"network_idle,omitempty" jsonschema:"description=If true, wait for network (HTML+images+CSS) to be idle."`
}

// WaitResponse is the output for the browser_wait tool.
type WaitResponse struct {
	Status string `json:"status"`
}

// ── Tool constructors ───────────────────────────────────────────────────

// toolSet groups all browser tools so constructors share the same receiver.
// This keeps each tool method tightly scoped while sharing the Browser pointer.
type toolSet struct {
	b *Browser
}

// NewNavigateTool creates the browser_navigate tool. It opens the requested
// URL in the shared browser tab. Without this tool the agent has no way to
// load a web page.
func NewNavigateTool(b *Browser) tool.CallableTool {
	ts := &toolSet{b: b}
	return function.NewFunctionTool(
		ts.navigate,
		function.WithName("browser_navigate"),
		function.WithDescription("Navigate the browser to a URL. This loads the page and waits for the DOM to be ready."),
	)
}

// NewClickTool creates the browser_click tool. It waits for the element to
// become visible and then clicks it. Without this tool the agent cannot
// interact with buttons, links, or other clickable elements.
func NewClickTool(b *Browser) tool.CallableTool {
	ts := &toolSet{b: b}
	return function.NewFunctionTool(
		ts.click,
		function.WithName("browser_click"),
		function.WithDescription("Click an element identified by a CSS selector. Waits for the element to be visible first."),
	)
}

// NewTypeTool creates the browser_type tool. It focuses the element and
// types the given text. Without this tool the agent cannot fill out forms.
func NewTypeTool(b *Browser) tool.CallableTool {
	ts := &toolSet{b: b}
	return function.NewFunctionTool(
		ts.typeText,
		function.WithName("browser_type"),
		function.WithDescription("Type text into an input element identified by a CSS selector. Clears existing value first."),
	)
}

// NewReadTextTool creates the browser_read_text tool. It extracts the visible
// text content of an element. Without this tool the agent cannot read page
// content as plain text.
func NewReadTextTool(b *Browser) tool.CallableTool {
	ts := &toolSet{b: b}
	return function.NewFunctionTool(
		ts.readText,
		function.WithName("browser_read_text"),
		function.WithDescription("Read the visible text content of an element identified by a CSS selector."),
	)
}

// NewReadHTMLTool creates the browser_read_html tool. It returns the outer
// HTML of an element, useful when the agent needs structural information.
// Without this tool the agent can only see text, not the underlying markup.
func NewReadHTMLTool(b *Browser) tool.CallableTool {
	ts := &toolSet{b: b}
	return function.NewFunctionTool(
		ts.readHTML,
		function.WithName("browser_read_html"),
		function.WithDescription("Read the outer HTML of an element identified by a CSS selector."),
	)
}

// NewScreenshotTool creates the browser_screenshot tool. It captures a PNG
// screenshot of the viewport or a specific element and returns it as base64.
// Without this tool the agent has no visual feedback of the page state.
func NewScreenshotTool(b *Browser) tool.CallableTool {
	ts := &toolSet{b: b}
	return function.NewFunctionTool(
		ts.screenshot,
		function.WithName("browser_screenshot"),
		function.WithDescription("Take a PNG screenshot of the browser viewport or a specific element. Returns a base64-encoded image."),
	)
}

// NewEvalJSTool creates the browser_eval_js tool. It evaluates an arbitrary
// JavaScript expression in the page context. This is the escape hatch for
// any interaction that the other tools cannot cover.
func NewEvalJSTool(b *Browser) tool.CallableTool {
	ts := &toolSet{b: b}
	return function.NewFunctionTool(
		ts.evalJS,
		function.WithName("browser_eval_js"),
		function.WithDescription("Evaluate a JavaScript expression in the browser page context and return the result as a string."),
	)
}

// NewWaitTool creates the browser_wait tool. It allows the agent to pause
// execution until a specific condition is met (time, selector visible, or network idle).
func NewWaitTool(b *Browser) tool.CallableTool {
	ts := &toolSet{b: b}
	return function.NewFunctionTool(
		ts.wait,
		function.WithName("browser_wait"),
		function.WithDescription("Wait for a condition: a fixed duration (e.g. '2s'), a CSS selector to be visible, or 'network_idle'."),
	)
}

// AllTools returns every browser tool wired to the given Browser instance.
// This is a convenience function for registering all tools at once.
func AllTools(b *Browser) []tool.CallableTool {
	return []tool.CallableTool{
		NewNavigateTool(b),
		NewClickTool(b),
		NewTypeTool(b),
		NewReadTextTool(b),
		NewReadHTMLTool(b),
		NewScreenshotTool(b),
		NewEvalJSTool(b),
		NewWaitTool(b),
	}
}

// ── Tool implementations ────────────────────────────────────────────────

func (ts *toolSet) navigate(ctx context.Context, req NavigateRequest) (NavigateResponse, error) {
	if req.URL == "" {
		return NavigateResponse{}, fmt.Errorf("url is required")
	}
	if ts.b.isBlockedDomain(req.URL) {
		return NavigateResponse{}, fmt.Errorf("navigation to %q is blocked by the domain blocklist", req.URL)
	}
	if err := ts.b.run(ctx, chromedp.Navigate(req.URL)); err != nil {
		return NavigateResponse{}, fmt.Errorf("navigate failed: %w", err)
	}
	return NavigateResponse{Status: "ok", URL: req.URL}, nil
}

func (ts *toolSet) click(ctx context.Context, req ClickRequest) (ClickResponse, error) {
	if req.Selector == "" {
		return ClickResponse{}, fmt.Errorf("selector is required")
	}
	if err := ts.b.run(ctx,
		chromedp.WaitVisible(req.Selector),
		chromedp.Click(req.Selector),
	); err != nil {
		return ClickResponse{}, fmt.Errorf("click failed: %w", err)
	}
	return ClickResponse{Status: "ok"}, nil
}

func (ts *toolSet) typeText(ctx context.Context, req TypeRequest) (TypeResponse, error) {
	if req.Selector == "" {
		return TypeResponse{}, fmt.Errorf("selector is required")
	}
	if err := ts.b.run(ctx,
		chromedp.WaitVisible(req.Selector),
		chromedp.Clear(req.Selector),
		chromedp.SendKeys(req.Selector, req.Text),
	); err != nil {
		return TypeResponse{}, fmt.Errorf("type failed: %w", err)
	}
	return TypeResponse{Status: "ok"}, nil
}

func (ts *toolSet) readText(ctx context.Context, req ReadTextRequest) (ReadTextResponse, error) {
	if req.Selector == "" {
		return ReadTextResponse{}, fmt.Errorf("selector is required")
	}
	var text string
	if err := ts.b.run(ctx,
		chromedp.WaitReady(req.Selector),
		chromedp.Text(req.Selector, &text),
	); err != nil {
		return ReadTextResponse{}, fmt.Errorf("read text failed: %w", err)
	}
	return ReadTextResponse{Text: text}, nil
}

func (ts *toolSet) readHTML(ctx context.Context, req ReadHTMLRequest) (ReadHTMLResponse, error) {
	if req.Selector == "" {
		return ReadHTMLResponse{}, fmt.Errorf("selector is required")
	}
	var html string
	if err := ts.b.run(ctx,
		chromedp.WaitVisible(req.Selector),
		chromedp.OuterHTML(req.Selector, &html),
	); err != nil {
		return ReadHTMLResponse{}, fmt.Errorf("read html failed: %w", err)
	}
	return ReadHTMLResponse{HTML: html}, nil
}

func (ts *toolSet) screenshot(ctx context.Context, req ScreenshotRequest) (ScreenshotResponse, error) {
	var buf []byte
	if req.Selector != "" {
		if err := ts.b.run(ctx,
			chromedp.WaitVisible(req.Selector),
			chromedp.Screenshot(req.Selector, &buf),
		); err != nil {
			return ScreenshotResponse{}, fmt.Errorf("element screenshot failed: %w", err)
		}
	} else {
		if err := ts.b.run(ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
			return ScreenshotResponse{}, fmt.Errorf("full page screenshot failed: %w", err)
		}
	}
	return ScreenshotResponse{ImageBase64: base64.StdEncoding.EncodeToString(buf)}, nil
}

func (ts *toolSet) evalJS(ctx context.Context, req EvalJSRequest) (EvalJSResponse, error) {
	if req.Expression == "" {
		return EvalJSResponse{}, fmt.Errorf("expression is required")
	}
	var result interface{}
	if err := ts.b.run(ctx, chromedp.Evaluate(req.Expression, &result)); err != nil {
		return EvalJSResponse{}, fmt.Errorf("eval js failed: %w", err)
	}
	// Marshal the result to JSON so the agent can parse complex objects (arrays, dicts)
	// instead of getting Go's fmt.Sprintf representation.
	bytes, err := json.Marshal(result)
	if err != nil {
		return EvalJSResponse{}, fmt.Errorf("failed to marshal eval result: %w", err)
	}
	return EvalJSResponse{Result: string(bytes)}, nil
}

func (ts *toolSet) wait(ctx context.Context, req WaitRequest) (WaitResponse, error) {
	var actions []chromedp.Action

	if req.Selector != "" {
		actions = append(actions, chromedp.WaitVisible(req.Selector))
	}

	if req.Duration != "" {
		d, err := time.ParseDuration(req.Duration)
		if err != nil {
			return WaitResponse{}, fmt.Errorf("invalid duration format: %w", err)
		}
		actions = append(actions, chromedp.Sleep(d))
	}

	if req.NetworkIdle {
		// Wait for the network to be idle (load event fired + no pending requests)
		actions = append(actions, chromedp.WaitReady("body"))
	}

	if len(actions) == 0 {
		return WaitResponse{}, fmt.Errorf("at least one wait condition (selector, duration, network_idle) is required")
	}

	if err := ts.b.run(ctx, actions...); err != nil {
		return WaitResponse{}, fmt.Errorf("wait failed: %w", err)
	}

	return WaitResponse{Status: "ok"}, nil
}
