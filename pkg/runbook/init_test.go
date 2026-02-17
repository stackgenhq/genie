package runbook

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRunbook(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Runbook Suite")
}
