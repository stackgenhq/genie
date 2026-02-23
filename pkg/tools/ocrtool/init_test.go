package ocrtool

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOCRTool(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OCR Tool Suite")
}
