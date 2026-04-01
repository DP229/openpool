// Package queue provides task queuing and worker pool management.
package queue

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Task represents a queued task.
type Task struct {
	ID         string          `json:"id"`
	Op         string          `json:"op"`
	Input      json.RawMessage `json:"input"`
	Priority   int             `json:"priority"`  // Higher = more important
	Credits    int             `json:"credits"`
	Timeout    int             `json:"timeout_sec"`
	Submitted  time.Time       `json:"submitted"`
	Status     string          `json:"status"` // pending, running, completed, failed
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	Completed  *time.Time      `json:"completed,omitempty"`
}

// Queue manages pending tasks with priority ordering.
type Queue struct {
	tasks   []*Task
	mu      sync.RWMutex
	maxSize int
	notify  chan struct{}
}

// NewQueue creates a new task queue.
func NewQueue(maxSize int) *Queue {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &Queue{
		tasks:   make([]*Task, 0),
		maxSize: maxSize,
		notify:  make(chan struct{}, 1),
	}
}

// Enqueue adds a task to the queue.
func (q *Queue) Enqueue(task *Task) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tasks) >= q.maxSize {
		return ErrQueueFull
	}

	task.Status = "pending"
	task.Submitted = time.Now()
	q.tasks = append(q.tasks, task)
	q.sort()
	q.notifyChan()

	return nil
}

// Dequeue removes and returns the highest priority task.
func (q *Queue) Dequeue() *Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tasks) == 0 {
		return nil
	}

	task := q.tasks[0]
	q.tasks = q.tasks[1:]
	return task
}

// Peek returns the next task without removing it.
func (q *Queue) Peek() *Task {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if len(q.tasks) == 0 {
		return nil
	}
	return q.tasks[0]
}

// Size returns the queue size.
func (q *Queue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.tasks)
}

// GetAll returns all pending tasks.
func (q *Queue) GetAll() []*Task {
	q.mu.RLock()
	defer q.mu.RUnlock()

	result := make([]*Task, len(q.tasks))
	copy(result, q.tasks)
	return result
}

// Remove removes a task by ID.
func (q *Queue) Remove(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, t := range q.tasks {
		if t.ID == id {
			q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)
			return true
		}
	}
	return false
}

// Wait blocks until a task is available or context is done.
func (q *Queue) Wait(ctx context.Context) *Task {
	for {
		if task := q.Dequeue(); task != nil {
			return task
		}

		select {
		case <-ctx.Done():
			return nil
		case <-q.notify:
			// Check again
		case <-time.After(100 * time.Millisecond):
			// Poll periodically
		}
	}
}

func (q *Queue) sort() {
	// Sort by priority (descending), then by submission time (ascending)
	for i := 0; i < len(q.tasks)-1; i++ {
		for j := i + 1; j < len(q.tasks); j++ {
			if q.tasks[i].Priority < q.tasks[j].Priority ||
				(q.tasks[i].Priority == q.tasks[j].Priority && q.tasks[i].Submitted.Before(q.tasks[j].Submitted)) {
				q.tasks[i], q.tasks[j] = q.tasks[j], q.tasks[i]
			}
		}
	}
}

func (q *Queue) notifyChan() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Error definitions
var ErrQueueFull = &QueueError{msg: "queue is full"}
var ErrTaskNotFound = &QueueError{msg: "task not found"}

type QueueError struct {
	msg string
}

func (e *QueueError) Error() string {
	return e.msg
}

// Worker processes tasks from the queue.
type Worker struct {
	id     int
	queue  *Queue
	exec   func(context.Context, *Task) (json.RawMessage, error)
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// WorkerPool manages multiple workers.
type WorkerPool struct {
	workers []*Worker
	queue   *Queue
	workersM int
	running  bool
	wg       sync.WaitGroup
	mu       sync.RWMutex
}

// NewWorkerPool creates a worker pool.
func NewWorkerPool(queueSize, workers int) *WorkerPool {
	if workers <= 0 {
		workers = 1
	}
	if queueSize <= 0 {
		queueSize = 100
	}
	return &WorkerPool{
		queue:    NewQueue(queueSize),
		workersM: workers,
	}
}

// SetExecutor sets the task execution function.
func (p *WorkerPool) SetExecutor(fn func(context.Context, *Task) (json.RawMessage, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop existing workers
	for _, w := range p.workers {
		w.cancel()
	}
	p.workers = nil
	p.running = false

	// Create new workers
	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < p.workersM; i++ {
		worker := &Worker{
			id:     i,
			queue:  p.queue,
			exec:   fn,
			ctx:    ctx,
			cancel: cancel,
		}
		p.workers = append(p.workers, worker)
	}

	p.running = true
	for _, w := range p.workers {
		p.wg.Add(1)
		go w.run(&p.wg)
	}
}

// Start begins the worker pool.
func (p *WorkerPool) Start() {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.running {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < p.workersM; i++ {
		worker := &Worker{
			id:     i,
			queue:  p.queue,
			ctx:    ctx,
			cancel: cancel,
		}
		p.workers = append(p.workers, worker)
		p.wg.Add(1)
		go worker.run(&p.wg)
	}
	p.running = true
}

// Stop stops all workers.
func (p *WorkerPool) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, w := range p.workers {
		w.cancel()
	}
	p.wg.Wait()
	p.running = false
}

// Submit adds a task to the pool.
func (p *WorkerPool) Submit(task *Task) error {
	return p.queue.Enqueue(task)
}

// GetQueue returns the task queue.
func (p *WorkerPool) GetQueue() *Queue {
	return p.queue
}

func (w *Worker) run(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		task := w.queue.Wait(w.ctx)
		if task == nil {
			return // Context cancelled
		}

		task.Status = "running"
		ctx, cancel := context.WithTimeout(w.ctx, time.Duration(task.Timeout)*time.Second)

		result, err := w.exec(ctx, task)
		cancel()

		task.Completed = timePtr(time.Now())
		if err != nil {
			task.Status = "failed"
			task.Error = err.Error()
		} else {
			task.Status = "completed"
			task.Result = result
		}
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}