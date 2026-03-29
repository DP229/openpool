package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dp229/openpool/pkg/ledger"
	"github.com/dp229/openpool/pkg/wasm"
)

// Executor runs WASM tasks and returns results.
type Executor struct {
	runtime *wasm.Runtime
	ledger  *ledger.Ledger
}

// New creates a new task executor.
func New(runtime *wasm.Runtime, ledger *ledger.Ledger) *Executor {
	return &Executor{runtime: runtime, ledger: ledger}
}

// Execute runs a task and returns the result.
func (e *Executor) Execute(ctx context.Context, task *Task) (json.RawMessage, error) {
	if e.runtime == nil {
		return nil, fmt.Errorf("WASM runtime not available")
	}

	if task.TimeoutSec <= 0 {
		task.TimeoutSec = 30
	}

	timeout := time.Duration(task.TimeoutSec) * time.Second
	if timeout > 4*time.Hour {
		timeout = 4 * time.Hour
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Use WASM path from task, pass raw JSON input
	result, err := e.runtime.RunTask(ctx, task.WASMPath, task.RawInput)
	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	// Validate JSON output
	if !json.Valid(result) {
		return nil, fmt.Errorf("WASM returned invalid JSON")
	}

	return result, nil
}

type Task struct {
	ID         string          `json:"-"`
	WASMPath   string          `json:"-"`
	RawInput   json.RawMessage `json:"-"`
	TimeoutSec int             `json:"timeout_sec"`
	Credits    int             `json:"credits"`
}
