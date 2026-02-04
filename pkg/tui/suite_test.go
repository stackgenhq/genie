package tui

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestInternalTUI(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Internal TUI Suite")
}
