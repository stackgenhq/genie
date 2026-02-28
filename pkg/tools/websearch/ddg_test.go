package websearch_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/websearch"
)

// sampleDDGHTML is a minimal mock of the DuckDuckGo HTML search response.
const sampleDDGHTML = `
<html>
<body>
<div class="results">
  <div class="result">
    <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fnews&amp;rut=abc">Example News Site</a>
    <a class="result__url" href="https://example.com/news">example.com/news</a>
    <a class="result__snippet" href="https://example.com/news">Breaking <b>news</b> and latest headlines from around the world.</a>
  </div>
  <div class="result">
    <a rel="nofollow" class="result__a" href="https://other.com/article">Other Article &amp; More</a>
    <a class="result__url" href="https://other.com/article">other.com/article</a>
    <a class="result__snippet" href="https://other.com/article">An interesting article about today&#x27;s events.</a>
  </div>
</div>
</body>
</html>
`

const emptyDDGHTML = `
<html>
<body>
<div class="results">
  <div class="no-results">No results found</div>
</div>
</body>
</html>
`

var _ = Describe("DuckDuckGo HTML Search Tool", func() {
	Context("NewDDGTool", func() {
		It("should create a tool with the correct name", func() {
			t := websearch.NewDDGTool()
			Expect(t.Declaration().Name).To(Equal("duckduckgo_search"))
		})
	})

	Context("Search", func() {
		var (
			server *httptest.Server
			tool   func(ctx context.Context, input []byte) (any, error)
		)

		AfterEach(func() {
			if server != nil {
				server.Close()
			}
		})

		It("should parse results from valid HTML", func(ctx context.Context) {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodPost))
				Expect(r.Header.Get("Content-Type")).To(Equal("application/x-www-form-urlencoded"))
				fmt.Fprint(w, sampleDDGHTML)
			}))

			t := websearch.NewDDGTool(
				websearch.WithDDGEndpoint(server.URL),
				websearch.WithDDGHTTPClient(server.Client()),
			)
			tool = t.Call

			res, err := tool(ctx, []byte(`{"query":"latest news"}`))
			Expect(err).NotTo(HaveOccurred())

			text := res.(string)
			Expect(text).To(ContainSubstring("Example News Site"))
			Expect(text).To(ContainSubstring("https://example.com/news"))
			Expect(text).To(ContainSubstring("Breaking news"))
			Expect(text).To(ContainSubstring("Other Article & More"))
			Expect(text).To(ContainSubstring("https://other.com/article"))
			Expect(text).To(ContainSubstring("today's events"))
		})

		It("should return no-results message for empty HTML", func(ctx context.Context) {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, emptyDDGHTML)
			}))

			t := websearch.NewDDGTool(
				websearch.WithDDGEndpoint(server.URL),
				websearch.WithDDGHTTPClient(server.Client()),
			)

			res, err := t.Call(ctx, []byte(`{"query":"xyznonexistent123"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("No results found"))
		})

		It("should return error on empty query", func(ctx context.Context) {
			t := websearch.NewDDGTool()
			_, err := t.Call(ctx, []byte(`{"query":""}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("empty search query"))
		})

		It("should return error on HTTP failure", func(ctx context.Context) {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			}))

			t := websearch.NewDDGTool(
				websearch.WithDDGEndpoint(server.URL),
				websearch.WithDDGHTTPClient(server.Client()),
			)

			_, err := t.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("HTTP 403"))
		})

		It("should resolve DuckDuckGo redirect URLs", func(ctx context.Context) {
			redirectHTML := `
			<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2Fdoc&amp;rut=xyz">Go Docs</a>
			<a class="result__snippet" href="#">The Go programming language docs.</a>
			`
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, redirectHTML)
			}))

			t := websearch.NewDDGTool(
				websearch.WithDDGEndpoint(server.URL),
				websearch.WithDDGHTTPClient(server.Client()),
			)

			res, err := t.Call(ctx, []byte(`{"query":"golang"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("https://golang.org/doc"))
		})

		It("should retry on HTTP 202 and succeed when server returns 200", func(ctx context.Context) {
			var attempt int32
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempt++
				if attempt <= 1 { // 202 on first attempt, 200 on retry
					w.WriteHeader(http.StatusAccepted)
					return
				}
				fmt.Fprint(w, sampleDDGHTML)
			}))

			t := websearch.NewDDGTool(
				websearch.WithDDGEndpoint(server.URL),
				websearch.WithDDGHTTPClient(server.Client()),
			)

			res, err := t.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("Example News Site"))
			Expect(attempt).To(BeNumerically("==", 2)) // 1 retry + 1 success
		})

		It("should fail after max retries on persistent HTTP 202", func(ctx context.Context) {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			}))

			t := websearch.NewDDGTool(
				websearch.WithDDGEndpoint(server.URL),
				websearch.WithDDGHTTPClient(server.Client()),
			)

			_, err := t.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("giving up"))
		})
	})
})
