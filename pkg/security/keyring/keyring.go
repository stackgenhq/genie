// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/

// Package keyring: device keychain storage for arbitrary key-value secrets.
// Uses macOS Keychain, Windows Credential Manager, or Linux Secret Service
// (via go-keyring). Callers use a logical service name and account (key) to
// store and retrieve values without writing token files to disk.
//
// When SetKeyringScopeFromConfigPath is set (e.g. from config file path),
// all account keys are namespaced so multiple Genie instances (different
// configs) do not overwrite each other's keychain data (e.g. Google OAuth).
package keyring

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

const service = "genie"

// scope is the optional instance suffix for keyring keys (derived from config path).
// When set, account names become "account_<scope>" so multiple Genie instances
// (different config files) do not collide.
var (
	scope   string
	scopeMu sync.RWMutex
)

// Account names Genie uses in the keyring (service "genie"). Use these
// constants instead of magic strings when calling KeyringGet/KeyringSet/KeyringDelete.
const (
	AccountGoogleOAuthToken       = "google_oauth_token"
	AccountGoogleOAuthUser        = "google_oauth_user"
	AccountGoogleOAuthCredentials = "google_oauth_credentials" // client ID/secret JSON stored after browser flow so Calendar/Gmail work without env at runtime
	AccountOpenAIAPIKey           = "OPENAI_API_KEY"
	AccountGoogleAPIKey           = "GOOGLE_API_KEY"
	AccountAnthropicAPIKey        = "ANTHROPIC_API_KEY"
	AccountTelegramBotToken       = "TELEGRAM_BOT_TOKEN"
	AccountSlackAppToken          = "SLACK_APP_TOKEN"
	AccountSlackBotToken          = "SLACK_BOT_TOKEN"
	AccountDiscordBotToken        = "DISCORD_BOT_TOKEN"
	AccountTeamsAppID             = "TEAMS_APP_ID"
	AccountTeamsAppPassword       = "TEAMS_APP_PASSWORD"
	// AccountAGUIPassword is the password for the AG-UI web chat (one per agent, scoped by config path).
	AccountAGUIPassword = "agui_password"
)

// knownGenieAccounts is the set of account names for KeyringDeleteAllGenie.
var knownGenieAccounts = []string{
	AccountGoogleOAuthToken,
	AccountGoogleOAuthUser,
	AccountGoogleOAuthCredentials,
	AccountOpenAIAPIKey,
	AccountGoogleAPIKey,
	AccountAnthropicAPIKey,
	AccountTelegramBotToken,
	AccountSlackAppToken,
	AccountSlackBotToken,
	AccountDiscordBotToken,
	AccountTeamsAppID,
	AccountTeamsAppPassword,
	AccountAGUIPassword,
}

// SetKeyringScopeFromConfigPath sets the keyring scope from the config file path
// so that multiple Genie instances (different config paths) use separate
// keychain entries and do not overwrite each other's Google OAuth (and other)
// data. Pass an empty path to use unscoped (legacy) keys. Should be called
// early (e.g. in root init or setup) before any keyring access.
func SetKeyringScopeFromConfigPath(configPath string) {
	scopeMu.Lock()
	defer scopeMu.Unlock()
	if configPath == "" {
		scope = ""
		return
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		scope = ""
		return
	}
	h := sha256.Sum256([]byte(abs))
	scope = hex.EncodeToString(h[:])[:12]
}

// scopedAccount returns the keyring account key to use (base name plus optional scope suffix).
func scopedAccount(account string) string {
	scopeMu.RLock()
	s := scope
	scopeMu.RUnlock()
	if s == "" {
		return account
	}
	return account + "_" + s
}

// memoryFallback holds secrets when the system keyring is unavailable
// (e.g. Docker, headless Linux). Process-only; not persisted across restarts.
var (
	memoryFallback   = make(map[string][]byte)
	memoryFallbackMu sync.RWMutex
)

// KeyringGet returns the value stored in the system keychain for the given
// service and account (key). If the keyring is unavailable (e.g. headless
// Linux, Docker), returns the value from the in-memory fallback when present.
// Returns (nil, err) when the entry is not found in either.
// Account is namespaced by the current keyring scope when set.
func KeyringGet(account string) ([]byte, error) {
	key := scopedAccount(account)
	s, err := keyring.Get(service, key)
	if err == nil {
		return []byte(s), nil
	}
	memoryFallbackMu.RLock()
	v, ok := memoryFallback[key]
	memoryFallbackMu.RUnlock()
	if ok {
		return v, nil
	}
	return nil, err
}

// KeyringSet stores a value in the system keychain for the given service and
// account (key). If the keyring is unavailable (e.g. Docker, headless Linux),
// stores in an in-memory fallback so the app keeps working; no error is
// returned. Values in the fallback are process-only and lost on restart.
// A warning is logged when falling back to memory so operators know secrets
// are not persisted. Account is namespaced by the current keyring scope when set.
func KeyringSet(account string, value []byte) error {
	key := scopedAccount(account)
	err := keyring.Set(service, key, string(value))
	if err == nil {
		return nil
	}
	log.Printf("[keyring] system keychain unavailable, using in-memory fallback for %q (secrets will be lost on restart)", key)
	memoryFallbackMu.Lock()
	clone := make([]byte, len(value))
	copy(clone, value)
	memoryFallback[key] = clone
	memoryFallbackMu.Unlock()
	return nil
}

// KeyringDelete removes the stored value for the given service and account
// (e.g. on sign-out or key rotation). Also removes from the in-memory fallback.
// Account is namespaced by the current keyring scope when set.
func KeyringDelete(account string) error {
	key := scopedAccount(account)
	_ = keyring.Delete(service, key)
	memoryFallbackMu.Lock()
	delete(memoryFallback, key)
	memoryFallbackMu.Unlock()
	return nil
}

// KeyringDeleteAllGenie removes all known Genie keyring entries for the
// current scope (or unscoped legacy entries when scope is not set). It deletes
// each account in knownGenieAccounts and ignores "not found" errors. Also
// clears those accounts from the in-memory fallback. Returns the first
// non-not-found error from the keyring if any, and the number of keyring
// entries successfully deleted. Use for cleanup (e.g. back-to-bottle).
// Call SetKeyringScopeFromConfigPath before this to clear only one instance's data.
func KeyringDeleteAllGenie() (deleted int, err error) {
	var firstErr error
	for _, account := range knownGenieAccounts {
		key := scopedAccount(account)
		delErr := keyring.Delete(service, key)
		if delErr == nil {
			deleted++
			continue
		}
		if isKeyringNotFoundErr(delErr) {
			continue
		}
		if firstErr == nil {
			firstErr = delErr
		}
	}
	memoryFallbackMu.Lock()
	for _, account := range knownGenieAccounts {
		delete(memoryFallback, scopedAccount(account))
	}
	memoryFallbackMu.Unlock()
	if firstErr != nil {
		return deleted, firstErr
	}
	return deleted, nil
}

func isKeyringNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "not found") || strings.Contains(s, "secret not found")
}
