package scheduler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dp229/openpool/pkg/p2p"
)

type mockLedger struct {
	credits map[string]int
}

func newMockLedger() *mockLedger {
	return &mockLedger{credits: make(map[string]int)}
}

func (m *mockLedger) AddCredits(id string, amt int) int {
	m.credits[id] += amt
	if m.credits[id] < 0 {
		m.credits[id] = 0
	}
	return m.credits[id]
}

func (m *mockLedger) GetCredits(id string) int {
	return m.credits[id]
}

func TestChunk(t *testing.T) {
	chunk := Chunk{
		ID:      "test-chunk-0",
		TaskID:  "parent-task",
		Index:   0,
		Params:  json.RawMessage(`{"type":"range","start":0,"end":100}`),
		Credits: 10,
		Timeout: 60,
	}

	if chunk.ID != "test-chunk-0" {
		t.Errorf("chunk.ID = %s, want test-chunk-0", chunk.ID)
	}
	if chunk.Credits != 10 {
		t.Errorf("chunk.Credits = %d, want 10", chunk.Credits)
	}
}

func TestChunkResult(t *testing.T) {
	result := ChunkResult{
		ChunkID:    "chunk-0",
		Index:      0,
		Success:    true,
		Data:       json.RawMessage(`{"sum":500}`),
		DurationMs: 150,
	}

	if !result.Success {
		t.Error("result.Success should be true")
	}
	if result.DurationMs != 150 {
		t.Errorf("result.DurationMs = %d, want 150", result.DurationMs)
	}
}

func TestMapReduceSplit(t *testing.T) {
	task := &p2p.Task{ID: "test-task", TimeoutSec: 60}

	mr := MapReduce{
		Split: func(t *p2p.Task) ([]Chunk, error) {
			chunks := make([]Chunk, 4)
			for i := 0; i < 4; i++ {
				chunks[i] = Chunk{
					ID:      t.ID + "-chunk-" + string(rune('0'+i)),
					TaskID:  t.ID,
					Index:   i,
					Params:  json.RawMessage(`{"type":"range","start":0,"end":100}`),
					Credits: 10,
					Timeout: 30,
				}
			}
			return chunks, nil
		},
		Reduce: func(results []ChunkResult) (json.RawMessage, error) {
			return json.RawMessage(`{"status":"completed"}`), nil
		},
	}

	chunks, err := mr.Split(task)
	if err != nil {
		t.Fatalf("Split error: %v", err)
	}
	if len(chunks) != 4 {
		t.Errorf("len(chunks) = %d, want 4", len(chunks))
	}
	for i, c := range chunks {
		if c.Index != i {
			t.Errorf("chunk[%d].Index = %d, want %d", i, c.Index, i)
		}
	}
}

func TestMapReduceReduce(t *testing.T) {
	results := []ChunkResult{
		{ChunkID: "chunk-0", Index: 0, Success: true, Data: json.RawMessage(`{"sum":100}`)},
		{ChunkID: "chunk-1", Index: 1, Success: true, Data: json.RawMessage(`{"sum":200}`)},
		{ChunkID: "chunk-2", Index: 2, Success: false, Error: "connection timeout"},
	}

	mr := MapReduce{
		Reduce: func(results []ChunkResult) (json.RawMessage, error) {
			var total int
			successCount := 0
			for _, r := range results {
				if r.Success {
					var d struct {
						Sum int `json:"sum"`
					}
					json.Unmarshal(r.Data, &d)
					total += d.Sum
					successCount++
				}
			}
			return json.RawMessage(`{"total":` + string(rune('0'+total)) + `,"success":` + string(rune('0'+successCount)) + `}`), nil
		},
	}

	final, err := mr.Reduce(results)
	if err != nil {
		t.Fatalf("Reduce error: %v", err)
	}
	if final == nil {
		t.Error("Reduce returned nil result")
	}
}

func TestNewScheduler(t *testing.T) {
	ledger := newMockLedger()
	node := p2p.NewNode(ledger)
	node.ID = "test-node"

	sched := New(node, "test-node")

	if sched == nil {
		t.Fatal("New returned nil")
	}
	if sched.node != node {
		t.Error("scheduler node mismatch")
	}
	if sched.nodeID != "test-node" {
		t.Errorf("scheduler.nodeID = %s, want test-node", sched.nodeID)
	}
}

func TestTimePtr(t *testing.T) {
	now := time.Now()
	ptr := timePtr(now)

	if ptr == nil {
		t.Fatal("timePtr returned nil")
	}
	if !ptr.Equal(now) {
		t.Errorf("timePtr returned wrong time: %v, want %v", ptr, now)
	}
}

func TestCountSuccess(t *testing.T) {
	tests := []struct {
		name     string
		results  []ChunkResult
		expected int
	}{
		{"empty", []ChunkResult{}, 0},
		{"all success", []ChunkResult{
			{Success: true},
			{Success: true},
			{Success: true},
		}, 3},
		{"mixed", []ChunkResult{
			{Success: true},
			{Success: false},
			{Success: true},
			{Success: false},
		}, 2},
		{"all failure", []ChunkResult{
			{Success: false},
			{Success: false},
		}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := countSuccess(tt.results)
			if count != tt.expected {
				t.Errorf("countSuccess() = %d, want %d", count, tt.expected)
			}
		})
	}
}

func TestSubmitChunked_NoPeers(t *testing.T) {
	ledger := newMockLedger()
	node := p2p.NewNode(ledger)
	node.ID = "test-scheduler"

	sched := New(node, "test-scheduler")

	task := &p2p.Task{ID: "test-task", TimeoutSec: 60}

	mr := MapReduce{
		Split: func(t *p2p.Task) ([]Chunk, error) {
			return []Chunk{{ID: "chunk-0"}}, nil
		},
		Reduce: func(results []ChunkResult) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := sched.SubmitChunked(ctx, task, mr)
	if err == nil {
		t.Error("SubmitChunked should fail without peers")
	}
}