package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/dp229/openpool/pkg/task"
)

// FibHandler handles Fibonacci computation tasks
type FibHandler struct {
	task.BaseHandler
}

// NewFibHandler creates a new Fibonacci handler
func NewFibHandler() *FibHandler {
	return &FibHandler{
		BaseHandler: task.BaseHandler{
			Name_:       "fib",
			CostCredits: task.DefaultCostCPU,
		},
	}
}

// Validate checks if the input is a valid number
func (h *FibHandler) Validate(input []byte) error {
	if len(input) == 0 {
		return fmt.Errorf("input is required")
	}

	var n int
	if err := json.Unmarshal(input, &n); err != nil {
		return fmt.Errorf("input must be a number: %w", err)
	}

	if n < 0 {
		return fmt.Errorf("n must be non-negative")
	}

	if n > 10000 {
		return fmt.Errorf("n is too large (max 10000)")
	}

	return nil
}

// Execute runs the Fibonacci computation
func (h *FibHandler) Execute(ctx context.Context, input []byte) (*task.Result, error) {
	start := time.Now()

	var n int
	if err := json.Unmarshal(input, &n); err != nil {
		return nil, err
	}

	// Calculate Fibonacci
	result := fibonacci(n)

	elapsed := time.Since(start)

	res := &task.Result{
		Output: must(json.Marshal(map[string]interface{}{
			"n":   n,
			"fib": result.String(),
		})),
		Success: true,
		Metrics: task.Metrics{
			LatencyMs:   int(elapsed.Milliseconds()),
			CostCredits: h.CostCredits,
			Steps: []task.Step{
				{
					ID:        1,
					Timestamp: start,
					Action:    "fibonacci",
					Input:     fmt.Sprintf("n=%d", n),
					Output:    result.String(),
					Duration:  int(elapsed.Milliseconds()),
				},
			},
		},
	}

	return res, nil
}

// fibonacci calculates the nth Fibonacci number
func fibonacci(n int) *big.Int {
	if n == 0 {
		return big.NewInt(0)
	}
	if n == 1 {
		return big.NewInt(1)
	}

	a := big.NewInt(0)
	b := big.NewInt(1)

	for i := 2; i <= n; i++ {
		a.Add(a, b)
		a, b = b, a
	}

	return b
}

func must(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}
