// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// sha256Sum returns the SHA-256 hash of data.
func sha256Sum(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// base64URLEncode encodes data using base64 URL encoding without padding (RFC 7636).
func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}
