package contacts_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestContacts(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Contacts Tool Suite")
}
