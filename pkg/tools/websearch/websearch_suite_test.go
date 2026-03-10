// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package websearch_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWebsearch(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Websearch Suite")
}
