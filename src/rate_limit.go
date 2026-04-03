package main

import (
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type tokenBucket struct {
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	now := time.Now()
	return &tokenBucket{
		rate:   rate,
		burst:  float64(burst),
		tokens: float64(burst),
		last:   now,
	}
}

func (b *tokenBucket) Allow(now time.Time) bool {
	if b == nil {
		return true
	}

	if now.Before(b.last) {
		now = b.last
	}

	elapsed := now.Sub(b.last).Seconds()
	b.tokens = math.Min(b.burst, b.tokens+(elapsed*b.rate))
	b.last = now

	if b.tokens < 1 {
		return false
	}

	b.tokens--
	return true
}

type ipBucket struct {
	limiter  *tokenBucket
	lastSeen time.Time
}

type ipRateLimiter struct {
	mu              sync.Mutex
	rate            float64
	burst           int
	entryTTL        time.Duration
	cleanupInterval time.Duration
	nextCleanup     time.Time
	now             func() time.Time
	entries         map[string]*ipBucket
}

func newIPRateLimiter(rate float64, burst int, entryTTL, cleanupInterval time.Duration) *ipRateLimiter {
	now := time.Now()
	return &ipRateLimiter{
		rate:            rate,
		burst:           burst,
		entryTTL:        entryTTL,
		cleanupInterval: cleanupInterval,
		nextCleanup:     now.Add(cleanupInterval),
		now:             time.Now,
		entries:         make(map[string]*ipBucket),
	}
}

func (l *ipRateLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}

	now := l.now()
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if !now.Before(l.nextCleanup) {
		l.cleanupLocked(now)
		l.nextCleanup = now.Add(l.cleanupInterval)
	}

	entry, ok := l.entries[key]
	if !ok {
		entry = &ipBucket{limiter: newTokenBucket(l.rate, l.burst)}
		l.entries[key] = entry
	}

	entry.lastSeen = now
	return entry.limiter.Allow(now)
}

func (l *ipRateLimiter) cleanupLocked(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) > l.entryTTL {
			delete(l.entries, key)
		}
	}
}

func clientIPFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}

	return strings.TrimSpace(r.RemoteAddr)
}
