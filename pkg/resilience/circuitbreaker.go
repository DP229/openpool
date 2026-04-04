package resilience

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrCircuitOpen     = errors.New("circuit breaker is open")
	ErrTimeout         = errors.New("operation timed out")
	ErrCircuitHalfOpen = errors.New("circuit breaker is half-open")
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

type CircuitBreaker struct {
	name          string
	maxFailures   int
	timeout       time.Duration
	halfOpenLimit int

	state        State
	failures     int
	lastFailTime time.Time
	successCount int

	mu sync.RWMutex
}

type CircuitBreakerConfig struct {
	Name          string
	MaxFailures   int
	Timeout       time.Duration
	HalfOpenLimit int
}

func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.MaxFailures <= 0 {
		config.MaxFailures = 5
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.HalfOpenLimit <= 0 {
		config.HalfOpenLimit = 1
	}

	return &CircuitBreaker{
		name:          config.Name,
		maxFailures:   config.MaxFailures,
		timeout:       config.Timeout,
		halfOpenLimit: config.HalfOpenLimit,
		state:         StateClosed,
	}
}

func (cb *CircuitBreaker) Call(fn func() error) error {
	return cb.CallWithTimeout(fn, cb.timeout)
}

func (cb *CircuitBreaker) CallWithTimeout(fn func() error, timeout time.Duration) error {
	if !cb.canExecute() {
		return ErrCircuitOpen
	}

	done := make(chan error, 1)

	go func() {
		defer close(done)
		done <- fn()
	}()

	select {
	case err := <-done:
		cb.recordResult(err == nil)
		return err
	case <-time.After(timeout):
		cb.recordResult(false)
		return ErrTimeout
	}
}

func (cb *CircuitBreaker) canExecute() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true

	case StateOpen:
		if time.Since(cb.lastFailTime) > cb.timeout {
			cb.state = StateHalfOpen
			cb.successCount = 0
			return true
		}
		return false

	case StateHalfOpen:
		return true

	default:
		return false
	}
}

func (cb *CircuitBreaker) recordResult(success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if success {
		cb.onSuccess()
	} else {
		cb.onFailure()
	}
}

func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case StateClosed:
		cb.failures = 0

	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.halfOpenLimit {
			cb.state = StateClosed
			cb.failures = 0
			cb.successCount = 0
		}

	default:
	}
}

func (cb *CircuitBreaker) onFailure() {
	switch cb.state {
	case StateClosed:
		cb.failures++
		cb.lastFailTime = time.Now()
		if cb.failures >= cb.maxFailures {
			cb.state = StateOpen
		}

	case StateHalfOpen:
		cb.state = StateOpen
		cb.lastFailTime = time.Now()
		cb.successCount = 0

	default:
	}
}

func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) GetState() State {
	return cb.State()
}

func (cb *CircuitBreaker) ExecuteWithResult(fn func() (interface{}, error)) (interface{}, error) {
	if !cb.canExecute() {
		return nil, ErrCircuitOpen
	}

	result, err := fn()
	cb.recordResult(err == nil)
	return result, err
}

func (cb *CircuitBreaker) Failures() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.failures
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.failures = 0
	cb.successCount = 0
}

func (cb *CircuitBreaker) Trip() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateOpen
	cb.lastFailTime = time.Now()
}

type CircuitBreakerGroup struct {
	breakers      map[string]*CircuitBreaker
	mu            sync.RWMutex
	defaultConfig CircuitBreakerConfig
}

func NewCircuitBreakerGroup(defaultConfig CircuitBreakerConfig) *CircuitBreakerGroup {
	return &CircuitBreakerGroup{
		breakers:      make(map[string]*CircuitBreaker),
		defaultConfig: defaultConfig,
	}
}

func (g *CircuitBreakerGroup) Get(name string) *CircuitBreaker {
	g.mu.RLock()
	breaker, exists := g.breakers[name]
	g.mu.RUnlock()

	if exists {
		return breaker
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if breaker, exists = g.breakers[name]; exists {
		return breaker
	}

	config := g.defaultConfig
	config.Name = name
	breaker = NewCircuitBreaker(config)
	g.breakers[name] = breaker

	return breaker
}

func (g *CircuitBreakerGroup) Call(name string, fn func() error) error {
	return g.Get(name).Call(fn)
}

func (g *CircuitBreakerGroup) States() map[string]State {
	g.mu.RLock()
	defer g.mu.RUnlock()

	states := make(map[string]State, len(g.breakers))
	for name, breaker := range g.breakers {
		states[name] = breaker.State()
	}

	return states
}

func (g *CircuitBreakerGroup) ResetAll() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, breaker := range g.breakers {
		breaker.Reset()
	}
}

type RetryConfig struct {
	MaxAttempts   int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	Multiplier    float64
	RetryableFunc func(error) bool
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      5 * time.Second,
		Multiplier:    2.0,
		RetryableFunc: func(err error) bool { return true },
	}
}

func Retry(config RetryConfig, fn func() error) error {
	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		if config.RetryableFunc != nil && !config.RetryableFunc(err) {
			return err
		}

		if attempt < config.MaxAttempts-1 {
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * config.Multiplier)
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
		}
	}

	return lastErr
}

type Bulkhead struct {
	name          string
	maxConcurrent int
	semaphore     chan struct{}
	mu            sync.RWMutex
}

func NewBulkhead(name string, maxConcurrent int) *Bulkhead {
	return &Bulkhead{
		name:          name,
		maxConcurrent: maxConcurrent,
		semaphore:     make(chan struct{}, maxConcurrent),
	}
}

func (b *Bulkhead) Acquire() bool {
	select {
	case b.semaphore <- struct{}{}:
		return true
	default:
		return false
	}
}

func (b *Bulkhead) Release() {
	select {
	case <-b.semaphore:
	default:
	}
}

func (b *Bulkhead) Execute(fn func() error) error {
	if !b.Acquire() {
		return errors.New("bulkhead: too many concurrent executions")
	}
	defer b.Release()

	return fn()
}

func (b *Bulkhead) Available() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.maxConcurrent - len(b.semaphore)
}

func (b *Bulkhead) MaxConcurrent() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.maxConcurrent
}
