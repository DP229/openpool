package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dp229/openpool/pkg/ledger"
	"github.com/dp229/openpool/pkg/resilience"
	"github.com/dp229/openpool/pkg/shutdown"
	"github.com/dp229/openpool/pkg/verification"
	"github.com/dp229/openpool/pkg/worker"
)

type IntegratedExecutor struct {
	pool           *worker.Pool
	circuitBreaker *resilience.CircuitBreaker
	verifier       *verification.Verifier
	ledger         *ledger.Ledger
	nodeID         string

	taskHandler worker.TaskHandler
	stats       *ExecutionStats

	mu           sync.RWMutex
	shuttingDown bool
}

type ExecutionStats struct {
	TasksSubmitted int64
	TasksCompleted int64
	TasksFailed    int64
	TasksRejected  int64
	AverageLatency int64
	TotalLatency   int64
	CircuitOpens   int64
	LastExecTime   int64
}

type IntegratedConfig struct {
	Workers        int
	QueueSize      int
	TaskTimeout    time.Duration
	MaxFailures    int
	CircuitTimeout time.Duration
	EnableVerifier bool
}

func NewIntegratedExecutor(config IntegratedConfig) *IntegratedExecutor {
	if config.Workers <= 0 {
		config.Workers = 4
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 100
	}
	if config.TaskTimeout <= 0 {
		config.TaskTimeout = 30 * time.Second
	}
	if config.MaxFailures <= 0 {
		config.MaxFailures = 5
	}
	if config.CircuitTimeout <= 0 {
		config.CircuitTimeout = 60 * time.Second
	}

	return &IntegratedExecutor{
		pool: worker.NewPool(worker.Config{
			Workers:     config.Workers,
			QueueSize:   config.QueueSize,
			TaskTimeout: config.TaskTimeout,
		}),
		circuitBreaker: resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
			Name:          "task-executor",
			MaxFailures:   config.MaxFailures,
			Timeout:       config.CircuitTimeout,
			HalfOpenLimit: 3,
		}),
		stats: &ExecutionStats{},
	}
}

func (e *IntegratedExecutor) SetNodeID(id string) {
	e.nodeID = id
}

func (e *IntegratedExecutor) SetLedger(l *ledger.Ledger) {
	e.ledger = l
}

func (e *IntegratedExecutor) SetVerifier(v *verification.Verifier) {
	e.verifier = v
}

func (e *IntegratedExecutor) Start(taskHandler worker.TaskHandler) error {
	e.taskHandler = taskHandler
	return e.pool.Start(taskHandler)
}

func (e *IntegratedExecutor) ExecuteWithProtection(ctx context.Context, task *Task) (*ExecutionResult, error) {
	if e.IsShuttingDown() {
		return nil, fmt.Errorf("executor is shutting down")
	}

	atomic.AddInt64(&e.stats.TasksSubmitted, 1)

	resultChan := make(chan *ExecutionResult, 1)
	errChan := make(chan error, 1)

	go func() {
		result, err := e.circuitBreaker.ExecuteWithResult(func() (interface{}, error) {
			return e.executeTask(ctx, task)
		})

		if err != nil {
			if err == resilience.ErrCircuitOpen {
				atomic.AddInt64(&e.stats.CircuitOpens, 1)
				atomic.AddInt64(&e.stats.TasksRejected, 1)
			}
			errChan <- err
			return
		}
		resultChan <- result.(*ExecutionResult)
	}()

	select {
	case result := <-resultChan:
		atomic.AddInt64(&e.stats.TasksCompleted, 1)
		e.updateLatency(result.DurationMs)
		return result, nil

	case err := <-errChan:
		atomic.AddInt64(&e.stats.TasksFailed, 1)
		return nil, err

	case <-ctx.Done():
		atomic.AddInt64(&e.stats.TasksFailed, 1)
		return nil, ctx.Err()
	}
}

