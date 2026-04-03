package verification

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"in-memory", ":memory:", false},
		{"temp file", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if path == "" {
				f, err := os.CreateTemp("", "verifier_test_*.db")
				if err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				path = f.Name()
				f.Close()
				defer os.Remove(path)
			}

			v, err := New(path, DefaultConfig())
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if v != nil {
				v.Close()
			}
		})
	}
}

func TestNewWithDefaults(t *testing.T) {
	v, err := NewWithDefaults(":memory:")
	if err != nil {
		t.Fatalf("NewWithDefaults() error = %v", err)
	}
	defer v.Close()

	cfg := v.GetConfig()
	if cfg.RedundantCount != 2 {
		t.Errorf("Default redundant count = %d, want 2", cfg.RedundantCount)
	}
	if cfg.AcceptThreshold != 1.0 {
		t.Errorf("Default accept threshold = %f, want 1.0", cfg.AcceptThreshold)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RedundantCount <= 0 {
		t.Error("RedundantCount should be positive")
	}
	if cfg.AcceptThreshold < 0 || cfg.AcceptThreshold > 1 {
		t.Error("AcceptThreshold should be between 0 and 1")
	}
}

func TestHashInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int // length of hash
	}{
		{"empty", "{}", 64},
		{"object", `{"key":"value"}`, 64},
		{"number", `{"n":123}`, 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := HashInput(json.RawMessage(tt.input))
			if len(hash) != tt.expected {
				t.Errorf("HashInput() hash length = %d, want %d", len(hash), tt.expected)
			}
		})
	}
}

