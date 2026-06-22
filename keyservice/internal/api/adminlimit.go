package api

import (
	"net/http"
	"sync"
	"time"
)

// adminLimiter is a simple in-process token-bucket rate limiter for admin
// endpoints. It prevents brute-force attacks against the admin token by
// throttling requests before the auth check runs.
type adminLimiter struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	rate     float64 // tokens per second
	lastTime time.Time
}

func newAdminLimiter(rate, capacity float64) *adminLimiter {
	return &adminLimiter{
		tokens:   capacity,
		capacity: capacity,
		rate:     rate,
		lastTime: time.Now(),
	}
}

// allow returns true if a token is available, false if the caller should be
// rate-limited. This runs before auth so an attacker cannot probe tokens at
// unlimited speed.
func (l *adminLimiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.lastTime).Seconds()
	l.lastTime = now

	l.tokens += elapsed * l.rate
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}

	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

// adminRateLimit is middleware that applies the admin limiter before the handler.
func adminRateLimit(limiter *adminLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.allow() {
			w.Header().Set("Retry-After", "1")
			writeJSONError(w, http.StatusTooManyRequests, "admin rate limit exceeded")
			return
		}
		next(w, r)
	}
}
