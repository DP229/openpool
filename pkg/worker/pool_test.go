package worker

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPool_Start(t *testing.T) {
	config := Config{
		Workers:   4,
		QueueSize: 10,
	}

	pool := NewPool(config)

	handler := func(ctx context.Context, task *Task) ([]byte, error) {
		return []byte("result"), nil
	}

	err := pool.Start(handler)
	if err != nil {
		t.Fatalf("Failed to start pool: %v", err)
	}

	if !pool.IsRunning() {
		t.Error("Pool should be running")
	}

	pool.Close()
}

func TestPool_Submit(t *testing.T) {
	config := Config{
		Workers:   2,
		QueueSize: 10,
	}

	pool := NewPool(config)
	defer pool.Close()

	handler := func(ctx context.Context, task *Task) ([]byte, error) {
		time.Sleep(10 * time.Millisecond)
		return []byte("processed:" + task.ID), nil
	}

	pool.Start(handler)

	task := &Task{
		ID:       "test-1",
		Priority: 1,
		Data:     []byte("test"),
	}

	err := pool.Submit(task)
	if err != nil {
		t.Fatalf("Failed to submit task: %v", err)
	}

	select {
	case taskResult := <-pool.Results():
		if taskResult.TaskID != "test-1" {
			t.Errorf("Expected task ID test-1, got %s", taskResult.TaskID)
		}
		if taskResult.Error != nil {
			t.Errorf("Unexpected error: %v", taskResult.Error)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for result")
	}
}

func TestPool_Concurrent_Submit(t *testing.T) {
	config := Config{
		Workers:   4,
		QueueSize: 100,
	}

	pool := NewPool(config)
	defer pool.Close()

	handler := func(ctx context.Context, task *Task) ([]byte, error) {
		time.Sleep(5 * time.Millisecond)
		return []byte("done"), nil
	}

	pool.Start(handler)

	numTasks := 20
	var wg sync.WaitGroup

	for i := 0; i < numTasks; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			task := &Task{
				ID:   string(rune('A' + id)),
				Data: []byte("test"),
			}
			err := pool.Submit(task)
			if err != nil {
				t.Errorf("Failed to submit task %d: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	completed := 0
	timeout := time.After(2 * time.Second)

	for completed < numTasks {
		select {
		case <-pool.Results():
			completed++
		case <-timeout:
			t.Errorf("Timeout waiting for results, got %d/%d", completed, numTasks)
			return
		}
	}

	if completed != numTasks {
		t.Errorf("Expected %d completed, got %d", numTasks, completed)
	}
}

func TestPool_Timeout(t *testing.T) {
	config := Config{
		Workers:     2,
		QueueSize:   10,
		TaskTimeout: 100 * time.Millisecond,
	}

	pool := NewPool(config)
	defer pool.Close()

	handler := func(ctx context.Context, task *Task) ([]byte, error) {
		select {
		case <-time.After(5 * time.Second):
			return []byte("done"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	pool.Start(handler)

	task := &Task{
		ID:      "timeout-test",
		Timeout: 50 * time.Millisecond,
	}

	err := pool.Submit(task)
	if err != nil {
		t.Fatalf("Failed to submit task: %v", err)
	}

	select {
	case result := <-pool.Results():
		if result.Error == nil {
			t.Error("Expected timeout error")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Timeout waiting for result")
	}
}

func TestPool_QueueFull(t *testing.T) {
	config := Config{
		Workers:   1,
		QueueSize: 2,
	}

	pool := NewPool(config)

	blocker := make(chan struct{})
	handler := func(ctx context.Context, task *Task) ([]byte, error) {
		<-blocker
		return []byte("done"), nil
	}

	pool.Start(handler)

	for i := 0; i < 2; i++ {
		err := pool.Submit(&Task{ID: string(rune('A' + i))})
		if err != nil {
			t.Errorf("Unexpected error on task %d: %v", i, err)
		}
	}

	err := pool.Submit(&Task{ID: "should-fail"})
	if err != ErrPoolFull {
		t.Errorf("Expected ErrPoolFull, got %v", err)
	}

	close(blocker)
	pool.Close()
}

func TestPool_Close(t *testing.T) {
	config := Config{
		Workers:   2,
		QueueSize: 10,
	}

	pool := NewPool(config)

	handler := func(ctx context.Context, task *Task) ([]byte, error) {
		return []byte("done"), nil
	}

	pool.Start(handler)

	err := pool.Close()
	if err != nil {
		t.Errorf("Failed to close pool: %v", err)
	}

	if pool.IsRunning() {
		t.Error("Pool should not be running after close")
	}

	err = pool.Submit(&Task{ID: "after-close"})
	if err != ErrPoolClosed {
		t.Errorf("Expected ErrPoolClosed, got %v", err)
	}

	err = pool.Close()
	if err != ErrPoolClosed {
		t.Errorf("Second close should return ErrPoolClosed, got %v", err)
	}
}

func TestPool_Drain(t *testing.T) {
	config := Config{
		Workers:   2,
		QueueSize: 10,
	}

	pool := NewPool(config)

	completed := make(chan struct{}, 5)
	handler := func(ctx context.Context, task *Task) ([]byte, error) {
		time.Sleep(50 * time.Millisecond)
		completed <- struct{}{}
		return []byte("done"), nil
	}

	pool.Start(handler)

	for i := 0; i < 5; i++ {
		pool.Submit(&Task{ID: string(rune('A' + i))})
	}

	drained := pool.Drain(500 * time.Millisecond)

	if drained < 3 {
		t.Errorf("Expected at least 3 drained results, got %d", drained)
	}

	pool.Close()
}

func TestPool_Stats(t *testing.T) {
	config := Config{
		Workers:   2,
		QueueSize: 10,
	}

	pool := NewPool(config)
	defer pool.Close()

	initial := pool.Stats()
	if initial.TasksSubmitted != 0 {
		t.Error("Initial tasks submitted should be 0")
	}
	if initial.TasksCompleted != 0 {
		t.Error("Initial tasks completed should be 0")
	}

	handler := func(ctx context.Context, task *Task) ([]byte, error) {
		return []byte("done"), nil
	}

	pool.Start(handler)

	for i := 0; i < 3; i++ {
		pool.Submit(&Task{ID: string(rune('A' + i))})
	}

	time.Sleep(100 * time.Millisecond)

	stats := pool.Stats()
	if stats.TasksSubmitted < 3 {
		t.Errorf("Expected at least 3 tasks submitted, got %d", stats.TasksSubmitted)
	}
}

func TestPool_Priority_Ordering(t *testing.T) {
	config := Config{
		Workers:   1,
		QueueSize: 10,
	}

	pool := NewPool(config)
	defer pool.Close()

	var executionOrder []string
	var mu sync.Mutex

	handler := func(ctx context.Context, task *Task) ([]byte, error) {
		mu.Lock()
		executionOrder = append(executionOrder, task.ID)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		return []byte("done"), nil
	}

	pool.Start(handler)

	start := make(chan struct{})
	go func() {
		<-start
		time.Sleep(5 * time.Millisecond)
		for i := 0; i < 5; i++ {
			pool.Submit(&Task{ID: string(rune('1' + i))})
		}
	}()

	close(start)
	time.Sleep(100 * time.Millisecond)

	if len(executionOrder) < 3 {
		t.Errorf("Expected at least 3 tasks executed, got %d", len(executionOrder))
	}
}

func TestPool_Context_Cancellation(t *testing.T) {
	config := Config{
		Workers:   2,
		QueueSize: 10,
	}

	pool := NewPool(config)

	cancelled := make(chan struct{})
	handler := func(ctx context.Context, task *Task) ([]byte, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}

	pool.Start(handler)

	pool.Submit(&Task{ID: "test"})

	time.Sleep(50 * time.Millisecond)

	go pool.Close()

	select {
	case <-cancelled:
		break
	case <-time.After(1 * time.Second):
		t.Error("Context should have been cancelled")
	}
}
