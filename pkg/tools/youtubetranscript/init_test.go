package youtubetranscript_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestYoutubetranscript(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "YouTube Transcript Tool Suite")
}
