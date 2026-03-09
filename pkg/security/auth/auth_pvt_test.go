package auth

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getIPAddress (private)", func() {
	newReq := func(remoteAddr string, headers map[string]string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remoteAddr
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r
	}

	DescribeTable("extracts the correct client IP",
		func(remoteAddr string, headers map[string]string, expected string) {
			r := newReq(remoteAddr, headers)
			Expect(getIPAddress(r)).To(Equal(expected))
		},

		// X-Forwarded-For takes highest priority.
		Entry("single X-Forwarded-For value",
			"10.0.0.1:1234", map[string]string{"X-Forwarded-For": "203.0.113.50"}, "203.0.113.50"),
		Entry("multiple X-Forwarded-For values returns first",
			"10.0.0.1:1234", map[string]string{"X-Forwarded-For": "203.0.113.50, 70.41.3.18, 150.172.238.178"}, "203.0.113.50"),
		Entry("X-Forwarded-For with extra whitespace",
			"10.0.0.1:1234", map[string]string{"X-Forwarded-For": "  203.0.113.50 , 70.41.3.18"}, "203.0.113.50"),
		Entry("X-Forwarded-For takes priority over X-Real-Ip",
			"10.0.0.1:1234", map[string]string{
				"X-Forwarded-For": "203.0.113.50",
				"X-Real-Ip":       "198.51.100.10",
			}, "203.0.113.50"),

		// X-Real-Ip is second priority.
		Entry("X-Real-Ip used when no X-Forwarded-For",
			"10.0.0.1:1234", map[string]string{"X-Real-Ip": "198.51.100.10"}, "198.51.100.10"),
		Entry("X-Real-Ip with whitespace",
			"10.0.0.1:1234", map[string]string{"X-Real-Ip": "  198.51.100.10  "}, "198.51.100.10"),
		Entry("whitespace-only X-Real-Ip falls through to RemoteAddr",
			"10.0.0.1:1234", map[string]string{"X-Real-Ip": "   "}, "10.0.0.1"),

		// Falls back to RemoteAddr.
		Entry("RemoteAddr with port",
			"192.168.1.1:4321", nil, "192.168.1.1"),
		Entry("RemoteAddr bare IP (no port)",
			"192.168.1.1", nil, "192.168.1.1"),
		Entry("IPv6 RemoteAddr with port",
			"[::1]:8080", nil, "::1"),
		Entry("IPv6 RemoteAddr without port",
			"::1", nil, "::1"),

		// Edge cases.
		Entry("empty X-Forwarded-For falls through to RemoteAddr",
			"10.0.0.1:9999", map[string]string{"X-Forwarded-For": ""}, "10.0.0.1"),
		Entry("whitespace-only X-Forwarded-For falls through to RemoteAddr",
			"10.0.0.1:9999", map[string]string{"X-Forwarded-For": "   "}, "10.0.0.1"),
	)
})
