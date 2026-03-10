// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package webfetch

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWebFetch(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Web Fetch Tools Suite")
}
