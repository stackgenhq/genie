// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package codeskim

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCodeSkim(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Code Skim Tools Suite")
}
