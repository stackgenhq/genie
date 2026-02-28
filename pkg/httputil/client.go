package httputil

import (
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"
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
	return r.rt.RoundTrip(req)
}
