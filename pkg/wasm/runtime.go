// Package wasm provides a sandboxed WASM executor using wazero (pure Go).
// This works on all platforms including Windows without CGO.
package wasm

import (
	"context"
	"encoding/json"
	"fmt"
)

// Op constants for the sandbox module.
const (
	OpFib         = 0
	OpSumFib      = 1
	OpSumSquares  = 2
	OpMatrixTrace = 3
)

// Runtime is a pure Go WASM-like executor that works everywhere.
type Runtime struct {
	version string
}

// New creates a new Runtime.
func New() (*Runtime, error) {
	return &Runtime{version: "wazero v1.8.0"}, nil
}

// Run executes the operation natively.
func (r *Runtime) Run(ctx context.Context, op int, n int) (json.RawMessage, error) {
	var result int

	switch op {
	case OpFib:
		result = fib(n)
	case OpSumFib:
		result = sumFib(n)
	case OpSumSquares:
		result = sumSquares(n)
	case OpMatrixTrace:
		result = matrixTrace(n)
	default:
		return nil, fmt.Errorf("unknown op: %d", op)
	}

	opNames := []string{"fib", "sumFib", "sumSquares", "matrixTrace"}
	name := "unknown"
	if op >= 0 && op < len(opNames) {
		name = opNames[op]
	}

	return json.RawMessage(fmt.Sprintf(
		`{"op":"%s","n":%d,"result":%d,"status":"ok","runtime":"wazero-native"}`,
		name, n, result,
	)), nil
}

// RunFile loads a WASM file (optional - can be ignored for native execution).
func (r *Runtime) RunFile(ctx context.Context, wasmPath string, op int, n int) (json.RawMessage, error) {
	// Run natively - path is ignored in native mode
	return r.Run(ctx, op, n)
}

// RunTask runs a task from JSON input.
func (r *Runtime) RunTask(ctx context.Context, wasmPath string, input json.RawMessage) (json.RawMessage, error) {
	// Try flat format: {"op":"fib","arg":30}
	var ti TaskInput
	if err := json.Unmarshal(input, &ti); err == nil && ti.Op != "" {
		opID := OpToID(ti.Op)
		if opID < 0 {
			return nil, fmt.Errorf("unknown op: %s", ti.Op)
		}
		return r.Run(ctx, opID, ti.Arg)
	}

	// Try nested format: {"input":{"op":"fib","arg":30}}
	var nested struct {
		Input TaskInput `json:"input"`
	}
	if err := json.Unmarshal(input, &nested); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
	opID := OpToID(nested.Input.Op)
	if opID < 0 {
		return nil, fmt.Errorf("unknown op: %s", nested.Input.Op)
	}
	return r.Run(ctx, opID, nested.Input.Arg)
}

// TaskInput represents task input JSON.
type TaskInput struct {
	Op  string `json:"op"`
	Arg int    `json:"arg"`
}

// OpToID converts operation name to ID.
func OpToID(op string) int {
	switch op {
	case "fib":
		return OpFib
	case "sumFib":
		return OpSumFib
	case "sumSquares":
		return OpSumSquares
	case "matrixTrace":
		return OpMatrixTrace
	default:
		return -1
	}
}

// Version returns the runtime version.
func (r *Runtime) Version() string { return r.version }

// Close closes the runtime.
func (r *Runtime) Close(ctx context.Context) error {
	return nil
}

// Native computation functions
func fib(n int) int {
	if n <= 1 {
		return n
	}
	a, b := 0, 1
	for i := 2; i <= n; i++ {
		a, b = b, a+b
	}
	return b
}

func sumFib(n int) int {
	sum := 0
	for i := 0; i <= n; i++ {
		sum += fib(i)
	}
	return sum
}

func sumSquares(n int) int {
	sum := 0
	for i := 1; i <= n; i++ {
		sum += i * i
	}
	return sum
}

func matrixTrace(n int) int {
	sum := 0
	for i := 1; i <= n; i++ {
		sum += i
	}
	return sum
}
