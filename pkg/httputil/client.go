// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package httputil

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	defaultTLSConfig *tls.Config
	defaultTLSMu     sync.RWMutex
)

// SetDefaultTLSConfig sets the TLS config used by GetClient and NewRoundTripper.
// Call this at startup (e.g. from config) to enforce NIST 2030 minimums (TLS 1.2+).
// When nil, no custom TLS config is applied (Go defaults apply).
// The config is cloned so later mutation by the caller does not affect the default.
func SetDefaultTLSConfig(cfg *tls.Config) {
	defaultTLSMu.Lock()
	defer defaultTLSMu.Unlock()
	if cfg != nil {
		defaultTLSConfig = cfg.Clone()
	} else {
		defaultTLSConfig = nil
	}
}

func getDefaultTLSConfig() *tls.Config {
	defaultTLSMu.RLock()
	defer defaultTLSMu.RUnlock()
	return defaultTLSConfig
}

func GetClient(requestEnhancer ...RequestEnhancer) *http.Client {
	return &http.Client{
		Timeout:   time.Second * 10,
		Transport: NewRoundTripper(requestEnhancer...),
	}
}

type RequestEnhancer func(req *http.Request)

type RoundTripper struct {
	rt              http.RoundTripper
	requestEnhancer []RequestEnhancer
}

// NewRoundTripper returns an http.RoundTripper that uses NIST 2030–compliant
// TLS when SetDefaultTLSConfig has been called (min TLS 1.2, optional cipher restriction).
func NewRoundTripper(requestEnhancer ...RequestEnhancer) http.RoundTripper {
	tlsCfg := getDefaultTLSConfig()
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 5 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}
	if tlsCfg != nil {
		transport.TLSClientConfig = tlsCfg.Clone()
	}
	return &RoundTripper{
		rt:              transport,
		requestEnhancer: requestEnhancer,
	}
}

func (r *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for i := range r.requestEnhancer {
		r.requestEnhancer[i](req)
	}

	ctx := req.Context()
	tracer := otel.Tracer("httputil")
	ctx, span := tracer.Start(ctx, "http.request",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	span.SetAttributes(
		attribute.String("http.method", req.Method),
		attribute.String("http.url", sanitizeURL(req.URL)),
	)

	start := time.Now()
	resp, err := r.rt.RoundTrip(req.WithContext(ctx))
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.Int64("http.duration_ms", durationMs))
		return nil, err
	}

	span.SetAttributes(
		attribute.Int("http.status_code", resp.StatusCode),
		attribute.Int64("http.duration_ms", durationMs),
	)
	span.SetStatus(codes.Ok, "")

	return resp, nil
}

// sanitizeURL returns scheme://host/path, stripping query parameters, userinfo,
// and fragments to prevent leaking API keys or signed URL tokens into telemetry.
func sanitizeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host + u.Path
}
