// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package oauth_test

import (
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/tools/google/oauth"
)

var _ = ginkgo.Describe("AG-UI port constant", func() {
	ginkgo.It("DefaultGenieAGUIPort matches messenger.DefaultAGUIPort to avoid configuration drift", func() {
		// DefaultGenieAGUIPort is used in browser_flow.go; we cannot import messenger there (circular dep).
		Expect(uint32(oauth.DefaultGenieAGUIPort)).To(Equal(messenger.DefaultAGUIPort))
	})
})
