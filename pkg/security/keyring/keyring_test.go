package keyring_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/security/keyring"
)

const testAuditAccount = "test_secret_access_audit"

var _ = Describe("KeyringGet", func() {
	AfterEach(func() {
		_ = keyring.KeyringDelete(testAuditAccount)
	})

	It("returns not found when key is missing", func() {
		_, err := keyring.KeyringGet(testAuditAccount)
		Expect(err).To(HaveOccurred())
	})

	It("returns value when key exists", func() {
		err := keyring.KeyringSet(testAuditAccount, []byte("test-value"))
		Expect(err).ToNot(HaveOccurred())

		val, err := keyring.KeyringGet(testAuditAccount)
		Expect(err).ToNot(HaveOccurred())
		Expect(val).To(Equal([]byte("test-value")))
	})
})
