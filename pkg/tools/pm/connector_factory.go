// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package pm

import (
	"context"

	"github.com/stackgenhq/genie/pkg/datasource"
)

func init() {
	datasource.RegisterConnectorFactory("linear", linearConnectorFactory)
}

func linearConnectorFactory(ctx context.Context, opts datasource.ConnectorOptions) datasource.DataSource {
	cfg := Config{
		Provider: ProviderLinear,
		APIToken: opts.ExtraString["pm_api_token"],
		BaseURL:  opts.ExtraString["pm_base_url"],
	}
	if cfg.APIToken == "" {
		return nil
	}
	svc, err := New(cfg)
	if err != nil {
		return nil
	}
	return NewLinearConnector(svc)
}
