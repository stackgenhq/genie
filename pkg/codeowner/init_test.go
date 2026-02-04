package codeowner

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCodeOwner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CodeOwner Suite")
}
