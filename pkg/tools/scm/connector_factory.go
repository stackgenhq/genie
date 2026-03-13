// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scm

import (
	"context"

	"github.com/stackgenhq/genie/pkg/datasource"
)

func RegisterDataSource(svc Service) {
	datasource.RegisterConnectorFactory(svc.Provider(), func(ctx context.Context, opts datasource.ConnectorOptions) datasource.DataSource {
		return NewSCMConnector(svc)
	})
}
