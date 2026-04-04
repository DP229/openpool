package integration

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dp229/openpool/pkg/connection"
	"github.com/dp229/openpool/pkg/resilience"
	"github.com/dp229/openpool/pkg/worker"
)

type IntegrationTestSuite struct {
	workers        *worker.Pool
	connections    *connection.Manager
	circuitBreaker *resilience.CircuitBreaker
}

func NewIntegrationTestSuite() *IntegrationTestSuite {
	return &IntegrationTestSuite{
		workers: worker.NewPool(worker.Config{
			Workers:   8,
			QueueSize: 100,
		}),
		connections: connection.NewManager(connection.DefaultLimits{}.Limits()),
		circuitBreaker: resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
			Name:        "integration-test",
			MaxFailures: 5,
			Timeout:     30 * time.Second,
		}),
	}
}

func TestIntegration_EndToEnd(t *testing.T) {
	suite := NewIntegrationTestSuite()

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		time.Sleep(10 * time.Millisecond)
		return []byte("processed"), nil
	}

	suite.workers.Start(handler)
	defer suite.workers.Close()

	var wg sync.WaitGroup
	var successCount int64
	var failCount int64

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			task := &worker.Task{
				ID:   fmt.Sprintf("task-%d", id),
				Type: "test",
			}

			suite.circuitBreaker.Call(func() error {
				err := suite.workers.Submit(task)
				if err != nil {
					atomic.AddInt64(&failCount, 1)
					return err
				}
				atomic.AddInt64(&successCount, 1)
				return nil
			})
		}(i)
	}

	wg.Wait()

	if successCount < 80 {
		t.Errorf("Expected at least 80 successful tasks, got %d", successCount)
	}

	if failCount > 20 {
		t.Errorf("Too many failures: %d", failCount)
	}
}

func TestIntegration_ConnectionPool(t *testing.T) {
	mgr := connection.NewManager(connection.Limits{
		MaxConnections:      100,
		MaxConnectionsPerIP: 10,
		ConnectionRate:      1000,
	})

	var wg sync.WaitGroup
	var successCount int64

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ip := fmt.Sprintf("192.168.%d.%d", id/256, id%256)
			err := mgr.CanConnect(ip)
			if err != nil {
				return
			}

			conn := &connection.Connection{
				ID:         fmt.Sprintf("conn-%d", id),
				RemoteAddr: ip,
			}

			if err := mgr.OnConnect(conn); err == nil {
				atomic.AddInt64(&successCount, 1)

				mgr.RecordActivity(conn.ID, 1024, 2048)

				time.Sleep(100 * time.Millisecond)

				mgr.OnDisconnect(conn.ID)
			}
		}(i)
	}

	wg.Wait()

	if successCount < 40 {
		t.Errorf("Expected at least 40 successful connections, got %d", successCount)
	}
}

func TestIntegration_WorkerPool_ConnectionPool(t *testing.T) {
	wp := worker.NewPool(worker.Config{
		Workers:   4,
		QueueSize: 50,
	})

	connMgr := connection.NewManager(connection.Limits{
		MaxConnections:      50,
		MaxConnectionsPerIP: 10,
		ConnectionRate:      1000,
	})

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		connID := string(task.Data)

		connMgr.RecordActivity(connID, 100, 200)

		return []byte("done"), nil
	}

	wp.Start(handler)

	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			connID := fmt.Sprintf("conn-%d", id)
			conn := &connection.Connection{
				ID:         connID,
				RemoteAddr: "192.168.1.1",
			}

			connMgr.OnConnect(conn)

			task := &worker.Task{
				ID:   fmt.Sprintf("task-%d", id),
				Data: []byte(connID),
			}

			wp.Submit(task)

			time.Sleep(50 * time.Millisecond)
			connMgr.OnDisconnect(connID)
		}(i)
	}

	wp.Drain(2 * time.Second)
	wp.Close()

	wg.Wait()
}

func TestIntegration_FailureRecovery(t *testing.T) {
	cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:          "test",
		MaxFailures:   3,
		Timeout:       100 * time.Millisecond,
		HalfOpenLimit: 2,
	})

	wp := worker.NewPool(worker.Config{
		Workers:   2,
		QueueSize: 10,
	})

	failCount := int64(0)

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		if atomic.LoadInt64(&failCount) < 3 {
			atomic.AddInt64(&failCount, 1)
			return nil, fmt.Errorf("simulated failure")
		}
		return []byte("success"), nil
	}

	wp.Start(handler)
	defer wp.Close()

	for i := 0; i < 10; i++ {
		err := cb.Call(func() error {
			task := &worker.Task{ID: fmt.Sprintf("task-%d", i)}
			return wp.SubmitWithTimeout(task, 50*time.Millisecond)
		})

		if err == resilience.ErrCircuitOpen {
			time.Sleep(150 * time.Millisecond)
		}
	}

	if cb.State() != resilience.StateClosed && cb.State() != resilience.StateHalfOpen {
		t.Errorf("Expected circuit to recover, got state: %v", cb.State())
	}
}

func TestIntegration_ConcurrentConnections(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Cannot create listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	connectCount := int64(0)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			atomic.AddInt64(&connectCount, 1)
			conn.Close()
		}
	}()

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
				if err == nil {
					conn.Close()
				}
			}
		}()
	}

	wg.Wait()

	if connectCount < 50 {
		t.Errorf("Expected at least 50 connections, got %d", connectCount)
	}
}

func TestIntegration_Shutdown(t *testing.T) {
	wp := worker.NewPool(worker.Config{
		Workers:   2,
		QueueSize: 10,
	})

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		time.Sleep(100 * time.Millisecond)
		return []byte("done"), nil
	}

	wp.Start(handler)

	taskCount := int64(0)

	for i := 0; i < 10; i++ {
		task := &worker.Task{
			ID:   fmt.Sprintf("shutdown-task-%d", i),
			Type: "shutdown-test",
		}
		wp.Submit(task)
		atomic.AddInt64(&taskCount, 1)
	}

	drained := wp.Drain(1 * time.Second)

	if drained < 5 {
		t.Errorf("Expected at least 5 drained tasks, got %d", drained)
	}

	wp.Close()
}

func BenchmarkIntegration_EndToEnd(b *testing.B) {
	suite := NewIntegrationTestSuite()

	handler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		return []byte("processed"), nil
	}

	suite.workers.Start(handler)
	defer suite.workers.Close()

	b.ResetTimer()

	var wg sync.WaitGroup

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			task := &worker.Task{
				ID:   fmt.Sprintf("bench-%d", i),
				Type: "benchmark",
			}

			suite.workers.Submit(task)
		}()
	}

	wg.Wait()
}

func BenchmarkIntegration_ConnectionPool(b *testing.B) {
	mgr := connection.NewManager(connection.Limits{
		MaxConnections:      10000,
		MaxConnectionsPerIP: 1000,
		ConnectionRate:      100000,
	})

	b.ResetTimer()

	var wg sync.WaitGroup

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ip := fmt.Sprintf("10.%d.%d.%d", id/65025, (id/255)%255, id%255)
			if mgr.CanConnect(ip) == nil {
				conn := &connection.Connection{
					ID:         fmt.Sprintf("bench-%d", id),
					RemoteAddr: ip,
				}
				mgr.OnConnect(conn)
				mgr.OnDisconnect(conn.ID)
			}
		}(i)
	}

	wg.Wait()
}
