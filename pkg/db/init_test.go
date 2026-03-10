// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package db_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSessionStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SessionStore Suite")
}
