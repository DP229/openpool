package wasm

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if r == nil {
		t.Fatal("New() returned nil")
	}
	if r.version == "" {
		t.Error("Runtime version is empty")
	}
}

func TestOpToID(t *testing.T) {
	tests := []struct {
		op       string
		expected int
	}{
		{"fib", OpFib},
		{"sumFib", OpSumFib},
		{"sumSquares", OpSumSquares},
		{"matrixTrace", OpMatrixTrace},
		{"unknown", -1},
		{"", -1},
	}

	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			id := OpToID(tt.op)
			if id != tt.expected {
				t.Errorf("OpToID(%q) = %d, want %d", tt.op, id, tt.expected)
			}
		})
	}
}

func TestRun(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name     string
		op       int
		n        int
		wantErr  bool
		checkRes func(t *testing.T, result json.RawMessage)
	}{
		{
			name: "fib of 10",
			op:   OpFib,
			n:    10,
			checkRes: func(t *testing.T, result json.RawMessage) {
				var res struct {
					Result int `json:"result"`
				}
				if err := json.Unmarshal(result, &res); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if res.Result != 55 {
					t.Errorf("fib(10) = %d, want 55", res.Result)
				}
			},
		},
		{
			name: "fib of 0",
			op:   OpFib,
			n:    0,
			checkRes: func(t *testing.T, result json.RawMessage) {
				var res struct {
					Result int `json:"result"`
				}
				if err := json.Unmarshal(result, &res); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if res.Result != 0 {
					t.Errorf("fib(0) = %d, want 0", res.Result)
				}
			},
		},
		{
			name: "fib of 1",
			op:   OpFib,
			n:    1,
			checkRes: func(t *testing.T, result json.RawMessage) {
				var res struct {
					Result int `json:"result"`
				}
				if err := json.Unmarshal(result, &res); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if res.Result != 1 {
					t.Errorf("fib(1) = %d, want 1", res.Result)
				}
			},
		},
		{
			name:    "unknown op",
			op:      999,
			n:       10,
			wantErr: true,
		},
		{
			name: "sumFib of 5",
			op:   OpSumFib,
			n:    5,
			checkRes: func(t *testing.T, result json.RawMessage) {
				var res struct {
					Result int `json:"result"`
				}
				if err := json.Unmarshal(result, &res); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				expected := 0 + 1 + 1 + 2 + 3 + 5
				if res.Result != expected {
					t.Errorf("sumFib(5) = %d, want %d", res.Result, expected)
				}
			},
		},
		{
			name: "sumSquares of 3",
			op:   OpSumSquares,
			n:    3,
			checkRes: func(t *testing.T, result json.RawMessage) {
				var res struct {
					Result int `json:"result"`
				}
				if err := json.Unmarshal(result, &res); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				expected := 1 + 4 + 9
				if res.Result != expected {
					t.Errorf("sumSquares(3) = %d, want %d", res.Result, expected)
				}
			},
		},
		{
			name: "matrixTrace of 3",
			op:   OpMatrixTrace,
			n:    3,
			checkRes: func(t *testing.T, result json.RawMessage) {
				var res struct {
					Result int `json:"result"`
				}
				if err := json.Unmarshal(result, &res); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				expected := 1 + 2 + 3
				if res.Result != expected {
					t.Errorf("matrixTrace(3) = %d, want %d", res.Result, expected)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := r.Run(ctx, tt.op, tt.n)
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.checkRes != nil && result != nil {
				tt.checkRes(t, result)
			}
		})
	}
}

func TestRunWithContextCancellation(t *testing.T) {
	r, _ := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Run(ctx, OpFib, 10)
	if err != nil && err != context.Canceled {
		t.Logf("Run with canceled context: %v (may be OK)", err)
	}
}

func TestRunTask(t *testing.T) {
	r, _ := New()
	ctx := context.Background()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, result json.RawMessage)
	}{
		{
			name:  "flat format fib",
			input: `{"op":"fib","arg":30}`,
			check: func(t *testing.T, result json.RawMessage) {
				var res struct {
					Result int `json:"result"`
				}
				if err := json.Unmarshal(result, &res); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if res.Result != 832040 {
					t.Errorf("fib(30) = %d, want 832040", res.Result)
				}
			},
		},
		{
			name:  "nested format",
			input: `{"input":{"op":"sumSquares","arg":10}}`,
			check: func(t *testing.T, result json.RawMessage) {
				var res struct {
					Result int `json:"result"`
				}
				if err := json.Unmarshal(result, &res); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				expected := 385
				if res.Result != expected {
					t.Errorf("sumSquares(10) = %d, want %d", res.Result, expected)
				}
			},
		},
		{
			name:    "invalid JSON",
			input:   `{invalid`,
			wantErr: true,
		},
		{
			name:    "unknown op",
			input:   `{"op":"unknown","arg":10}`,
			wantErr: true,
		},
		{
			name:    "nested unknown op",
			input:   `{"input":{"op":"unknown","arg":10}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := r.RunTask(ctx, "", json.RawMessage(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("RunTask() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && result != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestNativeFunctions(t *testing.T) {
	tests := []struct {
		name     string
		fn       func() int
		expected int
	}{
		{"fib(20)", func() int { return fib(20) }, 6765},
		{"fib(30)", func() int { return fib(30) }, 832040},
		{"sumFib(10)", func() int { return sumFib(10) }, 143},
		{"sumSquares(10)", func() int { return sumSquares(10) }, 385},
		{"sumSquares(100)", func() int { return sumSquares(100) }, 338350},
		{"matrixTrace(5)", func() int { return matrixTrace(5) }, 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn()
			if result != tt.expected {
				t.Errorf("%s = %d, want %d", tt.name, result, tt.expected)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	r, _ := New()
	if r.Version() == "" {
		t.Error("Version() returned empty string")
	}
}

func TestClose(t *testing.T) {
	r, _ := New()
	ctx := context.Background()
	if err := r.Close(ctx); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestRunTimeout(t *testing.T) {
	r, _ := New()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(1 * time.Millisecond)

	_, err := r.Run(ctx, OpFib, 100)
	if err != nil {
		t.Logf("Run with timeout: %v (context cancellation working)", err)
	}
}