func (e *IntegratedExecutor) executeTask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	startTime := time.Now()

	timeout := time.Duration(task.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 4*time.Hour {
		timeout = 4 * time.Hour
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workerTask := &worker.Task{
		ID:        task.ID,
		Type:      "wasm",
		Data:      task.RawInput,
		Timeout:   timeout,
		Timestamp: time.Now(),
	}

	result, err := e.taskHandler(execCtx, workerTask)
	if err != nil {
		e.recordVerification(task, result, startTime, err)
		return nil, err
	}

	execDuration := time.Since(startTime)

	if !json.Valid(result) {
		e.recordVerification(task, result, startTime, fmt.Errorf("invalid JSON output"))
		return nil, fmt.Errorf("task returned invalid JSON")
	}

	e.recordVerification(task, result, startTime, nil)

	return &ExecutionResult{
		Result:     result,
		DurationMs: execDuration.Milliseconds(),
		Verified:   e.shouldVerify(task.Credits),
		InputHash:  verification.HashInput(task.RawInput),
		OutputHash: verification.HashOutput(result),
	}, nil
}

func (e *IntegratedExecutor) recordVerification(task *Task, result []byte, startTime time.Time, execErr error) {
	if e.verifier == nil || task.ID == "" {
		return
	}

	inputHash := verification.HashInput(task.RawInput)
	outputHash := verification.HashOutput(result)
	execDuration := time.Since(startTime)

	verr := verification.VerificationResult{
		TaskID:       task.ID,
		Method:       verification.MethodNone,
		PrimaryNode:  task.NodeID,
		VerifierNode: e.nodeID,
		InputHash:    inputHash,
		OutputHash:   outputHash,
		Match:        execErr == nil,
		DurationMs:   execDuration.Milliseconds(),
		Timestamp:    time.Now().Unix(),
	}

	if execErr != nil {
		verr.Error = execErr.Error()
	}

	if e.shouldVerify(task.Credits) {
		verr.Method = verification.MethodRedundant
	}

	e.verifier.RecordVerification(context.Background(), verr)
}

func (e *IntegratedExecutor) shouldVerify(credits int) bool {
	if e.verifier == nil {
		return false
	}
	return e.verifier.ShouldVerify(credits)
}

func (e *IntegratedExecutor) updateLatency(durationMs int64) {
	for {
		old := atomic.LoadInt64(&e.stats.TotalLatency)
		count := atomic.LoadInt64(&e.stats.TasksCompleted)
		newTotal := old + durationMs
		if atomic.CompareAndSwapInt64(&e.stats.TotalLatency, old, newTotal) {
			if count > 0 {
				atomic.StoreInt64(&e.stats.AverageLatency, newTotal/count)
			}
			break
		}
	}
	atomic.StoreInt64(&e.stats.LastExecTime, time.Now().Unix())
}

func (e *IntegratedExecutor) SubmitToPool(task *Task, handler worker.TaskHandler) error {
	if e.IsShuttingDown() {
		return fmt.Errorf("executor is shutting down")
	}

	workerTask := &worker.Task{
		ID:        task.ID,
		Type:      "wasm",
		Data:      task.RawInput,
		Timeout:   time.Duration(task.TimeoutSec) * time.Second,
		Timestamp: time.Now(),
	}

	timeout := time.Duration(task.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return e.pool.SubmitWithTimeout(workerTask, timeout)
}

func (e *IntegratedExecutor) Results() <-chan *worker.TaskResult {
	return e.pool.Results()
}

func (e *IntegratedExecutor) GetStats() *ExecutionStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return &ExecutionStats{
		TasksSubmitted: atomic.LoadInt64(&e.stats.TasksSubmitted),
		TasksCompleted: atomic.LoadInt64(&e.stats.TasksCompleted),
		TasksFailed:    atomic.LoadInt64(&e.stats.TasksFailed),
		TasksRejected:  atomic.LoadInt64(&e.stats.TasksRejected),
		AverageLatency: atomic.LoadInt64(&e.stats.AverageLatency),
		CircuitOpens:   atomic.LoadInt64(&e.stats.CircuitOpens),
		LastExecTime:   atomic.LoadInt64(&e.stats.LastExecTime),
	}
}

func (e *IntegratedExecutor) GetCircuitBreakerState() resilience.State {
	return e.circuitBreaker.GetState()
}

func (e *IntegratedExecutor) GetPoolStats() worker.PoolStatistics {
	return e.pool.GetStats()
}

func (e *IntegratedExecutor) IsShuttingDown() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.shuttingDown
}

func (e *IntegratedExecutor) Shutdown() error {
	e.mu.Lock()
	e.shuttingDown = true
	e.mu.Unlock()

	return e.pool.Close()
}

func (e *IntegratedExecutor) RegisterShutdownHandler(gs *shutdown.GracefulShutdown) {
	gs.Register("executor", func(ctx context.Context) error {
		e.mu.Lock()
		e.shuttingDown = true
		e.mu.Unlock()

		poolClose := make(chan error, 1)
		go func() {
			poolClose <- e.pool.Close()
		}()

		select {
		case err := <-poolClose:
			return err
		case <-ctx.Done():
			return fmt.Errorf("executor shutdown timed out")
		}
	})
}

type BatchExecutor struct {
	executor   *IntegratedExecutor
	batchSize  int
	maxPending int
}

func NewBatchExecutor(executor *IntegratedExecutor, batchSize, maxPending int) *BatchExecutor {
	if batchSize <= 0 {
		batchSize = 10
	}
	if maxPending <= 0 {
		maxPending = 100
	}

	return &BatchExecutor{
		executor:   executor,
		batchSize:  batchSize,
		maxPending: maxPending,
	}
}

func (be *BatchExecutor) ExecuteBatch(ctx context.Context, tasks []*Task) ([]*ExecutionResult, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	if be.executor.IsShuttingDown() {
		return nil, fmt.Errorf("executor is shutting down")
	}

	results := make([]*ExecutionResult, len(tasks))
	errs := make([]error, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t *Task) {
			defer wg.Done()
			results[idx], errs[idx] = be.executor.ExecuteWithProtection(ctx, t)
		}(i, task)
	}

	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return results, fmt.Errorf("batch had errors: %v", err)
		}
	}

	return results, nil
}

func (e *IntegratedExecutor) HealthCheck() map[string]interface{} {
	stats := e.GetStats()
	cbState := e.GetCircuitBreakerState()
	poolStats := e.GetPoolStats()

	return map[string]interface{}{
		"executor": map[string]interface{}{
			"tasks_submitted": stats.TasksSubmitted,
			"tasks_completed": stats.TasksCompleted,
			"tasks_failed":    stats.TasksFailed,
			"tasks_rejected":  stats.TasksRejected,
			"avg_latency_ms":  stats.AverageLatency,
			"shutting_down":   e.IsShuttingDown(),
		},
		"circuit_breaker": map[string]interface{}{
			"state":         cbState.String(),
			"circuit_opens": stats.CircuitOpens,
		},
		"worker_pool": map[string]interface{}{
			"queue_length":   poolStats.QueueLength,
			"active_workers": poolStats.ActiveWorkers,
			"pending_tasks":  poolStats.PendingTasks,
		},
	}
}
