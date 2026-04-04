package benchmark

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dp229/openpool/pkg/worker"
)

type BenchmarkResult struct {
	Name          string
	Duration      time.Duration
	Operations    int64
	OpsPerSecond  float64
	LatencyAvg    time.Duration
	LatencyMin    time.Duration
	LatencyMax    time.Duration
	LatencyP50    time.Duration
	LatencyP95    time.Duration
	LatencyP99    time.Duration
	MemoryAllocMB float64
	Goroutines    int
	CPUUsage      float64
}

type BenchmarkConfig struct {
	Name          string
	Workers       int
	Duration      time.Duration
	WarmupTime    time.Duration
	RateLimit     int
	MaxOperations int64
}

func DefaultBenchmarkConfig() BenchmarkConfig {
	return BenchmarkConfig{
		Name:          "default",
		Workers:       runtime.NumCPU(),
		Duration:      10 * time.Second,
		WarmupTime:    2 * time.Second,
		RateLimit:     0,
		MaxOperations: 0,
	}
}

type LatencyTracker struct {
	latencies []time.Duration
	mu        sync.Mutex
}

func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		latencies: make([]time.Duration, 0, 10000),
	}
}

func (lt *LatencyTracker) Record(latency time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.latencies = append(lt.latencies, latency)
}

func (lt *LatencyTracker) Calculate() (avg, min, max, p50, p95, p99 time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	if len(lt.latencies) == 0 {
		return 0, 0, 0, 0, 0, 0
	}

	var sum time.Duration
	min = lt.latencies[0]
	max = lt.latencies[0]

	for _, l := range lt.latencies {
		sum += l
		if l < min {
			min = l
		}
		if l > max {
			max = l
		}
	}

	n := len(lt.latencies)
	avg = sum / time.Duration(n)

	p50 = lt.percentile(0.50)
	p95 = lt.percentile(0.95)
	p99 = lt.percentile(0.99)

	return
}

func (lt *LatencyTracker) percentile(p float64) time.Duration {
	if len(lt.latencies) == 0 {
		return 0
	}

	index := int(float64(len(lt.latencies)-1) * p)
	if index >= len(lt.latencies) {
		index = len(lt.latencies) - 1
	}

	sorted := make([]time.Duration, len(lt.latencies))
	copy(sorted, lt.latencies)

	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted[index]
}

func BenchmarkWorkerPool(b *testing.B, config BenchmarkConfig, handler worker.TaskHandler) *BenchmarkResult {
	return runBenchmark(b, config, handler)
}

func TestWorkerPool(t *testing.T, config BenchmarkConfig, handler worker.TaskHandler) *BenchmarkResult {
	return runBenchmark(t, config, handler)
}

func runBenchmark(tb testing.TB, config BenchmarkConfig, handler worker.TaskHandler) *BenchmarkResult {
	lt := NewLatencyTracker()

	wp := worker.NewPool(worker.Config{
		Workers:   config.Workers,
		QueueSize: 1000,
	})
	wp.Start(handler)

	var ops int64
	var startMem, endMem runtime.MemStats
	runtime.ReadMemStats(&startMem)

	ctx, cancel := context.WithTimeout(context.Background(), config.Duration)
	defer cancel()

	start := time.Now()

	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	for i := 0; i < config.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			taskID := 0
			for {
				select {
				case <-stopCh:
					return
				case <-ctx.Done():
					return
				default:
					taskID++
					opStart := time.Now()

					task := &worker.Task{
						ID:   fmt.Sprintf("bench-%d", taskID),
						Type: "benchmark",
					}

					if config.MaxOperations > 0 && atomic.LoadInt64(&ops) >= config.MaxOperations {
						return
					}

					err := wp.Submit(task)
					if err == nil {
						atomic.AddInt64(&ops, 1)
					}

					lt.Record(time.Since(opStart))

					if config.RateLimit > 0 {
						time.Sleep(time.Second / time.Duration(config.RateLimit))
					}
				}
			}
		}()
	}

	time.Sleep(config.Duration)
	close(stopCh)
	wg.Wait()

	duration := time.Since(start)
	runtime.ReadMemStats(&endMem)

	memAllocMB := float64(endMem.Alloc-startMem.Alloc) / 1024 / 1024
	if memAllocMB < 0 {
		memAllocMB = float64(endMem.Alloc) / 1024 / 1024
	}

	wp.Close()

	avg, min, max, p50, p95, p99 := lt.Calculate()

	result := &BenchmarkResult{
		Name:          config.Name,
		Duration:      duration,
		Operations:    ops,
		OpsPerSecond:  float64(ops) / duration.Seconds(),
		LatencyAvg:    avg,
		LatencyMin:    min,
		LatencyMax:    max,
		LatencyP50:    p50,
		LatencyP95:    p95,
		LatencyP99:    p99,
		MemoryAllocMB: memAllocMB,
		Goroutines:    runtime.NumGoroutine(),
	}

	return result
}

