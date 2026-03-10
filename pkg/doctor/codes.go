// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/

package doctor

// ErrCode is a stable identifier for each diagnostic so users can look up
// resolution steps in the documentation (e.g. docs/data/docs.yml error_codes).
// Every problem reported by genie doctor MUST have an ErrCode.
type ErrCode string

const (
	// Config
	ErrCodeConfigFileMissing    ErrCode = "GENIE_DOC_001"
	ErrCodeConfigFileRead       ErrCode = "GENIE_DOC_002"
	ErrCodeConfigParse          ErrCode = "GENIE_DOC_003"
	ErrCodeConfigUnsupportedExt ErrCode = "GENIE_DOC_004"

	// Secrets (external / [security.secrets])
	ErrCodeSecretUnavailable   ErrCode = "GENIE_DOC_010"
	ErrCodeSecretManagerInit   ErrCode = "GENIE_DOC_011"
	ErrCodeSecretResolveFailed ErrCode = "GENIE_DOC_012"

	// Model config
	ErrCodeModelNoProviders   ErrCode = "GENIE_DOC_020"
	ErrCodeModelMissingToken  ErrCode = "GENIE_DOC_021"
	ErrCodeModelValidateError ErrCode = "GENIE_DOC_022"

	// MCP
	ErrCodeMCPConfigInvalid    ErrCode = "GENIE_DOC_030"
	ErrCodeMCPConnectFailed    ErrCode = "GENIE_DOC_031"
	ErrCodeMCPInitializeFailed ErrCode = "GENIE_DOC_032"
	ErrCodeMCPListToolsFailed  ErrCode = "GENIE_DOC_033"

	// SCM
	ErrCodeSCMNotConfigured   ErrCode = "GENIE_DOC_040"
	ErrCodeSCMProviderInvalid ErrCode = "GENIE_DOC_041"
	ErrCodeSCMTokenMissing    ErrCode = "GENIE_DOC_042"
	ErrCodeSCMInitFailed      ErrCode = "GENIE_DOC_043"
	ErrCodeSCMValidateFailed  ErrCode = "GENIE_DOC_044"

	// Messenger (optional check)
	ErrCodeMessengerConfigInvalid ErrCode = "GENIE_DOC_050"
	ErrCodeMessengerSecretMissing ErrCode = "GENIE_DOC_051"

	// Web search (optional)
	ErrCodeWebSearchConfigInvalid ErrCode = "GENIE_DOC_060"
)
