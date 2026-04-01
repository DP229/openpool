package queue

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestNewQueue(t *testing.T) {
	tests := []struct {
		name    string
		maxSize int
		want    int
	}{
		{"default", 0, 100},
		{"negative", -1, 100},
		{"custom", 50, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := NewQueue(tt.maxSize)
			if q.maxSize != tt.want {
				t.Errorf("NewQueue().maxSize = %d, want %d", q.maxSize, tt.want)
			}
		})
	}
}

func TestQueueEnqueueDequeue(t *testing.T) {
	q := NewQueue(10)

	task1 := &Task{ID: "task-1", Op: "fib", Priority: 1}
	task2 := &Task{ID: "task-2", Op: "fib", Priority: 2}
	task3 := &Task{ID: "task-3", Op: "fib", Priority: 1}

	if err := q.Enqueue(task1); err != nil {
		t.Fatalf("Enqueue task1 failed: %v", err)
	}
	if err := q.Enqueue(task2); err != nil {
		t.Fatalf("Enqueue task2 failed: %v", err)
	}
	if err := q.Enqueue(task3); err != nil {
		t.Fatalf("Enqueue task3 failed: %v", err)
	}

	if q.Size() != 3 {
		t.Errorf("Queue length = %d, want 3", q.Size())
	}

	next := q.Dequeue()
	if next == nil {
		t.Fatal("Dequeue returned nil")
	}
	if next.ID != "task-2" {
		t.Errorf("First task = %s, want task-2 (highest priority)", next.ID)
	}

	next = q.Dequeue()
	if next == nil {
		t.Fatal("Dequeue returned nil")
	}
	if next.ID != "task-1" && next.ID != "task-3" {
		t.Errorf("Second task should be task-1 or task-3, got %s", next.ID)
	}
}

func TestQueueFull(t *testing.T) {
	q := NewQueue(2)

	if err := q.Enqueue(&Task{ID: "1"}); err != nil {
		t.Fatalf("Enqueue 1 failed: %v", err)
	}
	if err := q.Enqueue(&Task{ID: "2"}); err != nil {
		t.Fatalf("Enqueue 2 failed: %v", err)
	}

	err := q.Enqueue(&Task{ID: "3"})
	if err != ErrQueueFull {
		t.Errorf("Enqueue on full queue = %v, want ErrQueueFull", err)
	}
}

func TestQueuePriorityOrder(t *testing.T) {
	q := NewQueue(10)

	tasks := []*Task{
		{ID: "low", Priority: 1, Op: "fib"},
		{ID: "high", Priority: 10, Op: "fib"},
		{ID: "medium", Priority: 5, Op: "fib"},
		{ID: "highest", Priority: 100, Op: "fib"},
	}

	for _, task := range tasks {
		if err := q.Enqueue(task); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	expected := []string{"highest", "high", "medium", "low"}
	for i, want := range expected {
		task := q.Dequeue()
		if task == nil {
			t.Fatalf("Task %d is nil", i)
		}
		if task.ID != want {
			t.Errorf("Task %d = %s, want %s", i, task.ID, want)
		}
	}
}

func TestQueueConcurrent(t *testing.T) {
	q := NewQueue(1000)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			task := &Task{ID: string(rune(n)), Priority: n}
			q.Enqueue(task)
		}(i)
	}

	wg.Wait()

	if q.Size() != 100 {
		t.Errorf("Queue length = %d, want 100", q.Size())
	}
}

func TestWorkerPool(t *testing.T) {
	wp := NewWorkerPool(10, 2)

	var processed []string
	var mu sync.Mutex

	handler := func(ctx context.Context, task *Task) (json.RawMessage, error) {
		mu.Lock()
		processed = append(processed, task.ID)
		mu.Unlock()
		return nil, nil
	}

	wp.SetExecutor(handler)

	tasks := []*Task{
		{ID: "task-1", Op: "fib", Priority: 1},
		{ID: "task-2", Op: "fib", Priority: 2},
		{ID: "task-3", Op: "fib", Priority: 3},
	}

	for _, task := range tasks {
		wp.Submit(task)
	}

	time.Sleep(100 * time.Millisecond)
	wp.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(processed) != 3 {
		t.Errorf("Processed %d tasks, want 3", len(processed))
	}
}

func TestQueuePeek(t *testing.T) {
	q := NewQueue(10)

	task := q.Peek()
	if task != nil {
		t.Error("Peek on empty queue should return nil")
	}

	q.Enqueue(&Task{ID: "first", Priority: 1})
	q.Enqueue(&Task{ID: "second", Priority: 2})

	peeked := q.Peek()
	if peeked == nil {
		t.Fatal("Peek returned nil")
	}
	if peeked.ID != "second" {
		t.Errorf("Peek returned %s, want second (highest priority)", peeked.ID)
	}

	if q.Size() != 2 {
		t.Errorf("Peek should not remove items, size = %d, want 2", q.Size())
	}
}

func TestQueueRemove(t *testing.T) {
	q := NewQueue(10)

	q.Enqueue(&Task{ID: "task-1"})
	q.Enqueue(&Task{ID: "task-2"})
	q.Enqueue(&Task{ID: "task-3"})

	if !q.Remove("task-2") {
		t.Error("Remove should return true for existing task")
	}

	if q.Size() != 2 {
		t.Errorf("Size after remove = %d, want 2", q.Size())
	}

	if q.Remove("nonexistent") {
		t.Error("Remove should return false for nonexistent task")
	}
}

func TestQueueGetAll(t *testing.T) {
	q := NewQueue(10)

	q.Enqueue(&Task{ID: "task-1"})
	q.Enqueue(&Task{ID: "task-2"})

	all := q.GetAll()
	if len(all) != 2 {
		t.Errorf("GetAll returned %d tasks, want 2", len(all))
	}
}

func TestQueueEmpty(t *testing.T) {
	q := NewQueue(10)

	task := q.Dequeue()
	if task != nil {
		t.Error("Dequeue on empty queue should return nil")
	}

	task = q.Peek()
	if task != nil {
		t.Error("Peek on empty queue should return nil")
	}
}