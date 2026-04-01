package p2p

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHelloMsg(t *testing.T) {
	// Test serialization
	hello := HelloMsg{
		Type:     "hello",
		ID:       "test-node-123",
		Credits:  500,
	}

	data, err := json.Marshal(hello)
	if err != nil {
		t.Fatalf("Failed to marshal HelloMsg: %v", err)
	}

	var decoded HelloMsg
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal HelloMsg: %v", err)
	}

	if decoded.Type != hello.Type {
		t.Errorf("Type = %s, want %s", decoded.Type, hello.Type)
	}
	if decoded.ID != hello.ID {
		t.Errorf("ID = %s, want %s", decoded.ID, hello.ID)
	}
	if decoded.Credits != hello.Credits {
		t.Errorf("Credits = %d, want %d", decoded.Credits, hello.Credits)
	}
}

func TestTaskSerialization(t *testing.T) {
	task := Task{
		ID:         "task-123",
		Code:       "print('hello')",
		Lang:       "python",
		TimeoutSec: 30,
		Credits:    10,
		State:      "pending",
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Failed to marshal Task: %v", err)
	}

	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal Task: %v", err)
	}

	if decoded.ID != task.ID {
		t.Errorf("ID = %s, want %s", decoded.ID, task.ID)
	}
	if decoded.Lang != task.Lang {
		t.Errorf("Lang = %s, want %s", decoded.Lang, task.Lang)
	}
	if decoded.TimeoutSec != task.TimeoutSec {
		t.Errorf("TimeoutSec = %d, want %d", decoded.TimeoutSec, task.TimeoutSec)
	}
}

func TestTaskResultSerialization(t *testing.T) {
	result := TaskResult{
		ID:      "result-123",
		Success: true,
		Result:  json.RawMessage(`{"output": 42}`),
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal TaskResult: %v", err)
	}

	var decoded TaskResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal TaskResult: %v", err)
	}

	if decoded.ID != result.ID {
		t.Errorf("ID = %s, want %s", decoded.ID, result.ID)
	}
	if decoded.Success != result.Success {
		t.Errorf("Success = %v, want %v", decoded.Success, result.Success)
	}
}

func TestExtractPeerID(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		expected string
	}{
		{
			name:     "full multiaddr",
			addr:     "/ip4/192.168.1.1/tcp/9000/p2p/QmPeerID123",
			expected: "QmPeerID123",
		},
		{
			name:     "multiaddr with trailing",
			addr:     "/ip4/192.168.1.1/tcp/9000/p2p/QmPeerID123/ws",
			expected: "QmPeerID123",
		},
		{
			name:     "bare peer ID (Qm prefix)",
			addr:     "QmPeerID123Example",
			expected: "QmPeerID123Example",
		},
		{
			name:     "no peer id - returns addr as-is",
			addr:     "/ip4/192.168.1.1/tcp/9000",
			expected: "/ip4/192.168.1.1/tcp/9000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPeerID(tt.addr)
			if result != tt.expected {
				t.Errorf("extractPeerID(%s) = %s, want %s", tt.addr, result, tt.expected)
			}
		})
	}
}

func TestTaskWithParams(t *testing.T) {
	task := Task{
		ID:     "chunked-task-0",
		Params: json.RawMessage(`{"type":"range","start":0,"end":1000}`),
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Failed to marshal Task with params: %v", err)
	}

	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal Task: %v", err)
	}

	var params map[string]interface{}
	if err := json.Unmarshal(decoded.Params, &params); err != nil {
		t.Fatalf("Failed to unmarshal params: %v", err)
	}

	if params["type"] != "range" {
		t.Errorf("params[type] = %v, want range", params["type"])
	}
}

func TestTask_Error(t *testing.T) {
	task := Task{
		ID:    "failed-task",
		State: "failed",
		Error: "connection timeout",
	}

	if task.Error != "connection timeout" {
		t.Errorf("task.Error = %s, want 'connection timeout'", task.Error)
	}
}

func TestTask_CompletedAt(t *testing.T) {
	now := time.Now()
	task := Task{
		ID:          "completed-task",
		State:       "completed",
		CompletedAt: &now,
	}

	if task.CompletedAt == nil {
		t.Error("CompletedAt should not be nil")
	}
}

func BenchmarkExtractPeerID(b *testing.B) {
	addr := "/ip4/192.168.1.1/tcp/9000/p2p/QmPeerID123"
	for i := 0; i < b.N; i++ {
		extractPeerID(addr)
	}
}