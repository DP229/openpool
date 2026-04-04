package executor

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dp229/openpool/pkg/resilience"
	"github.com/dp229/openpool/pkg/worker"
)

func TestIntegratedExecutor_ExecuteWithProtection(t *testing.T) {
	config := IntegratedConfig{
		Workers:        2,
		QueueSize:      10,
		TaskTimeout:    5 * time.Second,
		MaxFailures:    3,
		CircuitTimeout: 10 * time.Second,
	}

	executor := NewIntegratedExecutor(config)

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		result := map[string]interface{}{
			"task_id": task.ID,
			"result":  "success",
		}
		return json.Marshal(result)
	}

	if err := executor.Start(handler); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}
	defer executor.Shutdown()

	task := &Task{
		ID:         "test-task-1",
		TimeoutSec: 5,
		RawInput:   json.RawMessage(`{"input": "test"}`),
	}

	result, err := executor.ExecuteWithProtection(context.Background(), task)
	if err != nil {
		t.Fatalf("ExecuteWithProtection failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.DurationMs < 0 {
		t.Errorf("Expected non-negative DurationMs, got %d", result.DurationMs)
	}

	stats := executor.GetStats()
	if stats.TasksSubmitted != 1 {
		t.Errorf("Expected TasksSubmitted=1, got %d", stats.TasksSubmitted)
	}
	if stats.TasksCompleted != 1 {
		t.Errorf("Expected TasksCompleted=1, got %d", stats.TasksCompleted)
	}
}

func TestIntegratedExecutor_CircuitBreaker(t *testing.T) {
	config := IntegratedConfig{
		Workers:        1,
		QueueSize:      5,
		TaskTimeout:    2 * time.Second,
		MaxFailures:    2,
		CircuitTimeout: 5 * time.Second,
	}

	executor := NewIntegratedExecutor(config)

	failCount := atomic.Int32{}
	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		if failCount.Load() < 3 {
			failCount.Add(1)
			return nil, context.DeadlineExceeded
		}
		return json.RawMessage(`{"status": "ok"}`), nil
	}

	if err := executor.Start(handler); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}
	defer executor.Shutdown()

	task := &Task{
		ID:         "test-task-fail",
		TimeoutSec: 1,
		RawInput:   json.RawMessage(`{}`),
	}

	for i := 0; i < 3; i++ {
		_, err := executor.ExecuteWithProtection(context.Background(), task)
		if err == nil {
			t.Errorf("Expected error on attempt %d", i+1)
		}
	}

	state := executor.GetCircuitBreakerState()
	if state == resilience.StateOpen {
	}

	stats := executor.GetStats()
	if stats.TasksFailed == 0 {
		t.Error("Expected at least one failed task")
	}
}

func TestIntegratedExecutor_Shutdown(t *testing.T) {
	config := IntegratedConfig{
		Workers:     2,
		QueueSize:   5,
		TaskTimeout: 5 * time.Second,
	}

	executor := NewIntegratedExecutor(config)

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		time.Sleep(100 * time.Millisecond)
		return json.RawMessage(`{"done": true}`), nil
	}

	if err := executor.Start(handler); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}

	if executor.IsShuttingDown() {
		t.Error("Executor should not be shutting down")
	}

	err := executor.Shutdown()
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	if !executor.IsShuttingDown() {
		t.Error("Executor should be marked as shutting down")
	}

	task := &Task{
		ID:         "post-shutdown-task",
		TimeoutSec: 1,
	}

	_, err = executor.ExecuteWithProtection(context.Background(), task)
	if err == nil {
		t.Error("Expected error when submitting to shutdown executor")
	}
}

func TestBatchExecutor_ExecuteBatch(t *testing.T) {
	executor := NewIntegratedExecutor(IntegratedConfig{
		Workers:     4,
		QueueSize:   20,
		TaskTimeout: 5 * time.Second,
	})

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		time.Sleep(50 * time.Millisecond)
		result := map[string]string{"task_id": task.ID, "status": "done"}
		return json.Marshal(result)
	}

	if err := executor.Start(handler); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}
	defer executor.Shutdown()

	batch := NewBatchExecutor(executor, 10, 50)

	tasks := make([]*Task, 5)
	for i := 0; i < 5; i++ {
		tasks[i] = &Task{
			ID:         string(rune('A' + i)),
			TimeoutSec: 2,
			RawInput:   json.RawMessage(`{}`),
		}
	}

	results, err := batch.ExecuteBatch(context.Background(), tasks)
	if err != nil {
		t.Fatalf("ExecuteBatch failed: %v", err)
	}

	if len(results) != 5 {
		t.Errorf("Expected 5 results, got %d", len(results))
	}

	for i, result := range results {
		if result == nil {
			t.Errorf("Result %d is nil", i)
		}
	}

	stats := executor.GetStats()
	if stats.TasksCompleted < 5 {
		t.Errorf("Expected at least 5 completed, got %d", stats.TasksCompleted)
	}
}

func TestIntegratedExecutor_HealthCheck(t *testing.T) {
	executor := NewIntegratedExecutor(IntegratedConfig{
		Workers:     2,
		QueueSize:   10,
		TaskTimeout: 5 * time.Second,
	})

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		return json.RawMessage(`{}`), nil
	}

	if err := executor.Start(handler); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}
	defer executor.Shutdown()

	health := executor.HealthCheck()

	if health == nil {
		t.Fatal("HealthCheck returned nil")
	}

	execMap, ok := health["executor"].(map[string]interface{})
	if !ok {
		t.Fatal("executor stats missing from health check")
	}

	if execMap["shutting_down"].(bool) {
		t.Error("Executor should not be shutting down")
	}

	cbMap, ok := health["circuit_breaker"].(map[string]interface{})
	if !ok {
		t.Fatal("circuit_breaker stats missing from health check")
	}

	if cbMap["state"].(string) != "closed" {
		t.Errorf("Expected circuit breaker state 'closed', got '%s'", cbMap["state"])
	}

	poolMap, ok := health["worker_pool"].(map[string]interface{})
	if !ok {
		t.Fatal("worker_pool stats missing from health check")
	}

	if poolMap["active_workers"].(int64) < 0 {
		t.Error("Invalid active workers count")
	}
}
