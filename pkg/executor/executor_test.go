package executor

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/dp229/openpool/pkg/ledger"
	"github.com/dp229/openpool/pkg/verification"
	"github.com/dp229/openpool/pkg/wasm"
)

func TestNew(t *testing.T) {
	r, _ := wasm.New()
	l, _ := ledger.New(":memory:")
	v, _ := verification.NewWithDefaults(":memory:")

	e := New(r, l, v)
	if e == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNewNilRuntime(t *testing.T) {
	l, _ := ledger.New(":memory:")

	e := New(nil, l, nil)
	if e == nil {
		t.Fatal("New() returned nil")
	}
}

func TestExecute(t *testing.T) {
	r, _ := wasm.New()
	l, _ := ledger.New(":memory:")
	v, _ := verification.NewWithDefaults(":memory:")

	e := New(r, l, v)

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "fib operation",
			input: `{"op":"fib","arg":30}`,
		},
		{
			name:  "sumSquares operation",
			input: `{"op":"sumSquares","arg":10}`,
		},
		{
			name:  "matrixTrace operation",
			input: `{"op":"matrixTrace","arg":5}`,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid`,
			wantErr: true,
		},
		{
			name:    "unknown op",
			input:   `{"op":"unknown","arg":1}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			task := &Task{
				ID:         "test-" + tt.name,
				RawInput:   json.RawMessage(tt.input),
				TimeoutSec: 30,
			}

			result, err := e.Execute(ctx, task)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result == nil {
				t.Error("Execute() returned nil result without error")
			}
		})
	}
}

func TestExecuteWithTimeout(t *testing.T) {
	r, _ := wasm.New()
	l, _ := ledger.New(":memory:")
	e := New(r, l, nil)

	ctx := context.Background()
	task := &Task{
		ID:         "test-timeout",
		RawInput:   json.RawMessage(`{"op":"fib","arg":30}`),
		TimeoutSec: 1,
	}

	result, err := e.Execute(ctx, task)
	if err != nil {
		t.Errorf("Execute() with timeout error = %v", err)
	}
	_ = result
}

func TestExecuteWithContextCancellation(t *testing.T) {
	r, _ := wasm.New()
	l, _ := ledger.New(":memory:")
	e := New(r, l, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	task := &Task{
		ID:         "test-cancel",
		RawInput:   json.RawMessage(`{"op":"fib","arg":30}`),
		TimeoutSec: 30,
	}

	_, err := e.Execute(ctx, task)
	if err != nil {
		t.Logf("Execute() with canceled context returned error: %v (expected)", err)
	}
}

func TestExecuteWithCredits(t *testing.T) {
	r, _ := wasm.New()
	l, _ := ledger.New(":memory:")
	l.AddCredits("test-node", 1000)
	e := New(r, l, nil)

	ctx := context.Background()
	task := &Task{
		ID:         "test-credits",
		NodeID:     "test-node",
		RawInput:   json.RawMessage(`{"op":"fib","arg":10}`),
		TimeoutSec: 30,
		Credits:    50,
	}

	result, err := e.Execute(ctx, task)
	if err != nil {
		t.Errorf("Execute() error = %v", err)
	}

	// Verify result is valid JSON
	if !json.Valid(result) {
		t.Error("Execute() returned invalid JSON")
	}
}

func TestExecuteWithVerification(t *testing.T) {
	r, _ := wasm.New()
	l, _ := ledger.New(":memory:")
	v, _ := verification.NewWithDefaults(":memory:")
	defer v.Close()

	e := New(r, l, v)

	ctx := context.Background()
	task := &Task{
		ID:         "test-verification",
		NodeID:     "test-node",
		RawInput:   json.RawMessage(`{"op":"fib","arg":20}`),
		TimeoutSec: 30,
	}

	result, err := e.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Check verification was recorded
	history, err := v.GetVerificationHistory("test-verification")
	if err != nil {
		t.Fatalf("GetVerificationHistory() error = %v", err)
	}

	if len(history) != 1 {
		t.Errorf("Verification history length = %d, want 1", len(history))
		return
	}

	if history[0].PrimaryNode != "test-node" {
		t.Errorf("PrimaryNode = %s, want test-node", history[0].PrimaryNode)
	}

	_ = result
}

func TestExecuteDefaultTimeout(t *testing.T) {
	r, _ := wasm.New()
	l, _ := ledger.New(":memory:")
	e := New(r, l, nil)

	ctx := context.Background()
	task := &Task{
		ID:         "test-default-timeout",
		RawInput:   json.RawMessage(`{"op":"fib","arg":5}`),
		TimeoutSec: 0,
	}

	result, err := e.Execute(ctx, task)
	if err != nil {
		t.Errorf("Execute() with default timeout error = %v", err)
	}
	_ = result
}

func TestExecuteNilRuntime(t *testing.T) {
	l, _ := ledger.New(":memory:")
	e := New(nil, l, nil)

	ctx := context.Background()
	task := &Task{
		ID:         "test-nil-runtime",
		RawInput:   json.RawMessage(`{"op":"fib","arg":10}`),
		TimeoutSec: 30,
	}

	_, err := e.Execute(ctx, task)
	if err == nil {
		t.Error("Execute() should return error with nil runtime")
	}
}

func TestRuntime(t *testing.T) {
	r, _ := wasm.New()
	l, _ := ledger.New(":memory:")
	e := New(r, l, nil)

	runtime := e.Runtime()
	if runtime == nil {
		t.Error("Runtime() returned nil")
	}
}

func TestTaskFields(t *testing.T) {
	task := &Task{
		ID:         "task-123",
		NodeID:     "node-456",
		WASMPath:   "/path/to/module.wasm",
		RawInput:   json.RawMessage(`{"op":"fib","arg":30}`),
		TimeoutSec: 60,
		Credits:    100,
	}

	if task.ID != "task-123" {
		t.Errorf("task.ID = %s, want task-123", task.ID)
	}
	if task.NodeID != "node-456" {
		t.Errorf("task.NodeID = %s, want node-456", task.NodeID)
	}
	if task.WASMPath != "/path/to/module.wasm" {
		t.Errorf("task.WASMPath = %s", task.WASMPath)
	}
	if task.TimeoutSec != 60 {
		t.Errorf("task.TimeoutSec = %d, want 60", task.TimeoutSec)
	}
	if task.Credits != 100 {
		t.Errorf("task.Credits = %d, want 100", task.Credits)
	}
}

func BenchmarkExecute(b *testing.B) {
	r, _ := wasm.New()
	l, _ := ledger.New(":memory:")
	e := New(r, l, nil)

	ctx := context.Background()
	task := &Task{
		RawInput:   json.RawMessage(`{"op":"fib","arg":30}`),
		TimeoutSec: 30,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Execute(ctx, task)
	}
}

func BenchmarkExecuteWithVerification(b *testing.B) {
	r, _ := wasm.New()
	l, _ := ledger.New(":memory:")
	v, _ := verification.NewWithDefaults(":memory:")
	e := New(r, l, v)

	ctx := context.Background()
	task := &Task{
		ID:         "bench-task",
		RawInput:   json.RawMessage(`{"op":"fib","arg":20}`),
		TimeoutSec: 30,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task.ID = "bench-task-" + string(rune(i))
		e.Execute(ctx, task)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}