package browser_test

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = SynchronizedBeforeSuite(func() {
	if os.Getenv("CI") == "true" {
		Skip("Skipping browser tests in CI")
	}

}, func(_ []byte) {

})

func TestBrowser(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Browser Suite")
}
