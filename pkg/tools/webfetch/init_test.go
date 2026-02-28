package webfetch

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWebFetch(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Web Fetch Tools Suite")
}
