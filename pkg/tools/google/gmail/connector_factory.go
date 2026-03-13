// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package gmail

import (
	"context"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/security"
)

func init() {
	datasource.RegisterConnectorFactory("gmail", gmailConnectorFactory)
}

func gmailConnectorFactory(ctx context.Context, opts datasource.ConnectorOptions) datasource.DataSource {
	sp, ok := opts.SecretProvider.(security.SecretProvider)
	if !ok || sp == nil {
		return nil
	}
	svc, err := NewFromSecretProvider(ctx, sp)
	if err != nil {
		return nil
	}
	return NewGmailConnector(svc)
}
