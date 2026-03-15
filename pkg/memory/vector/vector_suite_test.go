// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package vector_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sdktracetest "go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// exporter is a shared in-memory span exporter for tests that need to assert
// on recorded OTel span attributes (e.g. the empty-retrieval tag test).
// It is reset before each spec to avoid cross-test pollution.
var exporter *sdktracetest.InMemoryExporter

func TestVector(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Vector Suite")
}

var _ = BeforeEach(func() {
	exporter = sdktracetest.NewInMemoryExporter()
})
