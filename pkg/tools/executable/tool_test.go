// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package executable_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/security/securityfakes"
	"github.com/stackgenhq/genie/pkg/tools/executable"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("Executable Tool", func() {
	var (
		ctx context.Context
		sp  *securityfakes.FakeSecretProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		sp = &securityfakes.FakeSecretProvider{}
		// Basic stub matching anything returning "secret_val"
		sp.GetSecretReturns("secret_val", nil)
	})

	Describe("Configs.Tools", func() {
		It("should skip tools with missing name or binary", func() {
			configs := executable.Configs{
				{Name: "", Binary: "echo"},
				{Name: "valid", Binary: ""},
			}
			tools := configs.Tools(ctx, sp)
			Expect(tools).To(BeEmpty())
		})

		It("should initialize valid tools", func() {
			configs := executable.Configs{
				{Name: "execute_echo", Binary: "echo"},
			}
			tools := configs.Tools(ctx, sp)
			Expect(tools).To(HaveLen(1))
			decl := tools[0].Declaration()
			Expect(decl.Name).To(Equal("execute_echo"))
		})
	})

	Describe("Config.Tool", func() {
		It("initializes and validates secrets", func() {
			cfg := executable.Config{
				Name:   "execute_echo",
				Binary: "echo",
				Env: []executable.EnvVar{
					{Key: "STATIC", Value: "val"},
					{Key: "DYNAMIC", Secret: "my_secret"},
				},
			}
			t, err := cfg.Tool(ctx, sp)
			Expect(err).NotTo(HaveOccurred())
			Expect(t).NotTo(BeNil())

			Expect(sp.GetSecretCallCount()).To(Equal(1))
			_, req := sp.GetSecretArgsForCall(0)
			Expect(req.Name).To(Equal("my_secret"))
		})

		It("returns error if secret validation fails", func() {
			sp.GetSecretReturns("", fmt.Errorf("vault unavailable"))

			cfg := executable.Config{
				Name:   "execute_echo",
				Binary: "echo",
				Env: []executable.EnvVar{
					{Key: "DYNAMIC", Secret: "bad_secret"},
				},
			}
			_, err := cfg.Tool(ctx, sp)
			Expect(err).To(MatchError(ContainSubstring("failed to validate secret \"bad_secret\" for env var \"DYNAMIC\": vault unavailable")))
		})

		It("returns error if required secret is missing", func() {
			sp.GetSecretReturns("", nil)

			cfg := executable.Config{
				Name:   "execute_echo",
				Binary: "echo",
				Env: []executable.EnvVar{
					{Key: "DYNAMIC", Secret: "missing_secret"},
				},
			}
			_, err := cfg.Tool(ctx, sp)
			Expect(err).To(MatchError(ContainSubstring("missing required secret \"missing_secret\" for env var \"DYNAMIC\"")))
		})
	})

	Describe("executableTool.Call", func() {
		var (
			toolCall func(args string) (any, error)
			cfg      executable.Config
		)

		BeforeEach(func() {
			cfg = executable.Config{
				Name:   "execute_env",
				Binary: "env",
				Env: []executable.EnvVar{
					{Key: "TEST_FLAG", Value: "123"},
				},
			}

			toolCall = func(cmd string) (any, error) {
				t, err := cfg.Tool(ctx, sp)
				Expect(err).NotTo(HaveOccurred())

				argsBytes, _ := json.Marshal(map[string]string{"command": cmd})
				return t.(tool.CallableTool).Call(ctx, argsBytes)
			}
		})

		It("rejects empty commands", func() {
			_, err := toolCall("   ")
			Expect(err).To(MatchError("command is required"))
		})

		It("rejects metacharacters", func() {
			disallowed := []string{";", "|", "&", "`", "$", "(", ")", "{", "}", "!", ">", "<", "\n", "#", "\\"}
			for _, char := range disallowed {
				_, err := toolCall("hello " + char)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("disallowed shell metacharacters"))
			}
		})

		It("executes the binary successfully and includes the env variable", func() {
			// Using `env` tool we started with to print variables.
			out, err := toolCall("-0")
			Expect(err).NotTo(HaveOccurred())

			output := out.(string)
			Expect(output).To(ContainSubstring("TEST_FLAG=123"))
		})

		Context("secret resolution fails during call", func() {
			It("returns an error if GetSecret fails", func() {
				cfg = executable.Config{
					Name:   "execute_env",
					Binary: "env",
					Env: []executable.EnvVar{
						{Key: "TEST_FLAG", Secret: "bad_secret"},
					},
				}

				sp.GetSecretReturnsOnCall(0, "mock_value", nil)
				sp.GetSecretReturnsOnCall(1, "", fmt.Errorf("vault unavailable"))

				t, err := cfg.Tool(ctx, sp)
				Expect(err).NotTo(HaveOccurred())

				argsBytes, _ := json.Marshal(map[string]string{"command": "foo"})
				_, err = t.(tool.CallableTool).Call(ctx, argsBytes)

				Expect(err).To(MatchError(ContainSubstring("failed to get secret \"bad_secret\" for env var \"TEST_FLAG\": vault unavailable")))
			})
		})
	})
})
