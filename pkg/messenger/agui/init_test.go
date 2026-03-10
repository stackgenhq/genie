// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAGUI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Messenger AGUI Suite")
}
