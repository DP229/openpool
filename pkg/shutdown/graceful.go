package shutdown

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type GracefulShutdown struct {
	handlers       []ShutdownHandler
	mu             sync.RWMutex
	timeout        time.Duration
	signals        []os.Signal
	onShuttingDown func()
}

type ShutdownPriority int

const (
	PriorityCritical ShutdownPriority = iota
	PriorityHigh
	PriorityMedium
	PriorityLow
)

type ShutdownHandler struct {
	Name     string
	Priority ShutdownPriority
	Fn       func(context.Context) error
}

type ShutdownOption func(*GracefulShutdown)

func WithTimeout(timeout time.Duration) ShutdownOption {
	return func(gs *GracefulShutdown) {
		gs.timeout = timeout
	}
}

func WithSignals(signals ...os.Signal) ShutdownOption {
	return func(gs *GracefulShutdown) {
		gs.signals = signals
	}
}

func WithOnShuttingDown(fn func()) ShutdownOption {
	return func(gs *GracefulShutdown) {
		gs.onShuttingDown = fn
	}
}

func New(opts ...ShutdownOption) *GracefulShutdown {
	gs := &GracefulShutdown{
		handlers: make([]ShutdownHandler, 0),
		timeout:  30 * time.Second,
		signals:  []os.Signal{syscall.SIGINT, syscall.SIGTERM},
	}

	for _, opt := range opts {
		opt(gs)
	}

	return gs
}

func (gs *GracefulShutdown) Register(name string, handler func(context.Context) error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.handlers = append(gs.handlers, ShutdownHandler{
		Name: name,
		Fn:   handler,
	})
}

func (gs *GracefulShutdown) Wait() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, gs.signals...)

	sig := <-quit

	if gs.onShuttingDown != nil {
		gs.onShuttingDown()
	}

	log.Printf("Received signal: %v", sig)
	log.Println("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), gs.timeout)
	defer cancel()

	var wg sync.WaitGroup

	gs.mu.RLock()
	handlers := make([]ShutdownHandler, len(gs.handlers))
	copy(handlers, gs.handlers)
	gs.mu.RUnlock()

	for i := len(handlers) - 1; i >= 0; i-- {
		handler := handlers[i]
		wg.Add(1)

		go func(h ShutdownHandler) {
			defer wg.Done()

			log.Printf("Shutting down: %s", h.Name)

			done := make(chan error, 1)
			go func() {
				done <- h.Fn(ctx)
			}()

			select {
			case err := <-done:
				if err != nil {
					log.Printf("Handler %s error: %v", h.Name, err)
				} else {
					log.Printf("Handler %s completed", h.Name)
				}
			case <-ctx.Done():
				log.Printf("Handler %s timeout", h.Name)
			}
		}(handler)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("Shutdown complete")
	case <-ctx.Done():
		log.Println("Shutdown timeout exceeded, forcing exit")
		os.Exit(1)
	}
}

func (gs *GracefulShutdown) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), gs.timeout)
	defer cancel()

	var wg sync.WaitGroup
	var firstError error
	var errorMu sync.Mutex

	gs.mu.RLock()
	handlers := make([]ShutdownHandler, len(gs.handlers))
	copy(handlers, gs.handlers)
	gs.mu.RUnlock()

	for i := len(handlers) - 1; i >= 0; i-- {
		handler := handlers[i]
		wg.Add(1)

		go func(h ShutdownHandler) {
			defer wg.Done()

			if err := h.Fn(ctx); err != nil {
				errorMu.Lock()
				if firstError == nil {
					firstError = err
				}
				errorMu.Unlock()
				log.Printf("Handler %s error: %v", h.Name, err)
			}
		}(handler)
	}

	wg.Wait()
	return firstError
}

type Drainable interface {
	Drain(timeout time.Duration) int
}

func DrainAndWait(drainables []Drainable, timeout time.Duration) int {
	totalDrained := 0

	var wg sync.WaitGroup
	drainCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	results := make(chan int, len(drainables))

	for _, d := range drainables {
		wg.Add(1)
		go func(drain Drainable) {
			defer wg.Done()

			done := make(chan int, 1)
			go func() {
				done <- drain.Drain(timeout)
			}()

			select {
			case count := <-done:
				results <- count
			case <-drainCtx.Done():
				results <- 0
			}
		}(d)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for count := range results {
		totalDrained += count
	}

	return totalDrained
}

type Stoppable interface {
	Stop() error
}

func StopAll(stoppables []Stoppable) error {
	var lastError error

	for i := len(stoppables) - 1; i >= 0; i-- {
		if err := stoppables[i].Stop(); err != nil {
			lastError = err
		}
	}

	return lastError
}

type Closable interface {
	Close() error
}

func CloseAll(closables []Closable) error {
	var lastError error

	for i := len(closables) - 1; i >= 0; i-- {
		if err := closables[i].Close(); err != nil {
			lastError = err
		}
	}

	return lastError
}

type Timeout struct {
	duration  time.Duration
	onTimeout func()
}

func NewTimeout(duration time.Duration, onTimeout func()) *Timeout {
	return &Timeout{
		duration:  duration,
		onTimeout: onTimeout,
	}
}

func (t *Timeout) Run(fn func() error) error {
	done := make(chan error, 1)

	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(t.duration):
		if t.onTimeout != nil {
			t.onTimeout()
		}
		return context.DeadlineExceeded
	}
}
