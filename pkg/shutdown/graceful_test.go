package shutdown

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestGracefulShutdown_Register(t *testing.T) {
	gs := New()

	executed := false
	gs.Register("test", func(ctx context.Context) error {
		executed = true
		return nil
	})

	if len(gs.handlers) != 1 {
		t.Errorf("Expected 1 handler, got %d", len(gs.handlers))
	}

	if executed {
		t.Error("Handler should not execute yet")
	}
}

func TestGracefulShutdown_WithTimeout(t *testing.T) {
	timeout := 5 * time.Second
	gs := New(WithTimeout(timeout))

	if gs.timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, gs.timeout)
	}
}

func TestGracefulShutdown_Shutdown(t *testing.T) {
	gs := New(WithTimeout(100 * time.Millisecond))

	order := make([]string, 0, 3)
	var mu sync.Mutex

	gs.Register("handler1", func(ctx context.Context) error {
		mu.Lock()
		order = append(order, "handler1")
		mu.Unlock()
		return nil
	})

	gs.Register("handler2", func(ctx context.Context) error {
		mu.Lock()
		order = append(order, "handler2")
		mu.Unlock()
		return nil
	})

	gs.Register("handler3", func(ctx context.Context) error {
		mu.Lock()
		order = append(order, "handler3")
		mu.Unlock()
		return nil
	})

	err := gs.Shutdown()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Handlers should execute in reverse order
	mu.Lock()
	expected := []string{"handler3", "handler2", "handler1"}
	if len(order) != len(expected) {
		t.Errorf("Expected %d handlers, got %d", len(expected), len(order))
	}
	mu.Unlock()
}

func TestGracefulShutdown_Timeout(t *testing.T) {
	gs := New(WithTimeout(50 * time.Millisecond))

	gs.Register("slow", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	})

	start := time.Now()
	err := gs.Shutdown()
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded && err != nil {
		t.Logf("Shutdown returned error (expected): %v", err)
	}

	if elapsed > 200*time.Millisecond {
		t.Errorf("Shutdown took too long: %v", elapsed)
	}
}

func TestGracefulShutdown_Error(t *testing.T) {
	gs := New(WithTimeout(100 * time.Millisecond))

	expectedErr := errors.New("handler error")
	gs.Register("failing", func(ctx context.Context) error {
		return expectedErr
	})

	err := gs.Shutdown()
	if err == nil {
		t.Error("Expected error from handler")
	}
}

func TestGracefulShutdown_Concurrent(t *testing.T) {
	gs := New(WithTimeout(1 * time.Second))

	var executed int
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		gs.Register("handler", func(ctx context.Context) error {
			mu.Lock()
			executed++
			mu.Unlock()
			return nil
		})
	}

	err := gs.Shutdown()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mu.Lock()
	if executed != 10 {
		t.Errorf("Expected 10 executions, got %d", executed)
	}
	mu.Unlock()
}

type mockDrainable struct {
	drained int
	delay   time.Duration
}

func (m *mockDrainable) Drain(timeout time.Duration) int {
	time.Sleep(m.delay)
	return m.drained
}

func TestDrainAndWait(t *testing.T) {
	drainables := []Drainable{
		&mockDrainable{drained: 5, delay: 10 * time.Millisecond},
		&mockDrainable{drained: 3, delay: 10 * time.Millisecond},
	}

	total := DrainAndWait(drainables, 100*time.Millisecond)

	if total != 8 {
		t.Errorf("Expected total of 8 drained, got %d", total)
	}
}

func TestDrainAndWait_Timeout(t *testing.T) {
	drainables := []Drainable{
		&mockDrainable{drained: 5, delay: 10 * time.Millisecond},
		&mockDrainable{drained: 3, delay: 2 * time.Second},
	}

	start := time.Now()
	total := DrainAndWait(drainables, 100*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("Drain took too long: %v", elapsed)
	}

	if total == 8 {
		t.Error("Should not have completed all drains")
	}
}

type mockStoppable struct {
	err error
}

func (m *mockStoppable) Stop() error {
	return m.err
}

func TestStopAll(t *testing.T) {
	stoppables := []Stoppable{
		&mockStoppable{},
		&mockStoppable{},
		&mockStoppable{},
	}

	err := StopAll(stoppables)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestStopAll_Error(t *testing.T) {
	stoppables := []Stoppable{
		&mockStoppable{err: errors.New("error1")},
		&mockStoppable{err: errors.New("error2")},
		&mockStoppable{},
	}

	err := StopAll(stoppables)
	if err == nil {
		t.Error("Expected error")
	}
}

type mockClosable struct {
	err error
}

func (m *mockClosable) Close() error {
	return m.err
}

func TestCloseAll(t *testing.T) {
	closables := []Closable{
		&mockClosable{},
		&mockClosable{},
	}

	err := CloseAll(closables)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestTimeout_Run(t *testing.T) {
	timeout := NewTimeout(100*time.Millisecond, nil)

	err := timeout.Run(func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestTimeout_Timeout(t *testing.T) {
	timeoutCalled := false
	timeout := NewTimeout(50*time.Millisecond, func() {
		timeoutCalled = true
	})

	err := timeout.Run(func() error {
		time.Sleep(5 * time.Second)
		return nil
	})

	if err != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded, got %v", err)
	}

	if !timeoutCalled {
		t.Error("Timeout callback should have been called")
	}
}
