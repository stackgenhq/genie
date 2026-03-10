// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/reactree"
)

var _ = Describe("ActionReflector", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("NoOpReflector", func() {
		It("should always return ShouldProceed=true", func() {
			reflector := &reactree.NoOpReflector{}
			result, err := reflector.Reflect(ctx, reactree.ReflectionRequest{
				Goal:           "test goal",
				ProposedOutput: "some output",
				IterationCount: 1,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.ShouldProceed).To(BeTrue())
			Expect(result.Monologue).To(BeEmpty())
		})
	})
})
