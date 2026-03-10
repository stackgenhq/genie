// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//
// Change Date: 2029-03-10
// Change License: Apache License, Version 2.0

package reactree

import "trpc.group/trpc-go/trpc-agent-go/tool"

// testToolProvider satisfies tools.ToolProviders for tests.
type testToolProvider struct{ t []tool.Tool }

func (p *testToolProvider) GetTools() []tool.Tool { return p.t }
