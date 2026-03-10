// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package keyring provides a runtimevar implementation backed by the
// system keyring (macOS Keychain, Windows Credential Manager, Linux Secret
// Service). Use OpenVariable or OpenVariableURL to construct a
// *runtimevar.Variable. Secrets stored via security.KeyringSet can be
// referenced in config as keyring://service/account (e.g.
// keyring://genie/TELEGRAM_BOT_TOKEN).
//
// # URLs
//
// For runtimevar.OpenVariable, keyringvar registers for the scheme "keyring".
// URL form: keyring://<service>/<account>. Example: keyring://genie/OPENAI_API_KEY.
// Query parameters:
//   - decoder: optional; defaults to "string". See runtimevar.DecoderByName.
package keyring

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gocloud.dev/gcerrors"
	"gocloud.dev/runtimevar"
	"gocloud.dev/runtimevar/driver"
)

func init() {
	runtimevar.DefaultURLMux().RegisterVariable(Scheme, &URLOpener{})
}

// Scheme is the URL scheme keyringvar registers under on runtimevar.DefaultURLMux.
const Scheme = "keyring"

// URLOpener opens keyringvar URLs like "keyring://genie/TELEGRAM_BOT_TOKEN?decoder=string".
// The URL host is the keyring service name (e.g. "genie"); the path is the
// account (key) name (e.g. "/TELEGRAM_BOT_TOKEN"). Query parameter "decoder"
// defaults to "string".
type URLOpener struct {
	// Decoder specifies the decoder to use if one is not specified in the URL.
	// Defaults to string decoder.
	Decoder *runtimevar.Decoder
}

// OpenVariableURL opens the variable at the given keyring URL.
// URL form: keyring:///account (path only) or keyring://service/account.
// The host (service name) is ignored; only the path, or host when path is empty,
// is used as the account. All keyring access uses the fixed service from the keyring package.
func (o *URLOpener) OpenVariableURL(ctx context.Context, u *url.URL) (*runtimevar.Variable, error) {
	account := strings.TrimPrefix(u.Path, "/")
	if account == "" {
		account = u.Host // keyring://OPENAI_API_KEY → account from host
	}
	if account == "" {
		return nil, fmt.Errorf("open variable %v: keyring URL must be keyring:///account or keyring://service/account", u)
	}
	q := u.Query()
	decoderName := q.Get("decoder")
	if decoderName == "" {
		decoderName = "string"
	}
	q.Del("decoder")
	for param := range q {
		return nil, fmt.Errorf("open variable %v: invalid query parameter %q", u, param)
	}
	decoder, err := runtimevar.DecoderByName(ctx, decoderName, o.Decoder)
	if err != nil {
		return nil, fmt.Errorf("open variable %v: %w", u, err)
	}
	return OpenVariable(account, decoder)
}

// OpenVariable constructs a *runtimevar.Variable backed by the keyring entry
// for the given account. The value is read once and then treated
// as constant (keyring has no watch support).
func OpenVariable(account string, decoder *runtimevar.Decoder) (*runtimevar.Variable, error) {
	if account == "" {
		return nil, fmt.Errorf("keyringvar: account is required")
	}
	if decoder == nil {
		return nil, fmt.Errorf("keyringvar: decoder is required")
	}
	w, err := newWatcher(account, decoder)
	if err != nil {
		return nil, err
	}
	return runtimevar.New(w), nil
}

func newWatcher(account string, decoder *runtimevar.Decoder) (*watcher, error) {
	s, err := KeyringGet(account)
	if err != nil {
		return &watcher{err: err}, nil
	}
	raw := []byte(s)
	val, err := decoder.Decode(context.Background(), raw)
	if err != nil {
		return &watcher{err: err}, nil
	}
	return &watcher{value: val, updateTime: time.Now()}, nil
}

// watcher implements driver.Watcher and driver.State.
type watcher struct {
	value      any
	err        error
	updateTime time.Time
}

// Value implements driver.State.Value.
func (w *watcher) Value() (any, error) {
	return w.value, w.err
}

// UpdateTime implements driver.State.UpdateTime.
func (w *watcher) UpdateTime() time.Time {
	return w.updateTime
}

// As implements driver.State.As.
func (w *watcher) As(any) bool {
	return false
}

// WatchVariable implements driver.Watcher. Keyring has no change notifications;
// the value is returned once and subsequent calls block until context is done.
// Returns a new state holding ctx.Err() so the returned State is immutable.
func (w *watcher) WatchVariable(ctx context.Context, prev driver.State) (driver.State, time.Duration) {
	if prev == nil {
		return w, 0
	}
	<-ctx.Done()
	return &watcher{err: ctx.Err()}, 0
}

// Close implements driver.Watcher.
func (*watcher) Close() error { return nil }

// ErrorAs implements driver.Watcher.
func (*watcher) ErrorAs(err error, i any) bool { return false }

// ErrorCode implements driver.Watcher.
func (w *watcher) ErrorCode(err error) gcerrors.ErrorCode {
	if err != nil && isKeyringNotFound(err) {
		return gcerrors.NotFound
	}
	return gcerrors.Unknown
}

func isKeyringNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "not found") || strings.Contains(s, "secret not found")
}
