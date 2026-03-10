// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package security

import (
	"crypto/tls"
)

// NIST SP 800-131A (2012) minimum key lengths and algorithm requirements
// through 2030. Defaults in this package meet those requirements.
const (
	// MinRSAKeyBits is the minimum RSA key size (bits) per NIST through 2030.
	MinRSAKeyBits = 2048
	// MinECDSAKeyBits is the minimum ECDSA curve size (bits) per NIST through 2030.
	MinECDSAKeyBits = 224
)

// CryptoConfig holds cryptographic policy used by TLS clients and tools.
// Secure development is the only mode: weak algorithms and small key lengths
// are always disabled (NIST 2030 minimums).
type CryptoConfig struct{}

// DefaultCryptoConfig returns a NIST 2030–compliant default (TLS 1.2+, strong ciphers only).
func DefaultCryptoConfig() CryptoConfig {
	return CryptoConfig{}
}

// TLSConfig returns a *tls.Config suitable for TLS clients (HTTP, IMAP, etc.).
// It enforces minimum TLS 1.2 and strong cipher suites only. All selected cipher
// suites use ephemeral key agreement (ECDHE), providing perfect forward secrecy
// so that compromise of a long-term key does not reveal past session keys.
// Callers must not modify the returned config.
func (c CryptoConfig) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: defaultSecureCipherSuites(),
	}
}

// defaultSecureCipherSuites returns TLS 1.2 cipher suites that meet NIST strength
// requirements (no RC4, 3DES, or other weak algorithms). In Go, tls.Config.CipherSuites
// only applies to TLS 1.0–1.2; TLS 1.3 uses Go's default suites, which are already strong.
func defaultSecureCipherSuites() []uint16 {
	return []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	}
}
