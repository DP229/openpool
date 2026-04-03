package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dp229/openpool/pkg/task"
)

// SumFibHandler handles sum of Fibonacci computation
type SumFibHandler struct {
	task.BaseHandler
}

// NewSumFibHandler creates a new sum of Fibonacci handler
func NewSumFibHandler() *SumFibHandler {
	return &SumFibHandler{
		BaseHandler: task.BaseHandler{
			Name_:       "sumFib",
			CostCredits: task.DefaultCostCPU,
		},
	}
}

// Validate checks if the input is valid
func (h *SumFibHandler) Validate(input []byte) error {
	if len(input) == 0 {
		return fmt.Errorf("input is required")
	}
	
	var n int
	if err := json.Unmarshal(input, &n); err != nil {
		return fmt.Errorf("input must be a number: %w", err)
	}
	
	if n < 1 {
		return fmt.Errorf("n must be at least 1")
	}
	
	if n > 10000 {
		return fmt.Errorf("n is too large (max 10000)")
	}
	
	return nil
}

// Execute runs the sum of Fibonacci computation
func (h *SumFibHandler) Execute(ctx context.Context, input []byte) (*task.Result, error) {
	start := time.Now()
	
	var n int
	if err := json.Unmarshal(input, &n); err != nil {
		return nil, err
	}
	
	// Calculate sum of first n Fibonacci numbers
	sum := sumFibonacci(n)
	
	elapsed := time.Since(start)
	
	res := &task.Result{
		Output: must(json.Marshal(map[string]interface{}{
			"n":    n,
			"sum":  sum,
			"note": "Sum of first n Fibonacci numbers",
		})),
		Success: true,
		Metrics: task.Metrics{
			LatencyMs:   int(elapsed.Milliseconds()),
			CostCredits: h.CostCredits,
			Steps: []task.Step{
				{
					ID:        1,
					Timestamp: start,
					Action:    "sum_fibonacci",
					Input:     fmt.Sprintf("n=%d", n),
					Output:    fmt.Sprintf("%d", sum),
					Duration:  int(elapsed.Milliseconds()),
				},
			},
		},
	}
	
	return res, nil
}

// sumFibonacci calculates sum of first n Fibonacci numbers
func sumFibonacci(n int) int64 {
	if n <= 0 {
		return 0
	}
	
	// Sum of first n Fibonacci numbers = F(n+2) - 1
	a, b := int64(0), int64(1)
	for i := 2; i <= n+2; i++ {
		a, b = b, a+b
	}
	
	return b - 1
}
