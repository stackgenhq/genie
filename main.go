// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/stackgenhq/genie/cmd"
)

func main() {
	err := cmd.NewRootCommand().ExecuteContext(context.Background())
	if err != nil {
		fmt.Println("error", err)
		os.Exit(1)
	}
}
