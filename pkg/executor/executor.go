package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dp229/openpool/pkg/ledger"
	"github.com/dp229/openpool/pkg/verification"
	"github.com/dp229/openpool/pkg/wasm"
)

// Executor runs WASM tasks and returns results.
type Executor struct {
	runtime     *wasm.Runtime
	ledger      *ledger.Ledger
	verifier    *verification.Verifier
}

// New creates a new task executor.
func New(runtime *wasm.Runtime, ledger *ledger.Ledger, verifier *verification.Verifier) *Executor {
	return &Executor{runtime: runtime, ledger: ledger, verifier: verifier}
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

	startTime := time.Now()

	// Use WASM path from task, pass raw JSON input
	result, err := e.runtime.RunTask(ctx, task.WASMPath, task.RawInput)
	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	// Validate JSON output
	if !json.Valid(result) {
		return nil, fmt.Errorf("WASM returned invalid JSON")
	}

	durationMs := time.Since(startTime).Milliseconds()

	// Record verification if verifier is enabled
	if e.verifier != nil && task.ID != "" {
		inputHash := verification.HashInput(task.RawInput)
		outputHash := verification.HashOutput(result)
		
		verr := e.verifier.RecordVerification(context.Background(), verification.VerificationResult{
			TaskID:       task.ID,
			Method:       verification.MethodRedundant,
			PrimaryNode:  task.NodeID,
			InputHash:    inputHash,
			OutputHash:   outputHash,
			Match:        true, // First result always matches itself
			DurationMs:   durationMs,
			Timestamp:    time.Now().Unix(),
		})
		if verr != nil {
			fmt.Printf("⚠ Verification record error: %v\n", verr)
		}
	}

	return result, nil
}

type Task struct {
	ID         string          `json:"-"`
	NodeID     string          `json:"-"`
	WASMPath   string          `json:"-"`
	RawInput   json.RawMessage `json:"-"`
	TimeoutSec int             `json:"timeout_sec"`
	Credits    int             `json:"credits"`
}

// Runtime returns the underlying WASM runtime.
func (e *Executor) Runtime() *wasm.Runtime {
	return e.runtime
}
