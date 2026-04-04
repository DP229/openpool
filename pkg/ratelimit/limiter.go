package ratelimit

import (
	"sync"
	"time"
)

type RateLimiter struct {
	requests map[string]*ClientLimit
	mu       sync.RWMutex
	rate     int
	burst    int
	window   time.Duration
}

type ClientLimit struct {
	tokens   int
	lastSeen time.Time
	mu       sync.Mutex
}

func NewRateLimiter(rate, burst int, window time.Duration) *RateLimiter {
	if window == 0 {
		window = time.Minute
	}

	rl := &RateLimiter{
		requests: make(map[string]*ClientLimit),
		rate:     rate,
		burst:    burst,
		window:   window,
	}

	go rl.cleanupRoutine()

	return rl
}

func (rl *RateLimiter) Allow(clientID string) bool {
	rl.mu.RLock()
	client, exists := rl.requests[clientID]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		client = &ClientLimit{
			tokens:   rl.burst - 1,
			lastSeen: time.Now(),
		}
		rl.requests[clientID] = client
		rl.mu.Unlock()
		return true
	}

	client.mu.Lock()
	defer client.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(client.lastSeen)

	tokensToAdd := int(elapsed.Seconds() * float64(rl.rate) / rl.window.Seconds())
	client.tokens += tokensToAdd
	if client.tokens > rl.burst {
		client.tokens = rl.burst
	}
	client.lastSeen = now

	if client.tokens > 0 {
		client.tokens--
		return true
	}

	return false
}

func (rl *RateLimiter) WaitForToken(clientID string, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		if rl.Allow(clientID) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}

	return false
}

func (rl *RateLimiter) Reset(clientID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.requests, clientID)
}

func (rl *RateLimiter) cleanupRoutine() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		rl.Cleanup(10 * time.Minute)
	}
}

func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, client := range rl.requests {
		client.mu.Lock()
		if client.lastSeen.Before(cutoff) {
			delete(rl.requests, id)
		}
		client.mu.Unlock()
	}
}

func (rl *RateLimiter) Stats() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	activeClients := 0
	totalTokens := 0

	for _, client := range rl.requests {
		client.mu.Lock()
		activeClients++
		totalTokens += client.tokens
		client.mu.Unlock()
	}

	return map[string]interface{}{
		"active_clients": activeClients,
		"total_tokens":   totalTokens,
		"rate":           rl.rate,
		"burst":          rl.burst,
		"window_seconds": rl.window.Seconds(),
	}
}
