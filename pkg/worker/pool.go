package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrPoolClosed  = errors.New("worker pool is closed")
	ErrPoolFull    = errors.New("worker pool is full")
	ErrTaskTimeout = errors.New("task execution timeout")
)

type Task struct {
	ID        string
	Priority  int
	Data      []byte
	Type      string
	Timeout   time.Duration
	Timestamp time.Time
}

type TaskResult struct {
	TaskID    string
	Result    []byte
	Error     error
	Duration  time.Duration
	Timestamp time.Time
}

type TaskHandler func(ctx context.Context, task *Task) ([]byte, error)

type Pool struct {
	workers     int
	queueSize   int
	taskQueue   chan *Task
	resultQueue chan *TaskResult
	handler     TaskHandler

	workersRunning int64
	tasksSubmitted int64
	tasksCompleted int64
	tasksFailed    int64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.RWMutex
	closed bool

	statistics *Statistics
}

type Statistics struct {
	QueueLength    int64
	ActiveWorkers  int64
	TasksSubmitted int64
	TasksCompleted int64
	TasksFailed    int64
	AverageLatency time.Duration
	TotalLatency   time.Duration
}

type Config struct {
	Workers     int
	QueueSize   int
	TaskTimeout time.Duration
}

func NewPool(config Config) *Pool {
	if config.Workers <= 0 {
		config.Workers = 4
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 100
	}
	if config.TaskTimeout <= 0 {
		config.TaskTimeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Pool{
		workers:     config.Workers,
		queueSize:   config.QueueSize,
		taskQueue:   make(chan *Task, config.QueueSize),
		resultQueue: make(chan *TaskResult, config.QueueSize),
		ctx:         ctx,
		cancel:      cancel,
		statistics:  &Statistics{},
	}
}

func (p *Pool) Start(handler TaskHandler) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrPoolClosed
	}

	p.handler = handler

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}

	return nil
}

func (p *Pool) Submit(task *Task) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return ErrPoolClosed
	}

	if task.Timestamp.IsZero() {
		task.Timestamp = time.Now()
	}

	select {
	case p.taskQueue <- task:
		atomic.AddInt64(&p.tasksSubmitted, 1)
		atomic.AddInt64(&p.statistics.TasksSubmitted, 1)
		atomic.StoreInt64(&p.statistics.QueueLength, int64(len(p.taskQueue)))
		return nil
	case <-p.ctx.Done():
		return ErrPoolClosed
	default:
		return ErrPoolFull
	}
}

func (p *Pool) SubmitWithTimeout(task *Task, timeout time.Duration) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return ErrPoolClosed
	}

	if task.Timestamp.IsZero() {
		task.Timestamp = time.Now()
	}

	select {
	case p.taskQueue <- task:
		atomic.AddInt64(&p.tasksSubmitted, 1)
		return nil
	case <-time.After(timeout):
		return ErrTaskTimeout
	case <-p.ctx.Done():
		return ErrPoolClosed
	}
}

func (p *Pool) Results() <-chan *TaskResult {
	return p.resultQueue
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()

	atomic.AddInt64(&p.workersRunning, 1)
	atomic.AddInt64(&p.statistics.ActiveWorkers, 1)
	defer atomic.AddInt64(&p.workersRunning, -1)
	defer atomic.AddInt64(&p.statistics.ActiveWorkers, -1)

	for {
		select {
		case task, ok := <-p.taskQueue:
			if !ok {
				return
			}

			p.executeTask(task)

		case <-p.ctx.Done():
			drained := 0
			for {
				select {
				case task := <-p.taskQueue:
					p.executeTask(task)
					drained++
				default:
					return
				}
			}
		}
	}
}

func (p *Pool) executeTask(task *Task) {
	start := time.Now()

	taskTimeout := task.Timeout
	if taskTimeout == 0 {
		taskTimeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(p.ctx, taskTimeout)
	defer cancel()

	var result []byte
	var err error

	done := make(chan struct{})
	go func() {
		defer close(done)
		result, err = p.handler(ctx, task)
	}()

	select {
	case <-done:
		break
	case <-ctx.Done():
		err = ctx.Err()
	}

	duration := time.Since(start)

	p.resultQueue <- &TaskResult{
		TaskID:    task.ID,
		Result:    result,
		Error:     err,
		Duration:  duration,
		Timestamp: time.Now(),
	}

	if err != nil {
		atomic.AddInt64(&p.tasksFailed, 1)
		atomic.AddInt64(&p.statistics.TasksFailed, 1)
	} else {
		atomic.AddInt64(&p.tasksCompleted, 1)
		atomic.AddInt64(&p.statistics.TasksCompleted, 1)
	}
}

func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrPoolClosed
	}

	p.closed = true
	p.cancel()

	p.wg.Wait()

	close(p.taskQueue)
	close(p.resultQueue)

	return nil
}

func (p *Pool) Drain(timeout time.Duration) int {
	drained := 0
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-p.resultQueue:
			drained++
		default:
			time.Sleep(10 * time.Millisecond)
			if len(p.taskQueue) == 0 && len(p.resultQueue) == 0 {
				return drained
			}
		}
	}

	return drained
}

func (p *Pool) Stats() *Statistics {
	p.mu.RLock()
	defer p.mu.RUnlock()

	completed := atomic.LoadInt64(&p.statistics.TasksCompleted)
	failed := atomic.LoadInt64(&p.statistics.TasksFailed)

	stats := &Statistics{
		QueueLength:    int64(len(p.taskQueue)),
		ActiveWorkers:  atomic.LoadInt64(&p.statistics.ActiveWorkers),
		TasksSubmitted: atomic.LoadInt64(&p.statistics.TasksSubmitted),
		TasksCompleted: completed,
		TasksFailed:    failed,
		AverageLatency: 0,
		TotalLatency:   0,
	}

	if completed+failed > 0 {
		stats.AverageLatency = time.Duration(0)
	}

	return stats
}

func (p *Pool) GetStats() PoolStatistics {
	s := p.Stats()
	return PoolStatistics{
		QueueLength:   s.QueueLength,
		ActiveWorkers: s.ActiveWorkers,
		PendingTasks:  s.QueueLength,
	}
}

type PoolStatistics struct {
	QueueLength   int64
	ActiveWorkers int64
	PendingTasks  int64
}

func (p *Pool) QueueSize() int {
	return len(p.taskQueue)
}

func (p *Pool) WorkerCount() int {
	return p.workers
}

func (p *Pool) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return !p.closed
}
