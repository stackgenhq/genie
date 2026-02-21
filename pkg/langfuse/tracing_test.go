package langfuse

import (
	"context"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Config.startTrace", func() {
	It("should return early without error when credentials are missing", func() {
		cfg := Config{PublicKey: "", SecretKey: "", Host: ""}
		// startTrace should not panic and should return early
		Expect(func() {
			cfg.startTrace(context.Background())
		}).NotTo(Panic())
	})

	It("should return early when only host is set", func() {
		cfg := Config{PublicKey: "pk", SecretKey: "", Host: "example.com"}
		Expect(func() {
			cfg.startTrace(context.Background())
		}).NotTo(Panic())
	})
})

var _ = Describe("Config.Init", func() {
	BeforeEach(func() {
		// Reset the sync.Once so Init can be called in tests
		once = sync.Once{}
		defaultClient = nil
	})

	It("should initialize with empty credentials without panicking", func() {
		cfg := Config{PublicKey: "", SecretKey: "", Host: ""}
		Expect(func() {
			cfg.Init(context.Background())
		}).NotTo(Panic())
		// defaultClient should be set (to noopClient)
		Expect(defaultClient).NotTo(BeNil())
	})
})
