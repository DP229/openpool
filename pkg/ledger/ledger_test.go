package ledger

import (
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
				f, err := os.CreateTemp("", "ledger_test_*.db")
				if err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				path = f.Name()
				f.Close()
				defer os.Remove(path)
			}

			l, err := New(path)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if l == nil && !tt.wantErr {
				t.Error("New() returned nil ledger")
				return
			}
			if l != nil {
				l.db.Close()
			}
		})
	}
}

func TestAddCredits(t *testing.T) {
	l, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create ledger: %v", err)
	}
	defer l.db.Close()

	tests := []struct {
		name     string
		nodeID   string
		amount   int
		expected int
	}{
		{"initial credits", "node1", 100, 100},
		{"add more credits", "node1", 50, 150},
		{"subtract credits", "node1", -30, 120},
		{"new node", "node2", 200, 200},
		{"negative balance", "node1", -200, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			balance := l.AddCredits(tt.nodeID, tt.amount)
			if balance != tt.expected {
				t.Errorf("AddCredits(%s, %d) = %d, want %d", tt.nodeID, tt.amount, balance, tt.expected)
			}
		})
	}
}

func TestGetCredits(t *testing.T) {
	l, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create ledger: %v", err)
	}
	defer l.db.Close()

	l.AddCredits("node1", 100)
	l.AddCredits("node2", 200)

	tests := []struct {
		name     string
		nodeID   string
		expected int
	}{
		{"existing node1", "node1", 100},
		{"existing node2", "node2", 200},
		{"non-existent node", "node3", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			balance := l.GetCredits(tt.nodeID)
			if balance != tt.expected {
				t.Errorf("GetCredits(%s) = %d, want %d", tt.nodeID, balance, tt.expected)
			}
		})
	}
}

func TestGetAll(t *testing.T) {
	l, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create ledger: %v", err)
	}
	defer l.db.Close()

	l.AddCredits("node1", 300)
	l.AddCredits("node2", 200)
	l.AddCredits("node3", 100)

	entries := l.GetAll()
	if len(entries) != 3 {
		t.Errorf("GetAll() returned %d entries, want 3", len(entries))
		return
	}

	if entries[0].NodeID != "node1" || entries[0].Credits != 300 {
		t.Errorf("entries sorted incorrectly: first entry = %+v", entries[0])
	}
}

func TestRecordTask(t *testing.T) {
	l, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create ledger: %v", err)
	}
	defer l.db.Close()

	l.AddCredits("node1", 100)
	l.RecordTask("node1", true)

	entries := l.GetAll()
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
		return
	}

	if entries[0].TasksCompleted != 1 {
		t.Errorf("TasksCompleted = %d, want 1", entries[0].TasksCompleted)
	}

	l.RecordTask("node1", false)

	entries = l.GetAll()
	if entries[0].TasksFailed != 1 {
		t.Errorf("TasksFailed = %d, want 1", entries[0].TasksFailed)
	}
}

func TestConcurrency(t *testing.T) {
	l, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create ledger: %v", err)
	}
	defer l.db.Close()

	done := make(chan bool)
	const numOps = 100
	const numGoroutines = 10

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			for i := 0; i < numOps; i++ {
				l.AddCredits("concurrent_node", 1)
			}
			done <- true
		}(g)
	}

	for g := 0; g < numGoroutines; g++ {
		<-done
	}

	expected := numOps * numGoroutines
	balance := l.GetCredits("concurrent_node")
	if balance != expected {
		t.Errorf("concurrent balance = %d, want %d", balance, expected)
	}
}