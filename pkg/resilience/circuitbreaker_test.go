package resilience

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_Closed_State(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 3,
		Timeout:     1 * time.Second,
	}

	cb := NewCircuitBreaker(config)

	if cb.State() != StateClosed {
		t.Errorf("Expected initial state to be closed, got %v", cb.State())
	}

	for i := 0; i < 3; i++ {
		err := cb.Call(func() error {
			return errors.New("failure")
		})
		if err == nil {
			t.Error("Expected error from circuit breaker")
		}
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected state to be open after %d failures, got %v", 3, cb.State())
	}
}

func TestCircuitBreaker_Open_State(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 1,
		Timeout:     100 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	err := cb.Call(func() error {
		return errors.New("failure")
	})
	if err == nil {
		t.Error("Expected failure")
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected state to be open, got %v", cb.State())
	}

	// Try to call while open - should fail fast
	err = cb.Call(func() error {
		return nil
	})
	if err != ErrCircuitOpen {
		t.Errorf("Expected ErrCircuitOpen, got %v", err)
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Now should transition to half-open on next call
	err = cb.Call(func() error { return nil })
	if err != nil {
		t.Errorf("Expected success in half-open, got %v", err)
	}
}

func TestCircuitBreaker_HalfOpen_Success(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:          "test",
		MaxFailures:   1,
		Timeout:       50 * time.Millisecond,
		HalfOpenLimit: 2,
	}

	cb := NewCircuitBreaker(config)

	cb.Call(func() error { return errors.New("failure") })

	time.Sleep(60 * time.Millisecond)

	err := cb.Call(func() error { return nil })
	if err != nil {
		t.Errorf("Expected success in half-open, got %v", err)
	}

	if cb.State() != StateHalfOpen {
		t.Errorf("Expected half-open after one success, got %v", cb.State())
	}

	err = cb.Call(func() error { return nil })
	if err != nil {
		t.Errorf("Expected success on second attempt, got %v", err)
	}

	if cb.State() != StateClosed {
		t.Errorf("Expected closed after half-open limit reached, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpen_Failure(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 1,
		Timeout:     50 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	cb.Call(func() error { return errors.New("failure") })

	time.Sleep(60 * time.Millisecond)

	err := cb.Call(func() error { return errors.New("failure") })
	if err == nil {
		t.Error("Expected failure in half-open")
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected open after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_Success_Resets_Failures(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 5,
		Timeout:     1 * time.Second,
	}

	cb := NewCircuitBreaker(config)

	cb.Call(func() error { return errors.New("failure") })
	cb.Call(func() error { return errors.New("failure") })

	if cb.Failures() != 2 {
		t.Errorf("Expected 2 failures, got %d", cb.Failures())
	}

	cb.Call(func() error { return nil })

	if cb.Failures() != 0 {
		t.Errorf("Expected failures to reset to 0, got %d", cb.Failures())
	}

	if cb.State() != StateClosed {
		t.Errorf("Expected state to remain closed, got %v", cb.State())
	}
}

func TestCircuitBreaker_Timeout(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 1,
		Timeout:     100 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)

	err := cb.CallWithTimeout(func() error {
		time.Sleep(200 * time.Millisecond)
		return nil
	}, 50*time.Millisecond)

	if err != ErrTimeout {
		t.Errorf("Expected ErrTimeout, got %v", err)
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected open state after timeout, got %v", cb.State())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 1,
		Timeout:     1 * time.Second,
	}

	cb := NewCircuitBreaker(config)

	cb.Call(func() error { return errors.New("failure") })

	if cb.State() != StateOpen {
		t.Errorf("Expected open state, got %v", cb.State())
	}

	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("Expected closed state after reset, got %v", cb.State())
	}

	if cb.Failures() != 0 {
		t.Errorf("Expected 0 failures after reset, got %d", cb.Failures())
	}
}

func TestCircuitBreaker_Trip(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 10,
		Timeout:     1 * time.Second,
	}

	cb := NewCircuitBreaker(config)

	if cb.State() != StateClosed {
		t.Errorf("Expected closed, got %v", cb.State())
	}

	cb.Trip()

	if cb.State() != StateOpen {
		t.Errorf("Expected open after trip, got %v", cb.State())
	}
}

func TestCircuitBreakerGroup_Get(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:        "default",
		MaxFailures: 3,
		Timeout:     1 * time.Second,
	}

	group := NewCircuitBreakerGroup(config)

	cb1 := group.Get("service1")
	cb2 := group.Get("service2")
	cb1again := group.Get("service1")

	if cb1 != cb1again {
		t.Error("Expected same circuit breaker for same name")
	}

	if cb1 == cb2 {
		t.Error("Expected different circuit breakers for different names")
	}
}

func TestCircuitBreakerGroup_States(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:        "default",
		MaxFailures: 1,
		Timeout:     1 * time.Second,
	}

	group := NewCircuitBreakerGroup(config)

	group.Get("service1")
	group.Get("service2")

	group.Call("service1", func() error { return errors.New("failure") })

	states := group.States()

	if states["service1"] != StateOpen {
		t.Errorf("Expected service1 to be open, got %v", states["service1"])
	}

	if states["service2"] != StateClosed {
		t.Errorf("Expected service2 to be closed, got %v", states["service2"])
	}
}

func TestRetry_Success(t *testing.T) {
	config := DefaultRetryConfig()
	config.MaxAttempts = 3

	attempts := 0
	err := Retry(config, func() error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary failure")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Expected success after retry, got %v", err)
	}

	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}

func TestRetry_MaxAttempts(t *testing.T) {
	config := DefaultRetryConfig()
	config.MaxAttempts = 3
	config.InitialDelay = 10 * time.Millisecond

	attempts := 0
	err := Retry(config, func() error {
		attempts++
		return errors.New("permanent failure")
	})

	if err == nil {
		t.Error("Expected error after max attempts")
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestBulkhead_Execute(t *testing.T) {
	bulkhead := NewBulkhead("test", 2)

	executed := make(chan struct{}, 3)

	for i := 0; i < 2; i++ {
		go func() {
			err := bulkhead.Execute(func() error {
				executed <- struct{}{}
				time.Sleep(100 * time.Millisecond)
				return nil
			})
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)

	if bulkhead.Available() != 0 {
		t.Errorf("Expected 0 available concurrent executions, got %d", bulkhead.Available())
	}

	err := bulkhead.Execute(func() error {
		return nil
	})
	if err == nil {
		t.Error("Expected error when bulkhead is full")
	}
}

func TestBulkhead_Available(t *testing.T) {
	bulkhead := NewBulkhead("test", 5)

	if bulkhead.Available() != 5 {
		t.Errorf("Expected 5 available, got %d", bulkhead.Available())
	}

	bulkhead.Acquire()
	if bulkhead.Available() != 4 {
		t.Errorf("Expected 4 available, got %d", bulkhead.Available())
	}

	bulkhead.Acquire()
	bulkhead.Acquire()
	bulkhead.Acquire()

	if bulkhead.Available() != 1 {
		t.Errorf("Expected 1 available, got %d", bulkhead.Available())
	}

	bulkhead.Release()
	if bulkhead.Available() != 2 {
		t.Errorf("Expected 2 available after release, got %d", bulkhead.Available())
	}
}
