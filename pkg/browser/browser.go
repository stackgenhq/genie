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
const defaultTimeout = 30 * time.Second

// Config holds configuration for the browser tool provider.
// BlockedDomains prevents the agent from navigating to specific domains
// (e.g. internal admin panels, payment processors). Matching is suffix-based
// so "example.com" also blocks "sub.example.com".
type Config struct {
	BlockedDomains []string `yaml:"blocked_domains" toml:"blocked_domains"`
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
func New(opts ...Option) (*Browser, error) {
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
		chromedp.WindowSize(cfg.width, cfg.height),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
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
// for cancelling the context to close the tab.
func (b *Browser) NewTab(ctx context.Context) (context.Context, context.CancelFunc, error) {
	// Create a new context derived from the browser's allocator context (which is b.ctx)
	// This ensures the new tab shares the same browser process but has its own target.
	// Note: b.ctx here is the one created with NewContext(allocCtx), which represents the "first" tab
	// or the browser/process level context depending on how it was set up.
	// To cleanly spawn new tabs, we should use the *allocator* context or the existing browser context.
	// Using b.ctx works fine; it just spawns a new target sharing the browser.

	// Create a new target (tab)
	ctx, cancel := chromedp.NewContext(b.ctx)
	return ctx, cancel, nil
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

// run executes a set of chromedp actions with the configured timeout. It is
// the single entry-point used by every tool to interact with the browser.
// It accepts a context so that tools can operate on specific tabs/targets.
func (b *Browser) run(ctx context.Context, actions ...chromedp.Action) error {
	// If the context is already a chromedp context, we just need to add a timeout.
	// If it's a standard context, we might need to derive it from the browser's allocator (not typical for this usage).
	// In our design, tools will pass a context associated with a specific tab.
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	return chromedp.Run(ctx, actions...)
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
