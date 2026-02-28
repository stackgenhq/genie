// Package browser provides a chromedp-backed browser automation provider.
// It exposes small, composable trpc-agent tools that an AI agent can invoke
// to navigate pages, interact with elements, extract content, and take
// screenshots. Without this package the agent has no way to observe or
// manipulate live web pages.
package browser

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// defaultTimeout is the maximum duration for any single browser action.
const defaultTimeout = 1 * time.Minute

// Config holds configuration for the browser tool provider.
// BlockedDomains prevents the agent from navigating to specific domains
// (e.g. internal admin panels, payment processors). Matching is suffix-based
// so "example.com" also blocks "sub.example.com".
type Config struct {
	BlockedDomains []string `yaml:"blocked_domains,omitempty" toml:"blocked_domains,omitempty"`
}

// Option configures a Browser instance.
type Option func(*browserOpts)

// browserOpts holds configuration applied when creating a Browser.
type browserOpts struct {
	headless       bool
	timeout        time.Duration
	blockedDomains []string
	width, height  int
}

// WithHeadless controls whether the browser runs without a visible window.
// It defaults to true. Setting this to false is useful during local debugging.
func WithHeadless(v bool) Option {
	return func(o *browserOpts) { o.headless = v }
}

// WithTimeout overrides the default per-action timeout of 30 seconds.
func WithTimeout(d time.Duration) Option {
	return func(o *browserOpts) { o.timeout = d }
}

// WithBlockedDomains sets domains that the browser is not allowed to navigate to.
// Matching is suffix-based: "example.com" blocks both "example.com" and
// "sub.example.com". This is a safety measure to prevent the agent from
// accessing sensitive internal services.
func WithBlockedDomains(domains []string) Option {
	return func(o *browserOpts) { o.blockedDomains = domains }
}

// WithViewport sets the browser window size.
func WithViewport(width, height int) Option {
	return func(o *browserOpts) {
		o.width = width
		o.height = height
	}
}

// Browser manages a shared chromedp browser session. All tools operate on
// the same browser tab so that navigation state is preserved across calls.
// Without this struct every tool call would launch a new browser, losing
// cookies, logins, and page context.
type Browser struct {
	allocCancel    context.CancelFunc
	ctxCancel      context.CancelFunc
	ctx            context.Context
	timeout        time.Duration
	blockedDomains []string
}

// New allocates a new Chrome browser process (headless by default) and returns
// a Browser that tools can share. Callers MUST call Close when finished to
// avoid leaking Chrome processes.
func New(ctx context.Context, opts ...Option) (*Browser, error) {
	cfg := browserOpts{
		headless: true,
		timeout:  defaultTimeout,
		width:    1920,
		height:   1080,
	}
	for _, o := range opts {
		o(&cfg)
	}

	allocOpts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", cfg.headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true), // Prevent crashes in containerized environments
		chromedp.WindowSize(cfg.width, cfg.height),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx)

	// Run an empty action to ensure the browser actually starts.
	if err := chromedp.Run(ctx); err != nil {
		allocCancel()
		ctxCancel()
		return nil, fmt.Errorf("failed to start browser: %w", err)
	}

	return &Browser{
		allocCancel:    allocCancel,
		ctxCancel:      ctxCancel,
		ctx:            ctx,
		timeout:        cfg.timeout,
		blockedDomains: cfg.blockedDomains,
	}, nil
}

// NewTab creates a new isolated browser context (tab). The caller is responsible
// for cancelling the returned context to close the tab. The tab will also be
// closed if the underlying browser context is cancelled (for example, via Close).
//
// Note: The 'parent' argument is currently ignored for the purpose of browser
// inheritance to ensure the tab belongs to this Browser instance. If you need
// to tie the tab to an existing context's lifecycle, wrap the returned context
// with context.WithCancel/WithTimeout using your parent context as the reference
// (though hooking them up directly is not supported by chromedp structure).
func (b *Browser) NewTab(parent context.Context) (context.Context, context.CancelFunc, error) {
	if b == nil || b.ctx == nil {
		// Browser is not properly initialized; avoid creating a context that
		// is not tied to a running browser process.
		return nil, func() {}, fmt.Errorf("browser not initialized")
	}

	// Create a new target (tab) derived from the shared browser context (b.ctx).
	// This ensures we reuse the browser process managed by b.
	tabCtx, tabCancel := chromedp.NewContext(b.ctx)

	// Eagerly initialize the tab by running an empty action. Without this,
	// chromedp lazily creates the target on first use, which can cause
	// confusing timeouts if the browser process has issues.
	if err := chromedp.Run(tabCtx); err != nil {
		tabCancel()
		return nil, func() {}, fmt.Errorf("failed to initialize browser tab: %w", err)
	}

	// Propagate parent cancellation to the tab. chromedp contexts form their
	// own hierarchy (rooted at the allocator), so tabCtx isn't a child of
	// parent. We bridge the gap with a goroutine that cancels the tab when
	// the parent is done.
	go func() {
		select {
		case <-parent.Done():
			tabCancel()
		case <-tabCtx.Done():
			// Tab was closed independently (e.g. by the caller calling tabCancel).
		}
	}()

	return tabCtx, tabCancel, nil
}

// Close tears down the browser process and releases all resources.
// It is safe to call multiple times.
func (b *Browser) Close() {
	if b.ctxCancel != nil {
		b.ctxCancel()
	}
	if b.allocCancel != nil {
		b.allocCancel()
	}
}

// run executes a set of chromedp actions. It is the single entry-point used by
// every tool to interact with the browser.
//
// The caller's context (ctx) is used only for cancellation signaling.
// All chromedp operations are executed on a context derived from b.ctx,
// which carries the CDP executor / browser session. This guarantees that
// even if the caller passes a plain context.Background(), the browser
// bindings are always available.
func (b *Browser) run(ctx context.Context, actions ...chromedp.Action) error {
	// Determine the effective timeout.
	// If the caller has a deadline that is shorter than b.timeout, honour it.
	// Otherwise fall back to the configured default.
	timeout := b.timeout
	if dl, ok := ctx.Deadline(); ok {
		if remaining := time.Until(dl); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}

	// Use the caller's context as the parent if it already carries a
	// chromedp executor (e.g. from NewTab). Otherwise, fall back to b.ctx.
	// This ensures we don't accidentally run actions against the browser
	// context instead of the specific tab context.
	parentCtx := b.ctx
	if chromedp.FromContext(ctx) != nil {
		parentCtx = ctx
	}

	runCtx, runCancel := context.WithTimeout(parentCtx, timeout)
	defer runCancel()

	// Bridge cancellation: if the caller's context is cancelled (e.g. the
	// RPC was aborted), propagate that to the browser action.
	go func() {
		select {
		case <-ctx.Done():
			runCancel()
		case <-runCtx.Done():
		}
	}()

	return chromedp.Run(runCtx, actions...)
}

// isBlockedDomain returns true when the given raw URL's host matches any
// entry in the blocklist. Matching is suffix-based so "example.com" also
// blocks "sub.example.com".
func (b *Browser) isBlockedDomain(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true // Fail safe: block invalid URLs
	}
	host := strings.ToLower(parsed.Hostname())
	for _, blocked := range b.blockedDomains {
		blocked = strings.ToLower(strings.TrimSpace(blocked))
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}
	return false
}
