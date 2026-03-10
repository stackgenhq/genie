// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ocrtool

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOCRTool(t *testing.T) {
	t.Parallel()
	if err := canBootstrap(); err != nil {
		t.Skip("Skipping OCR tool tests: tesseract not installed")
		return
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "OCR Tool Suite")
}
