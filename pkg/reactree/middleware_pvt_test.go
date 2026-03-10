// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/tools/toolsfakes"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("Middleware", func() {
	Describe("ValidatingToolWrapper", func() {
		It("Call blocks a denied tool", func() {
			// Arrange
			fakeTool := &toolsfakes.FakeCallableTool{}
			fakeTool.DeclarationReturns(&tool.Declaration{Name: "run_shell"})
			vtw := WrapWithValidator(fakeTool, NewDeterministicValidator([]string{"run_shell"})).(*ValidatingToolWrapper)

			// Act
			_, err := vtw.Call(context.Background(), []byte(`{}`))

			// Assert
			Expect(err).To(MatchError(`action pruned by critic: tool execution prohibited by deterministic policy: tool="run_shell"`))
		})

		It("StreamableCall blocks a denied tool", func() {
			// Arrange
			fakeTool := &toolsfakes.FakeCallableTool{}
			fakeTool.DeclarationReturns(&tool.Declaration{Name: "run_shell"})
			vtw := WrapWithValidator(fakeTool, NewDeterministicValidator([]string{"run_shell"})).(*ValidatingToolWrapper)

			// Act
			_, err := vtw.StreamableCall(context.Background(), []byte(`{}`))

			// Assert
			Expect(err).To(MatchError(`action pruned by critic: tool execution prohibited by deterministic policy: tool="run_shell"`))
		})

		It("StreamableCall returns error for non-streamable tool", func() {
			// Arrange
			fakeTool := &toolsfakes.FakeCallableTool{}
			fakeTool.DeclarationReturns(&tool.Declaration{Name: "read_file"})
			vtw := WrapWithValidator(fakeTool, NewDeterministicValidator(nil)).(*ValidatingToolWrapper)

			// Act
			_, err := vtw.StreamableCall(context.Background(), []byte(`{}`))

			// Assert
			Expect(err).To(MatchError("tool read_file is not streamable"))
		})
	})

	Describe("DryRunToolWrapper", func() {
		It("StreamableCall returns error", func() {
			// Arrange
			fakeTool := &toolsfakes.FakeCallableTool{}
			fakeTool.DeclarationReturns(&tool.Declaration{Name: "run_shell"})

			// Act
			_, err := NewDryRunToolWrapper(fakeTool).StreamableCall(context.Background(), []byte(`{}`))

			// Assert
			Expect(err).To(MatchError("dry-run: streamable calls are not supported in simulation mode"))
		})
	})

	Describe("WrapToolsForDryRun", func() {
		It("wraps all tools", func() {
			// Arrange
			ft1 := &toolsfakes.FakeCallableTool{}
			ft1.DeclarationReturns(&tool.Declaration{Name: "run_shell"})
			ft2 := &toolsfakes.FakeCallableTool{}
			ft2.DeclarationReturns(&tool.Declaration{Name: "save_file"})

			// Act
			wrapped, excludedFn := WrapToolsForDryRun([]tool.Tool{ft1, ft2})

			// Assert
			Expect(wrapped).To(HaveLen(2))
			Expect(excludedFn()).To(BeEmpty())
		})
	})
})
