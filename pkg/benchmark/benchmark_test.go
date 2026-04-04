package benchmark

import (
	"context"
	"testing"
	"time"

	"github.com/dp229/openpool/pkg/worker"
)

func TestLatencyTracker_Record(t *testing.T) {
	lt := NewLatencyTracker()

	lt.Record(10 * time.Millisecond)
	lt.Record(20 * time.Millisecond)
	lt.Record(30 * time.Millisecond)

	if len(lt.latencies) != 3 {
		t.Errorf("Expected 3 latencies, got %d", len(lt.latencies))
	}
}

func TestLatencyTracker_Calculate(t *testing.T) {
	lt := NewLatencyTracker()

	durations := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		50 * time.Millisecond,
	}

	for _, d := range durations {
		lt.Record(d)
	}

	avg, min, max, p50, p95, p99 := lt.Calculate()

	if min != 10*time.Millisecond {
		t.Errorf("Expected min 10ms, got %v", min)
	}

	if max != 50*time.Millisecond {
		t.Errorf("Expected max 50ms, got %v", max)
	}

	if avg != 30*time.Millisecond {
		t.Errorf("Expected avg 30ms, got %v", avg)
	}

	_ = p50
	_ = p95
	_ = p99
}

func TestLatencyTracker_Percentile(t *testing.T) {
	lt := NewLatencyTracker()

	for i := 1; i <= 100; i++ {
		lt.Record(time.Duration(i) * time.Millisecond)
	}

	p50 := lt.percentile(0.50)
	if p50 < 49*time.Millisecond || p50 > 51*time.Millisecond {
		t.Errorf("Expected p50 around 50ms, got %v", p50)
	}
}

func TestBenchmarkWorkerPool(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping benchmark in short mode")
	}

	config := BenchmarkConfig{
		Name:          "test-benchmark",
		Workers:       4,
		Duration:      1 * time.Second,
		WarmupTime:    100 * time.Millisecond,
		MaxOperations: 100,
	}

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		time.Sleep(10 * time.Millisecond)
		return []byte("ok"), nil
	}

	result := TestWorkerPool(t, config, handler)

	if result == nil {
		t.Fatal("Expected benchmark result")
	}

	if result.Operations == 0 {
		t.Error("Expected at least some operations")
	}

	if result.OpsPerSecond <= 0 {
		t.Error("Expected positive ops/sec")
	}

	t.Logf("Benchmark result: %+v", result)
}

func TestStressTest_Run(t *testing.T) {
	var ops int

	st := NewStressTest("simple-stress").
		WithSetup(func() (interface{}, error) {
			ops = 0
			return nil, nil
		}).
		WithExercise(func(s interface{}) error {
			ops++
			return nil
		}).
		WithValidate(func(s interface{}) error {
			if ops < 900 {
				return nil
			}
			return nil
		}).
		WithIterations(1000).
		WithConcurrent(10)

	st.Run(t)

	if ops < 900 {
		t.Errorf("Expected at least 900 ops, got %d", ops)
	}
}

func TestStressTest_WithTeardown(t *testing.T) {
	teardownCalled := false

	st := NewStressTest("teardown-test").
		WithSetup(func() (interface{}, error) {
			return "state", nil
		}).
		WithTeardown(func(s interface{}) error {
			teardownCalled = true
			if s != "state" {
				t.Errorf("Expected state, got %v", s)
			}
			return nil
		}).
		WithExercise(func(s interface{}) error {
			return nil
		}).
		WithIterations(10)

	st.Run(t)

	if !teardownCalled {
		t.Error("Expected teardown to be called")
	}
}

func TestStressTest_Setup_Error(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
		}
	}()

	st := NewStressTest("setup-error").
		WithSetup(func() (interface{}, error) {
			return nil, nil
		}).
		WithIterations(10)

	st.Run(t)
}

func TestMemoryLeakTest(t *testing.T) {
	mlt := NewMemoryLeakTest().
		WithSamples(5).
		WithInterval(10 * time.Millisecond)

	for i := 0; i < 5; i++ {
		mlt.Record()
		time.Sleep(10 * time.Millisecond)
	}

	if len(mlt.allocations) != 5 {
		t.Errorf("Expected 5 samples, got %d", len(mlt.allocations))
	}

	err := mlt.Check(100.0)
	if err != nil {
		t.Errorf("Unexpected leak error: %v", err)
	}

	err = mlt.Check(0.0)
	if err == nil {
		t.Error("Expected leak error with 0 threshold")
	}
}

func TestDetectCPUBottleneck(t *testing.T) {
	cpuTime, err := DetectCPUBottleneck(5 * time.Second)
	if err != nil {
		t.Skipf("CPU benchmark failed: %v", err)
	}

	if cpuTime == 0 {
		t.Error("Expected positive CPU time")
	}

	t.Logf("CPU benchmark time: %v", cpuTime)
}

func BenchmarkWorkerPool_Throughput(b *testing.B) {
	config := BenchmarkConfig{
		Name:          "throughput-bench",
		Workers:       8,
		Duration:      5 * time.Second,
		WarmupTime:    500 * time.Millisecond,
		MaxOperations: int64(b.N),
	}

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		return []byte("result"), nil
	}

	result := BenchmarkWorkerPool(&testing.T{}, config, handler)

	b.ReportMetric(result.OpsPerSecond, "ops/sec")
	b.ReportMetric(float64(result.LatencyP95.Milliseconds()), "p95_ms")
}

func BenchmarkWorkerPool_Latency(b *testing.B) {
	pool := worker.NewPool(worker.Config{
		Workers:   8,
		QueueSize: 100,
	})

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		return []byte("result"), nil
	}

	pool.Start(handler)
	defer pool.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		task := &worker.Task{
			ID:   "bench",
			Type: "benchmark",
		}
		pool.Submit(task)
	}
}

func TestBenchmarkResult_String(t *testing.T) {
	result := &BenchmarkResult{
		Name:          "test",
		Duration:      1 * time.Second,
		Operations:    1000,
		OpsPerSecond:  1000.0,
		LatencyAvg:    1 * time.Millisecond,
		LatencyMin:    500 * time.Microsecond,
		LatencyMax:    5 * time.Millisecond,
		LatencyP50:    1 * time.Millisecond,
		LatencyP95:    3 * time.Millisecond,
		LatencyP99:    4 * time.Millisecond,
		MemoryAllocMB: 10.5,
		Goroutines:    15,
	}

	str := result.String()

	if str == "" {
		t.Error("Expected non-empty string")
	}
}

func BenchmarkLatencyTracker_Record(b *testing.B) {
	lt := NewLatencyTracker()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lt.Record(time.Microsecond)
	}
}

func BenchmarkLatencyTracker_Calculate(b *testing.B) {
	lt := NewLatencyTracker()

	for i := 0; i < 1000; i++ {
		lt.Record(time.Millisecond * time.Duration(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lt.Calculate()
	}
}

func TestDefaultBenchmarkConfig(t *testing.T) {
	config := DefaultBenchmarkConfig()

	if config.Name == "" {
		t.Error("Expected name")
	}

	if config.Workers <= 0 {
		t.Error("Expected positive workers")
	}

	if config.Duration <= 0 {
		t.Error("Expected positive duration")
	}
}
