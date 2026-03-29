// Package wasm provides a sandboxed WASM executor using the wasmtime Go API.
package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/bytecodealliance/wasmtime-go/v9"
)

// Op constants for the sandbox WASM module.
const (
	OpFib        = 0
	OpSumFib     = 1
	OpSumSquares = 2
	OpMatrixTrace = 3
)

// Runtime wraps a wasmtime engine for sandboxed WASM execution.
type Runtime struct {
	engine    *wasmtime.Engine
	module    *wasmtime.Module
}

// New creates a new WASM runtime.
func New() (*Runtime, error) {
	engine := wasmtime.NewEngine()
	return &Runtime{engine: engine}, nil
}

// LoadModule reads and compiles a WASM binary from disk.
func (r *Runtime) LoadModule(path string) error {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read wasm file: %w", err)
	}
	module, err := wasmtime.NewModule(r.engine, bytes)
	if err != nil {
		return fmt.Errorf("compile wasm: %w", err)
	}
	r.module = module
	return nil
}

// Run executes the loaded WASM module.
// op: 0=fib, 1=sumFib, 2=sumSquares, 3=matrixTrace
// n: input number
// Returns JSON: {"op":"fib","n":30,"result":832040}
func (r *Runtime) Run(ctx context.Context, op int, n int) (json.RawMessage, error) {
	if r.module == nil {
		return nil, fmt.Errorf("no module loaded")
	}

	store := wasmtime.NewStore(r.engine)
	linker := wasmtime.NewLinker(r.engine)

	// Link WASI
	if err := linker.DefineWasi(); err != nil {
		return nil, fmt.Errorf("define wasi: %w", err)
	}
	wasiConfig := wasmtime.NewWasiConfig()
	wasiConfig.InheritStdout()
	wasiConfig.InheritStderr()
	store.SetWasi(wasiConfig)

	instance, err := linker.Instantiate(store, r.module)
	if err != nil {
		return nil, fmt.Errorf("instantiate: %w", err)
	}

	runFunc := instance.GetFunc(store, "run")
	if runFunc == nil {
		return nil, fmt.Errorf("module missing 'run' export")
	}

	// Execute with timeout
	type result struct {
		ret int64
		err error
	}
	resCh := make(chan result, 1)

	go func() {
		ret, err := runFunc.Call(store, int32(op), int32(n))
		if err != nil {
			resCh <- result{err: fmt.Errorf("wasm: %w", err)}
			return
		}
		resCh <- result{ret: ret.(int64)}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout")
	case r := <-resCh:
		if r.err != nil {
			return nil, r.err
		}
		opNames := []string{"fib", "sumFib", "sumSquares", "matrixTrace"}
		name := "unknown"
		if op >= 0 && op < len(opNames) {
			name = opNames[op]
		}
		return json.RawMessage(fmt.Sprintf(
			`{"op":"%s","n":%d,"result":%d,"status":"ok","runtime":"wasmtime"}`,
			name, n, r.ret,
		)), nil
	}
}

// TaskInput represents task input JSON.
type TaskInput struct {
	Op   string `json:"op"`
	Arg  int    `json:"arg"`
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

// RunFile loads a module and runs it.
func (r *Runtime) RunFile(ctx context.Context, wasmPath string, op int, n int) (json.RawMessage, error) {
	if err := r.LoadModule(wasmPath); err != nil {
		return nil, err
	}
	return r.Run(ctx, op, n)
}

// RunTask runs a task from JSON input.
// Handles two formats:
// 1. Flat: {"op":"fib","arg":30}
// 2. Nested: {"input":{"op":"fib","arg":30}}
func (r *Runtime) RunTask(ctx context.Context, wasmPath string, input json.RawMessage) (json.RawMessage, error) {
	// Try flat format first
	var ti TaskInput
	if err := json.Unmarshal(input, &ti); err == nil && ti.Op != "" {
		opID := OpToID(ti.Op)
		if opID < 0 {
			return nil, fmt.Errorf("unknown op: %s", ti.Op)
		}
		return r.RunFile(ctx, wasmPath, opID, ti.Arg)
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
	return r.RunFile(ctx, wasmPath, opID, nested.Input.Arg)
}

// Version returns the wasmtime-go version.
func (r *Runtime) Version() string { return "wasmtime-go v9" }
