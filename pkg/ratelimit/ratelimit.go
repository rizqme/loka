package ratelimit

import (
	"net/http"
	"sync"
	"time"
)

// Limiter implements a simple token-bucket rate limiter per IP.
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     int           // Requests per window.
	window   time.Duration
	cleanup  time.Duration
}

type bucket struct {
	tokens    int
	lastReset time.Time
}

// New creates a rate limiter. rate is max requests per window.
func New(rate int, window time.Duration) *Limiter {
	l := &Limiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		window:  window,
		cleanup: 5 * time.Minute,
	}
	go l.cleanupLoop()
	return l
}

// Middleware returns an HTTP middleware that rate-limits by client IP.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr

		if !l.allow(ip) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (l *Limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.rate - 1, lastReset: now}
		return true
	}

	if now.Sub(b.lastReset) > l.window {
		b.tokens = l.rate - 1
		b.lastReset = now
		return true
	}

	if b.tokens > 0 {
		b.tokens--
		return true
	}

	return false
}

func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanup)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for k, b := range l.buckets {
			if now.Sub(b.lastReset) > l.cleanup {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}
