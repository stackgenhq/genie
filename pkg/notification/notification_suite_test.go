// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package notification_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNotification(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Notification Suite")
}
