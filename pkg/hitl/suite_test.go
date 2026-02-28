package hitl_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHITL(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HITL Suite")
}
