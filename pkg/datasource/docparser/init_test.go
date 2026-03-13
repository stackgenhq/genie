// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package docparser_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDocParser(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DocParser Suite")
}