func (r *BenchmarkResult) String() string {
	return fmt.Sprintf(
		`Benchmark Result: %s
  Duration: %v
  Operations: %d
  Ops/second: %.2f
  Latency:
    Avg: %v
    Min: %v
    Max: %v
    P50: %v
    P95: %v
    P99: %v
  Memory: %.2f MB
  Goroutines: %d`,
		r.Name, r.Duration, r.Operations, r.OpsPerSecond,
		r.LatencyAvg, r.LatencyMin, r.LatencyMax,
		r.LatencyP50, r.LatencyP95, r.LatencyP99,
		r.MemoryAllocMB, r.Goroutines,
	)
}

type StressTest struct {
	name       string
	setup      func() (interface{}, error)
	teardown   func(interface{}) error
	exercise   func(interface{}) error
	validate   func(interface{}) error
	iterations int
	concurrent int
}

func NewStressTest(name string) *StressTest {
	return &StressTest{
		name:       name,
		iterations: 1000,
		concurrent: 10,
	}
}

func (st *StressTest) WithSetup(fn func() (interface{}, error)) *StressTest {
	st.setup = fn
	return st
}

func (st *StressTest) WithTeardown(fn func(interface{}) error) *StressTest {
	st.teardown = fn
	return st
}

func (st *StressTest) WithExercise(fn func(interface{}) error) *StressTest {
	st.exercise = fn
	return st
}

func (st *StressTest) WithValidate(fn func(interface{}) error) *StressTest {
	st.validate = fn
	return st
}

func (st *StressTest) WithIterations(n int) *StressTest {
	st.iterations = n
	return st
}

func (st *StressTest) WithConcurrent(n int) *StressTest {
	st.concurrent = n
	return st
}

func (st *StressTest) Run(t *testing.T) {
	t.Helper()

	var state interface{}
	var err error

	if st.setup != nil {
		state, err = st.setup()
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
	}

	defer func() {
		if st.teardown != nil {
			if err := st.teardown(state); err != nil {
				t.Errorf("Teardown failed: %v", err)
			}
		}
	}()

	var wg sync.WaitGroup
	errCh := make(chan error, st.iterations)

	opsPerWorker := st.iterations / st.concurrent

	for i := 0; i < st.concurrent; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < opsPerWorker; j++ {
				if st.exercise != nil {
					if err := st.exercise(state); err != nil {
						errCh <- err
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("Operation failed: %v", err)
		}
	}

	if st.validate != nil {
		if err := st.validate(state); err != nil {
			t.Errorf("Validation failed: %v", err)
		}
	}
}

type MemoryLeakTest struct {
	allocations []uint64
	samples     int
	interval    time.Duration
}

func NewMemoryLeakTest() *MemoryLeakTest {
	return &MemoryLeakTest{
		allocations: make([]uint64, 0),
		samples:     10,
		interval:    1 * time.Second,
	}
}

func (m *MemoryLeakTest) WithSamples(n int) *MemoryLeakTest {
	m.samples = n
	return m
}

func (m *MemoryLeakTest) WithInterval(d time.Duration) *MemoryLeakTest {
	m.interval = d
	return m
}

func (m *MemoryLeakTest) Record() {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	m.allocations = append(m.allocations, mem.Alloc)
}

func (m *MemoryLeakTest) Check(maxGrowthMB float64) error {
	if len(m.allocations) < 2 {
		return nil
	}

	startAlloc := float64(m.allocations[0]) / 1024 / 1024
	endAlloc := float64(m.allocations[len(m.allocations)-1]) / 1024 / 1024

	growth := endAlloc - startAlloc

	if growth > maxGrowthMB {
		return fmt.Errorf("memory leak detected: grew by %.2f MB (from %.2f MB to %.2f MB)",
			growth, startAlloc, endAlloc)
	}

	return nil
}

func DetectCPUBottleneck(d time.Duration) (time.Duration, error) {
	start := time.Now()

	done := make(chan time.Duration)
	go func() {
		data := make([]int, 1000000)
		for i := 0; i < len(data); i++ {
			data[i] = i
		}
		done <- time.Since(start)
	}()

	select {
	case cpuTime := <-done:
		return cpuTime, nil
	case <-time.After(d):
		return 0, fmt.Errorf("CPU bottleneck: took longer than %v", d)
	}
}
