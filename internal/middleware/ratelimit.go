package middleware

import (
	"encoding/json"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"laika/pkg/logger"
)

// RateLimit installs a per-caller token bucket. Refill is RPM tokens/minute and
// the bucket capacity is Burst. When a caller exhausts their bucket they get
// 429 with a Retry-After header — Laika never accepts-and-buffers, so the
// caller's queue fills and *they* notice.
//
// The "caller" is keyed by the leftmost X-Forwarded-For entry when present
// (Laika sits behind Caddy + Tailscale), falling back to the TCP peer IP. If
// you front Laika with a different proxy that strips XFF, every caller will
// collapse into one bucket — make sure XFF is forwarded.
//
// This is transport-layer self-protection, not policy. Loud floods are caller
// bugs; the right fix is in the caller.
func RateLimit(base *slog.Logger, rpm, burst int) func(http.Handler) http.Handler {
	if rpm <= 0 {
		rpm = 60
	}
	if burst <= 0 {
		burst = rpm / 2
		if burst < 1 {
			burst = 1
		}
	}
	limiter := newRateLimiter(rpm, burst)
	go limiter.gcLoop()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			caller := callerKey(r)
			ok, retryAfter := limiter.allow(caller)
			if ok {
				next.ServeHTTP(w, r)
				return
			}

			logger.FromContext(r.Context(), base).Warn("rate limited",
				"caller", caller,
				"path", r.URL.Path,
				"retry_after_s", retryAfter,
			)
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "rate limit exceeded",
			})
		})
	}
}

// callerKey extracts the caller identity for bucketing. Prefers the leftmost
// X-Forwarded-For (the real client through Caddy/Tailscale) and falls back to
// the TCP peer.
func callerKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// -- bucket impl --------------------------------------------------------------

type bucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

type rateLimiter struct {
	mu             sync.Mutex
	buckets        map[string]*bucket
	tokensPerSec   float64
	capacity       float64
	idleEvictAfter time.Duration
}

func newRateLimiter(rpm, burst int) *rateLimiter {
	return &rateLimiter{
		buckets:        make(map[string]*bucket),
		tokensPerSec:   float64(rpm) / 60.0,
		capacity:       float64(burst),
		idleEvictAfter: 10 * time.Minute,
	}
}

// allow returns true if the request should proceed. When false, retryAfter is
// the integer seconds the caller should wait before the next token is available.
func (l *rateLimiter) allow(key string) (bool, int) {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.capacity, lastRefill: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(l.capacity, b.tokens+elapsed*l.tokensPerSec)
		b.lastRefill = now
	}
	b.lastSeen = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	deficit := 1 - b.tokens
	wait := math.Ceil(deficit / l.tokensPerSec)
	if wait < 1 {
		wait = 1
	}
	return false, int(wait)
}

func (l *rateLimiter) gcLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for now := range t.C {
		l.mu.Lock()
		for k, b := range l.buckets {
			if now.Sub(b.lastSeen) > l.idleEvictAfter {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}
