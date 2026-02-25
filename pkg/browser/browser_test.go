package browser_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/browser"
)

// testPage serves a small HTML page used by every test in this suite.
// It contains a heading, an input field, and a button that sets
// a result paragraph when clicked.
func testPage() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
  <h1 id="heading">Hello Browser</h1>
  <input id="name" type="text" value="" />
  <button id="greet" onclick="document.getElementById('result').innerText='Hi '+document.getElementById('name').value">Greet</button>
  <p id="result"></p>
</body>
</html>`)
	}))
}

var _ = Describe("Browser tools", Ordered, func() {

	var (
		b   *browser.Browser
		srv *httptest.Server
	)

	BeforeAll(func(ctx context.Context) {
		if os.Getenv("CI") == "true" {
			Skip("Skipping browser tests in CI")
		}
		srv = testPage()

		var err error
		b, err = browser.New(context.WithoutCancel(ctx), browser.WithHeadless(true))
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if b != nil {
			b.Close()
		}
		if srv != nil {
			srv.Close()
		}
	})

	It("should navigate to a URL", func(ctx context.Context) {
		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		tool := browser.NewNavigateTool(b)
		res, err := tool.Call(ctx, []byte(fmt.Sprintf(`{"url":%q}`, srv.URL)))
		Expect(err).NotTo(HaveOccurred())
		Expect(fmt.Sprintf("%v", res)).To(ContainSubstring("ok"))
	})

	It("should read text from an element", func(ctx context.Context) {
		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		navTool := browser.NewNavigateTool(b)
		_, err = navTool.Call(ctx, []byte(fmt.Sprintf(`{"url":%q}`, srv.URL)))
		Expect(err).NotTo(HaveOccurred())

		readTool := browser.NewReadTextTool(b)
		res, err := readTool.Call(ctx, []byte(`{"selector":"#heading"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(fmt.Sprintf("%v", res)).To(ContainSubstring("Hello Browser"))
	})

	It("should type text and click a button", func(ctx context.Context) {
		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		navTool := browser.NewNavigateTool(b)
		_, err = navTool.Call(ctx, []byte(fmt.Sprintf(`{"url":%q}`, srv.URL)))
		Expect(err).NotTo(HaveOccurred())

		typeTool := browser.NewTypeTool(b)
		_, err = typeTool.Call(ctx, []byte(`{"selector":"#name","text":"Agent"}`))
		Expect(err).NotTo(HaveOccurred())

		clickTool := browser.NewClickTool(b)
		_, err = clickTool.Call(ctx, []byte(`{"selector":"#greet"}`))
		Expect(err).NotTo(HaveOccurred())

		readTool := browser.NewReadTextTool(b)
		res, err := readTool.Call(ctx, []byte(`{"selector":"#result"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(fmt.Sprintf("%v", res)).To(ContainSubstring("Hi Agent"))
	})

	PIt("should read outer HTML", func(ctx context.Context) {
		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		navTool := browser.NewNavigateTool(b)
		_, err = navTool.Call(ctx, []byte(fmt.Sprintf(`{"url":%q}`, srv.URL)))
		Expect(err).NotTo(HaveOccurred())

		htmlTool := browser.NewReadHTMLTool(b)
		res, err := htmlTool.Call(ctx, []byte(`{"selector":"#heading"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(fmt.Sprintf("%v", res)).To(ContainSubstring("<h1"))
	})

	PIt("should take a full-page screenshot", func(ctx context.Context) {
		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		navTool := browser.NewNavigateTool(b)
		_, err = navTool.Call(ctx, []byte(fmt.Sprintf(`{"url":%q}`, srv.URL)))
		Expect(err).NotTo(HaveOccurred())

		ssTool := browser.NewScreenshotTool(b)
		res, err := ssTool.Call(ctx, []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(fmt.Sprintf("%v", res)).To(ContainSubstring("image_base64"))
	})

	PIt("should evaluate JavaScript", func(ctx context.Context) {
		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		navTool := browser.NewNavigateTool(b)
		_, err = navTool.Call(ctx, []byte(fmt.Sprintf(`{"url":%q}`, srv.URL)))
		Expect(err).NotTo(HaveOccurred())

		evalTool := browser.NewEvalJSTool(b)
		res, err := evalTool.Call(ctx, []byte(`{"expression":"document.title"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(fmt.Sprintf("%v", res)).To(ContainSubstring("Test Page"))
	})

	It("should return all 8 tools from AllTools", func() {
		tools := browser.AllTools(b)
		Expect(tools).To(HaveLen(8))
	})

	// ── Validation edge cases ──────────────────────────────────

	It("should error when navigate URL is empty", func(ctx context.Context) {
		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		tool := browser.NewNavigateTool(b)
		_, err = tool.Call(ctx, []byte(`{"url":""}`))
		Expect(err).To(MatchError(`url is required`))
	})

	It("should error when click selector is empty", func(ctx context.Context) {
		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		tool := browser.NewClickTool(b)
		_, err = tool.Call(ctx, []byte(`{"selector":""}`))
		Expect(err).To(MatchError(`selector is required`))
	})

	It("should error when eval expression is empty", func(ctx context.Context) {
		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		tool := browser.NewEvalJSTool(b)
		_, err = tool.Call(ctx, []byte(`{"expression":""}`))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("Domain blocklist", func() {
	It("should block an exact-match domain", func(ctx context.Context) {
		b, err := browser.New(
			ctx,
			browser.WithHeadless(true),
			browser.WithBlockedDomains([]string{"evil.com"}),
		)
		Expect(err).NotTo(HaveOccurred())
		defer b.Close()

		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		tool := browser.NewNavigateTool(b)
		_, err = tool.Call(ctx, []byte(`{"url":"https://evil.com/page"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("blocked"))
	})

	It("should block a subdomain of a blocked domain", func(ctx context.Context) {
		b, err := browser.New(
			ctx,
			browser.WithHeadless(true),
			browser.WithBlockedDomains([]string{"evil.com"}),
		)
		Expect(err).NotTo(HaveOccurred())
		defer b.Close()

		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		tool := browser.NewNavigateTool(b)
		_, err = tool.Call(ctx, []byte(`{"url":"https://admin.evil.com/login"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("blocked"))
	})

	It("should allow domains not in the blocklist", func(ctx context.Context) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "<html><body>OK</body></html>")
		}))
		defer srv.Close()

		b, err := browser.New(
			ctx,
			browser.WithHeadless(true),
			browser.WithBlockedDomains([]string{"evil.com"}),
		)
		Expect(err).NotTo(HaveOccurred())
		defer b.Close()

		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		tool := browser.NewNavigateTool(b)
		_, err = tool.Call(ctx, []byte(fmt.Sprintf(`{"url":%q}`, srv.URL)))
		Expect(err).NotTo(HaveOccurred())
	})

	It("should match blocked domains case-insensitively", func(ctx context.Context) {
		b, err := browser.New(
			ctx,
			browser.WithHeadless(true),
			browser.WithBlockedDomains([]string{"Evil.COM"}),
		)
		Expect(err).NotTo(HaveOccurred())
		defer b.Close()

		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		tool := browser.NewNavigateTool(b)
		_, err = tool.Call(ctx, []byte(`{"url":"https://EVIL.com/page"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("blocked"))
	})

	It("should not block when blocklist is empty", func(ctx context.Context) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "<html><body>OK</body></html>")
		}))
		defer srv.Close()

		b, err := browser.New(
			ctx,
			browser.WithHeadless(true),
			browser.WithBlockedDomains(nil),
		)
		Expect(err).NotTo(HaveOccurred())
		defer b.Close()

		ctx, cancel, err := b.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer cancel()

		tool := browser.NewNavigateTool(b)
		_, err = tool.Call(ctx, []byte(fmt.Sprintf(`{"url":%q}`, srv.URL)))
		Expect(err).NotTo(HaveOccurred())
	})
})
