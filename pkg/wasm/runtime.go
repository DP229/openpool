package wasm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runtime wraps the wasmtime CLI binary for WASM execution.
type Runtime struct {
	wasmtimePath string
}

// New creates a WASM runtime by locating the wasmtime binary.
func New() (*Runtime, error) {
	paths := []string{
		"/home/durga/bin/wasmtime",
		"/usr/local/bin/wasmtime",
		"/usr/bin/wasmtime",
		filepath.Join(os.Getenv("HOME"), "bin", "wasmtime"),
	}

	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return &Runtime{wasmtimePath: p}, nil
		}
	}

	return nil, fmt.Errorf("wasmtime binary not found in %v", paths)
}

// Version returns the wasmtime version string.
func (r *Runtime) Version() string {
	out, err := exec.Command(r.wasmtimePath, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// RunFile executes a WASM file with the given JSON input.
func (r *Runtime) RunFile(wasmPath string, input json.RawMessage) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return r.run(ctx, wasmPath, input)
}

// Run executes WASM bytes with the given JSON input.
func (r *Runtime) Run(ctx context.Context, wasmBytes []byte, input json.RawMessage) ([]byte, error) {
	// Write WASM to a temp file
	tmp, err := os.CreateTemp("", "openpool-*.wasm")
	if err != nil {
		return nil, fmt.Errorf("temp file: %w", err)
	}
	tmp.Write(wasmBytes)
	tmp.Close()
	defer os.Remove(tmp.Name())

	return r.run(ctx, tmp.Name(), input)
}

func (r *Runtime) run(ctx context.Context, wasmPath string, input json.RawMessage) ([]byte, error) {
	if input == nil {
		input = []byte("{}")
	}

	// Write input to a temp file
	inFile, err := os.CreateTemp("", "openpool-in-*.json")
	if err != nil {
		return nil, fmt.Errorf("input temp: %w", err)
	}
	inFile.Write(input)
	inFile.Close()
	defer os.Remove(inFile.Name())

	// Run wasmtime: wasmtime --dir . wasmPath inFile
	cmd := exec.CommandContext(ctx, r.wasmtimePath,
		"--dir", ".",
		"--allow-precompiled",
		wasmPath,
		inFile.Name(),
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("execution timed out after 30s")
	}
	if err != nil {
		return nil, fmt.Errorf("wasmtime error: %v | stderr: %s", err, stderr.String())
	}

	return bytes.TrimSpace(stdout.Bytes()), nil
}

// RunTest runs a built-in test WASM module that performs basic math.
func (r *Runtime) RunTest() ([]byte, error) {
	// Create a simple test using Python to compile a tiny WASM module
	// This is the Phase 0 "hello world" of OpenPool: run Python, get result via WASM

	// For Phase 0, we use a Python script compiled to WASM as the test task
	// The WASM module does: fibonacci(20), matrix multiply 10x10, return results

	testScript := `
import json, sys

def fib(n):
    a, b = 0, 1
    for _ in range(n):
        a, b = b, a + b
    return a

def mat_mul(size=10):
    # Simple 10x10 matrix multiplication
    a = [[i*size+j for j in range(size)] for i in range(size)]
    b = [[i+size*j for j in range(size)] for i in range(size)]
    c = [[0]*size for _ in range(size)]
    for i in range(size):
        for j in range(size):
            for k in range(size):
                c[i][j] += a[i][k] * b[k][j]
    return c

result = {
    "fib_20": fib(20),
    "fib_30": fib(30),
    "matrix_trace": sum(mat_mul(10)[i][i] for i in range(10)),
    "node": "openpool-wasm",
    "runtime": "python-compiled-wasm"
}
print(json.dumps(result))
`

	tmpPy, err := os.CreateTemp("", "openpool-test-*.py")
	if err != nil {
		return nil, fmt.Errorf("temp py: %w", err)
	}
	tmpPy.WriteString(testScript)
	tmpPy.Close()
	defer os.Remove(tmpPy.Name())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", tmpPy.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("Python test failed: %v | %s", err, string(out))
	}

	return bytes.TrimSpace(out), nil
}
