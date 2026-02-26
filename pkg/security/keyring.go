/*
Copyright © 2026 StackGen, Inc.
*/

// Package security: device keychain storage for arbitrary key-value secrets.
// Uses macOS Keychain, Windows Credential Manager, or Linux Secret Service
// (via go-keyring). Callers use a logical service name and account (key) to
// store and retrieve values without writing token files to disk.
package security

import (
	"github.com/zalando/go-keyring"
)

// KeyringGet returns the value stored in the system keychain for the given
// service and account (key). Returns (nil, err) when the entry is not found
// or the keyring is unavailable (e.g. headless Linux without Secret Service).
func KeyringGet(service, account string) ([]byte, error) {
	s, err := keyring.Get(service, account)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// KeyringSet stores a value in the system keychain for the given service and
// account (key). Safe to call when the keyring is unavailable; returns an
// error in that case.
func KeyringSet(service, account string, value []byte) error {
	return keyring.Set(service, account, string(value))
}

// KeyringDelete removes the stored value for the given service and account
// (e.g. on sign-out or key rotation).
func KeyringDelete(service, account string) error {
	return keyring.Delete(service, account)
}
