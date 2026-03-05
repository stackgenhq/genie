package youtubetranscript

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Youtube Internal", func() {
	Describe("extractVideoID", func() {
		It("extracts from youtu.be", func() {
			id, err := extractVideoID("https://youtu.be/dQw4w9WgXcQ")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal("dQw4w9WgXcQ"))

			id, err = extractVideoID("https://youtu.be/dQw4w9WgXcQ?t=10")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal("dQw4w9WgXcQ"))
		})
		It("extracts from youtube.com/watch", func() {
			id, err := extractVideoID("https://www.youtube.com/watch?v=dQw4w9WgXcQ")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal("dQw4w9WgXcQ"))
		})
		It("extracts from youtube.com/embed", func() {
			id, err := extractVideoID("https://www.youtube.com/embed/dQw4w9WgXcQ")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal("dQw4w9WgXcQ"))

			id, err = extractVideoID("https://www.youtube.com/embed/dQw4w9WgXcQ/some/path")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal("dQw4w9WgXcQ"))
		})
		It("fails on invalid url", func() {
			_, err := extractVideoID(":::")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("extractCaptionBaseURL", func() {
		It("extracts baseLang pattern", func() {
			body := []byte(`"baseUrl":"http://example.com/timedtext", "languageCode":"en"`)
			url, err := extractCaptionBaseURL(body, "en")
			Expect(err).NotTo(HaveOccurred())
			Expect(url).To(Equal("http://example.com/timedtext"))
		})
		It("extracts langBase pattern", func() {
			body := []byte(`"languageCode":"es", "baseUrl":"http://example.com/timedtext"`)
			url, err := extractCaptionBaseURL(body, "es")
			Expect(err).NotTo(HaveOccurred())
			Expect(url).To(Equal("http://example.com/timedtext"))
		})
		It("returns first fallback of baseLang pattern if preferLang not matched", func() {
			body := []byte(`"baseUrl":"http://example.com/timedtext", "languageCode":"fr"`)
			url, err := extractCaptionBaseURL(body, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(url).To(Equal("http://example.com/timedtext"))
		})
		It("returns first baseUrl matching timedtext if regex 1 and 2 fail", func() {
			body := []byte(`"baseUrl":"http://example.com/some/timedtext?param=1"`)
			url, err := extractCaptionBaseURL(body, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(url).To(Equal("http://example.com/some/timedtext?param=1"))
		})
		It("returns error if no tracks found", func() {
			body := []byte(`"baseUrl":"http://example.com/"`)
			_, err := extractCaptionBaseURL(body, "")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("parseJSON3Captions", func() {
		It("parses correctly with times", func() {
			body := []byte(`{
				"events": [
					{
						"tStartMs": 65000,
						"segs": [{"utf8": "hello"}]
					}
				]
			}`)
			txt, err := parseJSON3Captions(body, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(txt).To(Equal("[00:01:05] hello"))
		})
		It("parses correctly without times", func() {
			body := []byte(`{
				"events": [
					{
						"tStartMs": 65000,
						"segs": [{"utf8": "hello"}, {"utf8": "world\n"}]
					}
				]
			}`)
			txt, err := parseJSON3Captions(body, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(txt).To(Equal("hello world"))
		})
		It("returns error on invalid json", func() {
			_, err := parseJSON3Captions([]byte(`invalid`), false)
			Expect(err).To(HaveOccurred())
		})
	})
})
