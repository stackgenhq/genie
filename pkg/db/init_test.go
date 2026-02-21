package db_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSessionStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SessionStore Suite")
}
