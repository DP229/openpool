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
	if cap(sched.sem) != DefaultMaxInFlight {
		t.Errorf("semaphore cap = %d, want %d", cap(sched.sem), DefaultMaxInFlight)
	}
}

func TestNewWithConfig(t *testing.T) {
	ledger := newMockLedger()
	node := p2p.NewNode(ledger)
	cfg := SchedulerConfig{
		MaxRetries:      5,
		MaxInFlight:     16,
		RetryBaseMs:     100,
		RetryMaxMs:      5000,
		RetryMultiplier: 3.0,
		CollectTimeout:  10 * time.Minute,
	}

	sched := NewWithConfig(node, "custom-node", cfg)

	if sched.config.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", sched.config.MaxRetries)
	}
	if cap(sched.sem) != 16 {
		t.Errorf("semaphore cap = %d, want 16", cap(sched.sem))
	}
	if sched.config.RetryMultiplier != 3.0 {
		t.Errorf("RetryMultiplier = %f, want 3.0", sched.config.RetryMultiplier)
	}
}

func TestSchedulerConfig_Defaults(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	if cfg.MaxInFlight != DefaultMaxInFlight {
		t.Errorf("MaxInFlight = %d, want %d", cfg.MaxInFlight, DefaultMaxInFlight)
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, DefaultMaxRetries)
	}
	if cfg.CollectTimeout != DefaultCollectTimeout {
		t.Errorf("CollectTimeout = %v, want %v", cfg.CollectTimeout, DefaultCollectTimeout)
	}
}

func TestSchedulerConfig_ZeroDefaults(t *testing.T) {
	ledger := newMockLedger()
	node := p2p.NewNode(ledger)
	cfg := SchedulerConfig{}
	sched := NewWithConfig(node, "zero-node", cfg)

	if sched.config.MaxInFlight != DefaultMaxInFlight {
		t.Errorf("zero MaxInFlight should default to %d", DefaultMaxInFlight)
	}
	if sched.config.MaxRetries != DefaultMaxRetries {
		t.Errorf("zero MaxRetries should default to %d", DefaultMaxRetries)
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

// ── Semaphore (Backpressure) Tests ─────────────────────────────────────────────────

func TestSemaphore_Capacity(t *testing.T) {
	ledger := newMockLedger()
	node := p2p.NewNode(ledger)
	cfg := SchedulerConfig{MaxInFlight: 4}
	sched := NewWithConfig(node, "sem-test", cfg)

	if cap(sched.sem) != 4 {
		t.Errorf("semaphore capacity = %d, want 4", cap(sched.sem))
	}
}

func TestSemaphore_AcquireRelease(t *testing.T) {
	sem := make(chan struct{}, 2)

	sem <- struct{}{}
	sem <- struct{}{}

	select {
	case sem <- struct{}{}:
		t.Error("semaphore should be full")
	default:
	}

	<-sem
	select {
	case sem <- struct{}{}:
	default:
		t.Error("semaphore should have room after release")
	}
}

// ── Checkpoint Store Tests ─────────────────────────────────────────────────────────

func TestCheckpointStore_SaveLoad(t *testing.T) {
	store := NewCheckpointStore(NewMemoryCheckpointer())

	cp := &Checkpoint{
		JobID:     "job-42",
		Completed: map[string]bool{"a": true, "b": true},
		Results: map[string][]byte{
			"a": []byte(`{"node_id":"a","success":true}`),
			"b": []byte(`{"node_id":"b","success":true}`),
		},
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}

	if err := store.Save(cp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load("job-42")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.JobID != "job-42" {
		t.Errorf("JobID = %s, want job-42", loaded.JobID)
	}
	if len(loaded.Completed) != 2 {
		t.Errorf("Completed = %d, want 2", len(loaded.Completed))
	}
	if !loaded.Completed["a"] {
		t.Error("node 'a' should be marked completed")
	}
}

func TestCheckpointStore_Delete(t *testing.T) {
	store := NewCheckpointStore(NewMemoryCheckpointer())

	cp := &Checkpoint{JobID: "job-del", Completed: map[string]bool{}}
	store.Save(cp)

	if err := store.Delete("job-del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Load("job-del")
	if err == nil {
		t.Error("Load after Delete should fail")
	}
}

func TestCheckpointStore_LoadMissing(t *testing.T) {
	store := NewCheckpointStore(NewMemoryCheckpointer())

	_, err := store.Load("nonexistent")
	if err == nil {
		t.Error("Load nonexistent should return error")
	}
}

func TestMemoryCheckpointer(t *testing.T) {
	mc := NewMemoryCheckpointer()

	cp1 := &Checkpoint{JobID: "j1", Completed: map[string]bool{"x": true}}
	cp2 := &Checkpoint{JobID: "j2", Completed: map[string]bool{"y": true}}

	mc.CheckpointSave(cp1)
	mc.CheckpointSave(cp2)

	loaded, err := mc.CheckpointLoad("j2")
	if err != nil {
		t.Fatalf("Load j2: %v", err)
	}
	if !loaded.Completed["y"] {
		t.Error("j2 should have y completed")
	}

	mc.CheckpointDelete("j1")
	if _, err := mc.CheckpointLoad("j1"); err == nil {
		t.Error("j1 should be deleted")
	}
}

func TestSchedulerCheckpoint_Integration(t *testing.T) {
	ledger := newMockLedger()
	node := p2p.NewNode(ledger)
	sched := New(node, "cp-test")

	cp := &Checkpoint{
		JobID:     "dag-job-1",
		Completed: map[string]bool{"tile-0-0": true},
		Results:   map[string][]byte{},
	}
	if err := sched.SaveCheckpoint(cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	loaded, err := sched.LoadCheckpoint("dag-job-1")
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	if !loaded.Completed["tile-0-0"] {
		t.Error("tile-0-0 should be completed in loaded checkpoint")
	}
}
