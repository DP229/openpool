package scoring

import (
	"context"
	"sync"
	"time"
)

// AsyncResult wraps a result with its channel
type AsyncResult struct {
	Result *ResultWrapper
	Err    error
}

// ResultWrapper wraps task results with metadata
type ResultWrapper struct {
	TaskID    string
	NodeID   string
	Output   []byte
	Latency  int
	Cost     int
	Score    float64
	Steps    []Step
	Success  bool
	ReceivedAt time.Time
}

// Step represents a single execution step
type Step struct {
	ID        int       `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Duration  int       `json:"duration_ms"`
}

// AsyncExecutor executes tasks asynchronously with worker pool
type AsyncExecutor struct {
	pool    *WorkerPool
	results chan *AsyncResult
}

// WorkerPool manages a pool of workers
type WorkerPool struct {
	workers   int
	taskQueue chan func()
	wg        sync.WaitGroup
}

// NewAsyncExecutor creates a new async executor
func NewAsyncExecutor(workers int) *AsyncExecutor {
	if workers <= 0 {
		workers = 4
	}

	pool := &WorkerPool{
		workers:   workers,
		taskQueue: make(chan func(), 100),
	}

	// Start workers
	for i := 0; i < workers; i++ {
		pool.wg.Add(1)
		go pool.worker()
	}

	return &AsyncExecutor{
		pool:    pool,
		results: make(chan *AsyncResult, 50),
	}
}

// worker is the worker goroutine
func (p *WorkerPool) worker() {
	defer p.wg.Done()
	for task := range p.taskQueue {
		task()
	}
}

// Submit submits a task for async execution
func (e *AsyncExecutor) Submit(task func() (interface{}, error)) {
	go func() {
		result, err := task()
		
		var wrapped *ResultWrapper
		if result != nil {
			if r, ok := result.(*ResultWrapper); ok {
				wrapped = r
			}
		}
		
		e.results <- &AsyncResult{
			Result: wrapped,
			Err:    err,
		}
	}()
}

// SubmitTask submits a task with the worker pool
func (e *AsyncExecutor) SubmitTask(task func()) {
	e.pool.taskQueue <- task
}

// Results returns the results channel
func (e *AsyncExecutor) Results() <-chan *AsyncResult {
	return e.results
}

// Close waits for all tasks to complete and closes resources
func (e *AsyncExecutor) Close() {
	close(e.pool.taskQueue)
	e.pool.wg.Wait()
	close(e.results)
}

// ParallelExecute executes multiple tasks in parallel
func ParallelExecute(tasks []func() (interface{}, error), maxConcurrency int) []*AsyncResult {
	if maxConcurrency <= 0 {
		maxConcurrency = 4
	}

	sem := make(chan struct{}, maxConcurrency)
	results := make([]*AsyncResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t func() (interface{}, error)) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			result, err := t()
			var wrapped *ResultWrapper
			if result != nil {
				if r, ok := result.(*ResultWrapper); ok {
					wrapped = r
				}
			}
			results[idx] = &AsyncResult{
				Result: wrapped,
				Err:    err,
			}
		}(i, task)
	}

	wg.Wait()
	return results
}

// ExecuteWithRetry executes a task with retry logic
func ExecuteWithRetry(ctx context.Context, task func() error, maxRetries int, backoff time.Duration) error {
	var err error
	for i := 0; i <= maxRetries; i++ {
		if err = task(); err == nil {
			return nil
		}

		if i < maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff * time.Duration(i+1)):
				// Continue
			}
		}
	}
	return err
}

// TimeoutExecute executes a task with a timeout
func TimeoutExecute(ctx context.Context, task func() (interface{}, error), timeout time.Duration) (interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		val interface{}
		err error
	}

	resultCh := make(chan result, 1)
	go func() {
		val, err := task()
		resultCh <- result{val, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-resultCh:
		return r.val, r.err
	}
}
