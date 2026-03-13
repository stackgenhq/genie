// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package gdrive

import (
	"context"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/security"
)

func init() {
	datasource.RegisterConnectorFactory("gdrive", gdriveConnectorFactory)
}

func gdriveConnectorFactory(ctx context.Context, opts datasource.ConnectorOptions) datasource.DataSource {
	sp, ok := opts.SecretProvider.(security.SecretProvider)
	if !ok || sp == nil {
		return nil
	}

	credentialsFile := opts.ExtraString["credentials_file"]
	maxDepth := opts.ExtraInt["max_depth"]

	var (
		svc Service
		err error
	)
	if credentialsFile != "" {
		// Build config with the credentials file from ExtraString.
		svc, err = New(ctx, Config{CredentialsFile: credentialsFile, MaxDepth: maxDepth})
	} else {
		svc, err = NewFromSecretProvider(ctx, sp)
	}
	if err != nil {
		return nil
	}
	return NewGDriveConnector(svc, maxDepth)
}
