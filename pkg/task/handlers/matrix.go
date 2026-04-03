package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dp229/openpool/pkg/task"
)

// MatrixInput represents matrix operation input
type MatrixInput struct {
	A     [][]float64 `json:"a"`
	B     [][]float64 `json:"b"`
	Size  int         `json:"size"`
	Type  string      `json:"type"` // "mul", "trace", "transpose"
}

// MatrixHandler handles matrix operations
type MatrixHandler struct {
	task.BaseHandler
}

// NewMatrixHandler creates a new matrix handler
func NewMatrixHandler() *MatrixHandler {
	return &MatrixHandler{
		BaseHandler: task.BaseHandler{
			Name_:       "matrix",
			CostCredits: task.DefaultCostCPU,
		},
	}
}

// Validate checks if the matrix input is valid
func (h *MatrixHandler) Validate(input []byte) error {
	if len(input) == 0 {
		return fmt.Errorf("input is required")
	}
	
	var data MatrixInput
	if err := json.Unmarshal(input, &data); err != nil {
		return fmt.Errorf("invalid input format: %w", err)
	}
	
	if data.Size <= 0 || data.Size > 1000 {
		return fmt.Errorf("size must be between 1 and 1000")
	}
	
	return nil
}

// Execute runs the matrix operation
func (h *MatrixHandler) Execute(ctx context.Context, input []byte) (*task.Result, error) {
	start := time.Now()
	
	var data MatrixInput
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}
	
	steps := []task.Step{
		{
			ID:        1,
			Timestamp: start,
			Action:    "validate_matrix",
			Output:    "OK",
		},
	}
	
	var result interface{}
	var err error
	
	switch data.Type {
	case "mul":
		result, steps = h.matrixMultiply(data, steps, start)
	case "trace":
		result, steps = h.matrixTrace(data, steps, start)
	case "transpose":
		result, steps = h.matrixTranspose(data, steps, start)
	default:
		result, steps = h.matrixMultiply(data, steps, start)
	}
	
	if err != nil {
		return nil, err
	}
	
	elapsed := time.Since(start)
	
	res := &task.Result{
		Output: must(json.Marshal(result)),
		Success: true,
		Metrics: task.Metrics{
			LatencyMs:   int(elapsed.Milliseconds()),
			CostCredits: h.CostCredits,
			Steps:       steps,
		},
	}
	
	return res, nil
}

func (h *MatrixHandler) matrixMultiply(data MatrixInput, steps []task.Step, start time.Time) (interface{}, []task.Step) {
	size := data.Size
	
	// Generate matrices if not provided
	A := data.A
	B := data.B
	
	if A == nil {
		A = generateMatrix(size)
	}
	if B == nil {
		B = generateMatrix(size)
	}
	
	// Multiply
	C := make([][]float64, size)
	for i := 0; i < size; i++ {
		C[i] = make([]float64, size)
		for j := 0; j < size; j++ {
			for k := 0; k < size; k++ {
				C[i][j] += A[i][k] * B[k][j]
			}
		}
	}
	
	elapsed := time.Since(start)
	steps = append(steps, task.Step{
		ID:        2,
		Timestamp: start.Add(elapsed),
		Action:    "matrix_multiply",
		Input:     fmt.Sprintf("size=%d", size),
		Output:    fmt.Sprintf("computed %dx%d matrix", size, size),
		Duration:  int(elapsed.Milliseconds()),
	})
	
	return map[string]interface{}{
		"operation": "matrix_multiply",
		"size":     size,
		"result":   C,
	}, steps
}

func (h *MatrixHandler) matrixTrace(data MatrixInput, steps []task.Step, start time.Time) (interface{}, []task.Step) {
	size := data.Size
	A := data.A
	
	if A == nil {
		A = generateMatrix(size)
	}
	
	// Calculate trace
	trace := 0.0
	for i := 0; i < size; i++ {
		trace += A[i][i]
	}
	
	elapsed := time.Since(start)
	steps = append(steps, task.Step{
		ID:        2,
		Timestamp: start.Add(elapsed),
		Action:    "matrix_trace",
		Input:     fmt.Sprintf("size=%d", size),
		Output:    fmt.Sprintf("trace=%.2f", trace),
		Duration:  int(elapsed.Milliseconds()),
	})
	
	return map[string]interface{}{
		"operation": "matrix_trace",
		"size":     size,
		"trace":    trace,
	}, steps
}

func (h *MatrixHandler) matrixTranspose(data MatrixInput, steps []task.Step, start time.Time) (interface{}, []task.Step) {
	size := data.Size
	A := data.A
	
	if A == nil {
		A = generateMatrix(size)
	}
	
	// Transpose
	T := make([][]float64, size)
	for i := 0; i < size; i++ {
		T[i] = make([]float64, size)
		for j := 0; j < size; j++ {
			T[i][j] = A[j][i]
		}
	}
	
	elapsed := time.Since(start)
	steps = append(steps, task.Step{
		ID:        2,
		Timestamp: start.Add(elapsed),
		Action:    "matrix_transpose",
		Input:     fmt.Sprintf("size=%d", size),
		Output:    fmt.Sprintf("transposed %dx%d matrix", size, size),
		Duration:  int(elapsed.Milliseconds()),
	})
	
	return map[string]interface{}{
		"operation": "matrix_transpose",
		"size":     size,
		"result":   T,
	}, steps
}

func generateMatrix(size int) [][]float64 {
	m := make([][]float64, size)
	for i := 0; i < size; i++ {
		m[i] = make([]float64, size)
		for j := 0; j < size; j++ {
			m[i][j] = float64(i*size + j) + 1.0
		}
	}
	return m
}
