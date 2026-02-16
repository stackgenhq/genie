package httputil

import (
	"net"
	"net/http"
	"time"
)

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

func NewRoundTripper(requestEnhancer ...RequestEnhancer) http.RoundTripper {
	return &RoundTripper{
		rt: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			Dial: (&net.Dialer{
				Timeout: 5 * time.Second,
			}).Dial,
			TLSHandshakeTimeout: 5 * time.Second,
			IdleConnTimeout:     90 * time.Second,
		},
		requestEnhancer: requestEnhancer,
	}
}

func (r *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for i := range r.requestEnhancer {
		r.requestEnhancer[i](req)
	}
	return r.rt.RoundTrip(req)
}
