package agui

import (
	"net/http"
	"strings"
	"sync"

	"github.com/stackgenhq/genie/pkg/logger"
	"golang.org/x/time/rate"
)

// ---------------------------------------------------------------------------
// Per-IP rate limiter (token bucket)
// ---------------------------------------------------------------------------

// ipRateLimiter holds a per-IP token-bucket limiter backed by sync.Map.
// Each unique IP gets its own *rate.Limiter created on first request.
type ipRateLimiter struct {
	limiters sync.Map   // map[string]*rate.Limiter
	rate     rate.Limit // tokens per second
	burst    int        // burst capacity
}

func newIPRateLimiter(r rate.Limit, burst int) *ipRateLimiter {
	return &ipRateLimiter{rate: r, burst: burst}
}

func (l *ipRateLimiter) get(ip string) *rate.Limiter {
	if v, ok := l.limiters.Load(ip); ok {
		return v.(*rate.Limiter)
	}
	limiter := rate.NewLimiter(l.rate, l.burst)
	actual, _ := l.limiters.LoadOrStore(ip, limiter)
	return actual.(*rate.Limiter)
}

// rateLimitMiddleware returns HTTP 429 when the per-IP token bucket is exhausted.
func rateLimitMiddleware(rl *ipRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// not for approval request
			if r.URL.Path == "/approve" {
				next.ServeHTTP(w, r)
				return
			}
			ip := r.RemoteAddr // includes port, but that's fine for server-side limiting
			if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
				// X-Forwarded-For can contain multiple IPs (client, proxy1, proxy2...).
				// We take the first one (client IP).
				parts := strings.Split(fwd, ",")
				ip = strings.TrimSpace(parts[0])
			}

			if !rl.get(ip).Allow() {
				logr := logger.GetLogger(r.Context())
				logr.Warn("rate limit exceeded", "ip", ip)
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------------
// Global concurrency limiter (counting semaphore)
// ---------------------------------------------------------------------------

// concurrencyLimitMiddleware caps the number of in-flight requests.
// When all slots are occupied it returns HTTP 503.
func concurrencyLimitMiddleware(maxConcurrent int) func(http.Handler) http.Handler {
	sem := make(chan struct{}, maxConcurrent)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			default:
				logr := logger.GetLogger(r.Context())
				logr.Warn("concurrency limit reached")
				http.Error(w, `{"error":"server busy, try again later"}`, http.StatusServiceUnavailable)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Request body size cap
// ---------------------------------------------------------------------------

// maxBodyMiddleware wraps the request body with http.MaxBytesReader so that
// JSON decoding will fail with a clear error when the payload is too large.
func maxBodyMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
