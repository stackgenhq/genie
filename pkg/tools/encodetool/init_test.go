package encodetool

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEncode(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Encode Tools Suite")
}
