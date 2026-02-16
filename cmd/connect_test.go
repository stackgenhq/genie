package cmd_test

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/cmd"
)

var _ = Describe("Connect Command", func() {
	var rootOpts *cmd.RootCmdOption

	BeforeEach(func() {
		rootOpts = &cmd.RootCmdOption{}
	})

	Describe("Command Setup", func() {
		It("should create a connect command with correct Use field", func() {
			cobraCmd := cmd.NewConnectCommand(rootOpts)
			Expect(cobraCmd).NotTo(BeNil())
			Expect(cobraCmd.Use).To(Equal("connect"))
		})

		It("should have the url flag", func() {
			cobraCmd := cmd.NewConnectCommand(rootOpts)
			Expect(cobraCmd.Flags().Lookup("url")).NotTo(BeNil())
		})

		Context("with GENIE_AGUI_URL env var", func() {
			BeforeEach(func() {
				os.Setenv("GENIE_AGUI_URL", "http://remote:9090")
			})
			AfterEach(func() {
				os.Unsetenv("GENIE_AGUI_URL")
			})

			It("should use the env var as default url", func() {
				cobraCmd := cmd.NewConnectCommand(rootOpts)
				url := cobraCmd.Flags().Lookup("url")
				Expect(url.DefValue).To(Equal("http://remote:9090"))
			})
		})
	})

	Describe("Run (Connection Failure)", func() {
		It("should return an error when the server is not reachable", func() {
			cobraCmd := cmd.NewConnectCommand(rootOpts)
			// Use a URL that will fail to connect
			cobraCmd.SetArgs([]string{"--url", "http://localhost:1"})

			errCh := make(chan error, 1)
			go func() {
				errCh <- cobraCmd.Execute()
			}()

			Eventually(errCh, 5*time.Second).Should(Receive(HaveOccurred()))
		})
	})
})

var _ = Describe("Grant Command", func() {
	Describe("Command Setup", func() {
		It("should create a grant command with correct Use field", func() {
			g := cmd.NewGrantCommand(&cmd.RootCmdOption{})
			cobraCmd, err := g.Command()
			Expect(err).NotTo(HaveOccurred())
			Expect(cobraCmd.Use).To(Equal("grant"))
		})

		It("should have all expected flags", func() {
			g := cmd.NewGrantCommand(&cmd.RootCmdOption{})
			cobraCmd, err := g.Command()
			Expect(err).NotTo(HaveOccurred())

			expectedFlags := []string{
				"working-dir", "audit-log-path",
			}
			for _, flag := range expectedFlags {
				Expect(cobraCmd.Flags().Lookup(flag)).NotTo(BeNil(), "expected flag %q to be registered", flag)
			}
		})
	})
})
