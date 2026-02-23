package pkgsearch

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPkgSearch(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Package Search Tools Suite")
}
