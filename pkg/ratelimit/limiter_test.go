package ratelimit

import (
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(10, 5, time.Minute)

	for i := 0; i < 5; i++ {
		if !rl.Allow("client1") {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	if rl.Allow("client1") {
		t.Error("Request 6 should be denied (burst exhausted)")
	}
}

func TestRateLimiter_MultipleClients(t *testing.T) {
	rl := NewRateLimiter(10, 3, time.Minute)

	if !rl.Allow("client1") {
		t.Error("client1 first request should be allowed")
	}

	if !rl.Allow("client2") {
		t.Error("client2 first request should be allowed")
	}

	if !rl.Allow("client1") {
		t.Error("client1 second request should be allowed")
	}

	rl.Reset("client1")

	stats := rl.Stats()
	if stats["active_clients"].(int) < 1 {
		t.Error("Should have at least one active client")
	}
}

func TestRateLimiter_TokenRefill(t *testing.T) {
	rl := NewRateLimiter(60, 2, time.Minute)

	if !rl.Allow("client1") {
		t.Error("First request should be allowed")
	}
	if !rl.Allow("client1") {
		t.Error("Second request should be allowed")
	}
	if rl.Allow("client1") {
		t.Error("Third request should be denied (burst exhausted)")
	}

	time.Sleep(2 * time.Second)

	if !rl.Allow("client1") {
		t.Error("Request after refill should be allowed")
	}
}

func TestRateLimiter_WaitForToken(t *testing.T) {
	rl := NewRateLimiter(60, 1, time.Minute)

	if !rl.Allow("client1") {
		t.Error("First request should be allowed")
	}

	start := time.Now()
	if !rl.WaitForToken("client1", 2*time.Second) {
		t.Error("Should eventually get a token")
	}
	elapsed := time.Since(start)

	if elapsed < 500*time.Millisecond {
		t.Errorf("Should have waited for token refill, elapsed: %v", elapsed)
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(10, 5, time.Minute)

	rl.Allow("client1")
	rl.Allow("client2")
	rl.Allow("client3")

	time.Sleep(100 * time.Millisecond)

	rl.Cleanup(50 * time.Millisecond)

	stats := rl.Stats()
	if stats["active_clients"].(int) != 0 {
		t.Errorf("All clients should be cleaned up, got %d", stats["active_clients"])
	}
}

func TestRateLimiter_Concurrent(t *testing.T) {
	rl := NewRateLimiter(1000, 100, time.Minute)

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(id string) {
			allowed := 0
			for j := 0; j < 20; j++ {
				if rl.Allow(id) {
					allowed++
				}
			}
			done <- true
		}(string(rune('A' + i)))
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
