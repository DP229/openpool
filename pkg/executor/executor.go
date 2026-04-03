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
	runtime  *wasm.Runtime
	ledger   *ledger.Ledger
	verifier *verification.Verifier
	nodeID   string
}

// New creates a new task executor.
func New(runtime *wasm.Runtime, ledger *ledger.Ledger, verifier *verification.Verifier) *Executor {
	return &Executor{runtime: runtime, ledger: ledger, verifier: verifier}
}

// SetNodeID sets the local node ID for verification records.
func (e *Executor) SetNodeID(id string) {
	e.nodeID = id
}

// Execute runs a task and returns the result.
func (e *Executor) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
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

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startTime := time.Now()

	// Use WASM path from task, pass raw JSON input
	result, err := e.runtime.RunTask(execCtx, task.WASMPath, task.RawInput)
	execDuration := time.Since(startTime)

	// Check if context was cancelled (timeout or cancellation)
	if ctx.Err() != nil {
		if e.verifier != nil && task.ID != "" {
			e.verifier.RecordVerification(context.Background(), verification.VerificationResult{
				TaskID:       task.ID,
				Method:       verification.MethodNone,
				PrimaryNode:  task.NodeID,
				VerifierNode: e.nodeID,
				InputHash:    verification.HashInput(task.RawInput),
				OutputHash:   "",
				Match:        false,
				DurationMs:   execDuration.Milliseconds(),
				Timestamp:    time.Now().Unix(),
				Error:        fmt.Sprintf("task cancelled: %v", ctx.Err()),
			})
		}
		return nil, fmt.Errorf("execution cancelled: %w", ctx.Err())
	}

	if err != nil {
		if e.verifier != nil && task.ID != "" {
			e.verifier.RecordVerification(context.Background(), verification.VerificationResult{
				TaskID:       task.ID,
				Method:       verification.MethodNone,
				PrimaryNode:  task.NodeID,
				VerifierNode: e.nodeID,
				InputHash:    verification.HashInput(task.RawInput),
				OutputHash:   "",
				Match:        false,
				DurationMs:   execDuration.Milliseconds(),
				Timestamp:    time.Now().Unix(),
				Error:        fmt.Sprintf("execution failed: %v", err),
			})
		}
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	// Validate JSON output
	if !json.Valid(result) {
		if e.verifier != nil && task.ID != "" {
			e.verifier.RecordVerification(context.Background(), verification.VerificationResult{
				TaskID:       task.ID,
				Method:       verification.MethodNone,
				PrimaryNode:  task.NodeID,
				VerifierNode: e.nodeID,
				InputHash:    verification.HashInput(task.RawInput),
				OutputHash:   verification.HashOutput(result),
				Match:        false,
				DurationMs:   execDuration.Milliseconds(),
				Timestamp:    time.Now().Unix(),
				Error:        "WASM returned invalid JSON",
			})
		}
		return nil, fmt.Errorf("WASM returned invalid JSON")
	}

	// Record verification audit if enabled and task has an ID
	var needsVerify bool
	if e.verifier != nil && task.ID != "" {
		inputHash := verification.HashInput(task.RawInput)
		outputHash := verification.HashOutput(result)
		needsVerify = e.verifier.ShouldVerify(task.Credits)
		method := verification.MethodNone
		if needsVerify {
			method = verification.MethodRedundant
		}

		verr := e.verifier.RecordVerification(context.Background(), verification.VerificationResult{
			TaskID:       task.ID,
			Method:       method,
			PrimaryNode:  task.NodeID,
			VerifierNode: e.nodeID,
			InputHash:    inputHash,
			OutputHash:   outputHash,
			Match:        true,
			DurationMs:   execDuration.Milliseconds(),
			Timestamp:    time.Now().Unix(),
		})
		if verr != nil {
			fmt.Printf("⚠ Verification record error: %v\n", verr)
		}
	}

	return &ExecutionResult{
		Result:     result,
		DurationMs: execDuration.Milliseconds(),
		Verified:   needsVerify,
		InputHash:  verification.HashInput(task.RawInput),
		OutputHash: verification.HashOutput(result),
	}, nil
}

// ExecutionResult contains the task result with verification metadata.
type ExecutionResult struct {
	Result     json.RawMessage `json:"result"`
	DurationMs int64           `json:"duration_ms"`
	Verified   bool            `json:"verified"`
	InputHash  string          `json:"input_hash"`
	OutputHash string          `json:"output_hash"`
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