func TestHashOutput(t *testing.T) {
	input1 := json.RawMessage(`{"result":42}`)
	input2 := json.RawMessage(`{"result":42}`)
	input3 := json.RawMessage(`{"result":43}`)

	hash1 := HashOutput(input1)
	hash2 := HashOutput(input2)
	hash3 := HashOutput(input3)

	if hash1 != hash2 {
		t.Error("Same input should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("Different input should produce different hash")
	}
}

func TestVerifyResult(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{"same", `{"result":42}`, `{"result":42}`, true},
		{"different", `{"result":42}`, `{"result":43}`, false},
		{"empty", `{}`, `{}`, true},
		{"order matters", `{"a":1,"b":2}`, `{"b":2,"a":1}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := VerifyResult(json.RawMessage(tt.a), json.RawMessage(tt.b))
			if result != tt.expected {
				t.Errorf("VerifyResult(%s, %s) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestRecordVerification(t *testing.T) {
	v, _ := NewWithDefaults(":memory:")
	defer v.Close()

	ctx := context.Background()
	res := VerificationResult{
		TaskID:       "task-1",
		Method:       MethodRedundant,
		PrimaryNode:  "node-1",
		VerifierNode: "node-2",
		InputHash:    HashInput(json.RawMessage(`{"op":"fib","arg":30}`)),
		OutputHash:   HashOutput(json.RawMessage(`{"result":832040}`)),
		Match:        true,
		DurationMs:   150,
		Timestamp:    1234567890,
	}

	if err := v.RecordVerification(ctx, res); err != nil {
		t.Fatalf("RecordVerification() error = %v", err)
	}

	history, err := v.GetVerificationHistory("task-1")
	if err != nil {
		t.Fatalf("GetVerificationHistory() error = %v", err)
	}

	if len(history) != 1 {
		t.Errorf("GetVerificationHistory() returned %d results, want 1", len(history))
		return
	}

	if history[0].TaskID != "task-1" {
		t.Errorf("history[0].TaskID = %s, want task-1", history[0].TaskID)
	}
	if history[0].PrimaryNode != "node-1" {
		t.Errorf("history[0].PrimaryNode = %s, want node-1", history[0].PrimaryNode)
	}
}

func TestGetNodeScore(t *testing.T) {
	v, _ := NewWithDefaults(":memory:")
	defer v.Close()
	ctx := context.Background()

	score, matched, total, err := v.GetNodeScore("unknown-node")
	if err != nil {
		t.Fatalf("GetNodeScore() error = %v", err)
	}
	if score != 0.5 {
		t.Errorf("Initial score = %f, want 0.5 (default)", score)
	}

	v.RecordVerification(ctx, VerificationResult{
		TaskID:      "task-1",
		Method:      MethodRedundant,
		PrimaryNode: "node-1",
		InputHash:   "hash1",
		OutputHash:  "hash2",
		Match:       true,
		Timestamp:   1,
	})

	v.RecordVerification(ctx, VerificationResult{
		TaskID:      "task-2",
		Method:      MethodRedundant,
		PrimaryNode: "node-1",
		InputHash:   "hash3",
		OutputHash:  "hash4",
		Match:       true,
		Timestamp:   2,
	})

	v.RecordVerification(ctx, VerificationResult{
		TaskID:      "task-3",
		Method:      MethodRedundant,
		PrimaryNode: "node-1",
		InputHash:   "hash5",
		OutputHash:  "hash6",
		Match:       false,
		Timestamp:   3,
	})

	score, matched, total, err = v.GetNodeScore("node-1")
	if err != nil {
		t.Fatalf("GetNodeScore() error = %v", err)
	}

	if total != 3 {
		t.Errorf("Total = %d, want 3", total)
	}
	if matched != 2 {
		t.Errorf("Matched = %d, want 2", matched)
	}
	expectedScore := float64(2) / float64(3)
	if score != expectedScore {
		t.Errorf("Score = %f, want %f", score, expectedScore)
	}
}

func TestShouldVerify(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RedundantCount = 2
	v, _ := New(":memory:", cfg)
	defer v.Close()

	tests := []struct {
		name     string
		credits  int
		expected bool
	}{
		{"below threshold", 5, false},
		{"at threshold", 10, true},
		{"above threshold", 50, true},
		{"zero", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.ShouldVerify(tt.credits)
			if result != tt.expected {
				t.Errorf("ShouldVerify(%d) = %v, want %v", tt.credits, result, tt.expected)
			}
		})
	}
}

func TestVerificationMethodString(t *testing.T) {
	tests := []struct {
		method   VerificationMethod
		expected string
	}{
		{MethodNone, "none"},
		{MethodRedundant, "redundant"},
		{MethodCheckpoint, "checkpoint"},
		{MethodVDF, "vdf"},
		{VerificationMethod(100), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.method.String()
			if result != tt.expected {
				t.Errorf("VerificationMethod(%d).String() = %s, want %s", tt.method, result, tt.expected)
			}
		})
	}
}

func TestGetAllNodeStats(t *testing.T) {
	v, _ := NewWithDefaults(":memory:")
	defer v.Close()
	ctx := context.Background()

	v.RecordVerification(ctx, VerificationResult{
		TaskID:      "task-1",
		PrimaryNode: "node-1",
		InputHash:   "h1",
		OutputHash:  "h2",
		Match:       true,
		Timestamp:   1,
	})

	v.RecordVerification(ctx, VerificationResult{
		TaskID:      "task-2",
		PrimaryNode: "node-2",
		InputHash:   "h3",
		OutputHash:  "h4",
		Match:       false,
		Timestamp:   2,
	})

	stats, err := v.GetAllNodeStats()
	if err != nil {
		t.Fatalf("GetAllNodeStats() error = %v", err)
	}

	if len(stats) != 2 {
		t.Errorf("GetAllNodeStats() returned %d stats, want 2", len(stats))
		return
	}

	for _, s := range stats {
		if s.NodeID == "node-1" {
			if s.TasksVerified != 1 {
				t.Errorf("node-1 TasksVerified = %d, want 1", s.TasksVerified)
			}
			if s.ReliabilityScore != 1.0 {
				t.Errorf("node-1 ReliabilityScore = %f, want 1.0", s.ReliabilityScore)
			}
		}
		if s.NodeID == "node-2" {
			if s.TasksMatched != 0 {
				t.Errorf("node-2 TasksMatched = %d, want 0", s.TasksMatched)
			}
		}
	}
}

func TestFormatDurationMs(t *testing.T) {
	tests := []struct {
		ms       int64
		expected string
	}{
		{100, "100ms"},
		{500, "500ms"},
		{1000, "1.00s"},
		{1500, "1.50s"},
		{5000, "5.00s"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatDurationMs(tt.ms)
			if result != tt.expected {
				t.Errorf("FormatDurationMs(%d) = %s, want %s", tt.ms, result, tt.expected)
			}
		})
	}
}

func TestConcurrentRecord(t *testing.T) {
	v, _ := NewWithDefaults(":memory:")
	defer v.Close()
	ctx := context.Background()

	done := make(chan bool)
	const numOps = 10

	for i := 0; i < numOps; i++ {
		go func(id int) {
			res := VerificationResult{
				TaskID:      "task-concurrent",
				Method:      MethodRedundant,
				PrimaryNode: "node-test",
				InputHash:   "hash",
				OutputHash:  "hash",
				Match:       true,
				Timestamp:   int64(id),
			}
			v.RecordVerification(ctx, res)
			done <- true
		}(i)
	}

	for i := 0; i < numOps; i++ {
		<-done
	}

	history, err := v.GetVerificationHistory("task-concurrent")
	if err != nil {
		t.Fatalf("GetVerificationHistory() error = %v", err)
	}

	if len(history) != numOps {
		t.Errorf("History count = %d, want %d", len(history), numOps)
	}
}

func TestVerifyAndDecide(t *testing.T) {
	v, _ := New(":memory:", DefaultConfig())

	// Single result - should pass
	passed, reason := v.VerifyAndDecide([]json.RawMessage{json.RawMessage(`{"a":1}`)})
	if !passed {
		t.Errorf("Single result should pass, got: %s", reason)
	}

	// Two matching results
	passed, reason = v.VerifyAndDecide([]json.RawMessage{
		json.RawMessage(`{"a":1}`),
		json.RawMessage(`{"a":1}`),
	})
	if !passed {
		t.Errorf("Two matching results should pass, got: %s", reason)
	}

	// Two mismatching results
	passed, reason = v.VerifyAndDecide([]json.RawMessage{
		json.RawMessage(`{"a":1}`),
		json.RawMessage(`{"a":2}`),
	})
	if passed {
		t.Errorf("Two mismatching results should fail, got: %s", reason)
	}

	// 2 out of 3 match (66% < 100% threshold)
	passed, reason = v.VerifyAndDecide([]json.RawMessage{
		json.RawMessage(`{"a":1}`),
		json.RawMessage(`{"a":1}`),
		json.RawMessage(`{"a":2}`),
	})
	if passed {
		t.Errorf("2/3 match should fail at 100%% threshold, reason: %s", reason)
	}
}

func TestVerifyAndDecideLowerThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AcceptThreshold = 0.5
	v, _ := New(":memory:", cfg)

	// 2 out of 3 match (66% >= 50% threshold)
	passed, reason := v.VerifyAndDecide([]json.RawMessage{
		json.RawMessage(`{"a":1}`),
		json.RawMessage(`{"a":1}`),
		json.RawMessage(`{"a":2}`),
	})
	if !passed {
		t.Errorf("2/3 match should pass at 50%% threshold, reason: %s", reason)
	}
}
