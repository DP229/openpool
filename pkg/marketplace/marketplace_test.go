package marketplace

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
				f, err := os.CreateTemp("", "marketplace_test_*.db")
				if err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				path = f.Name()
				f.Close()
				defer os.Remove(path)
			}

			m, err := New(path, "test-node")
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if m != nil {
				m.Close()
			}
		})
	}
}

func TestRegisterNode(t *testing.T) {
	m, err := New(":memory:", "test-node")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer m.Close()

	node := NodeInfo{
		NodeID:    "node-1",
		Multiaddr: "/ip4/192.168.1.1/tcp/9000/p2p/QmNode1",
		Capabilities: NodeCapabilities{
			CPUCores:    8,
			CPUArch:     "x86_64",
			RAMGB:       16,
			WASMEnabled: true,
		},
		PricePerTask: 10,
		Status:       "online",
	}

	if err := m.RegisterNode(node); err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}

	nodes, err := m.GetNodes()
	if err != nil {
		t.Fatalf("GetNodes() error = %v", err)
	}

	if len(nodes) != 1 {
		t.Errorf("GetNodes() returned %d nodes, want 1", len(nodes))
		return
	}

	if nodes[0].NodeID != "node-1" {
		t.Errorf("nodes[0].NodeID = %s, want node-1", nodes[0].NodeID)
	}
	if nodes[0].PricePerTask != 10 {
		t.Errorf("nodes[0].PricePerTask = %d, want 10", nodes[0].PricePerTask)
	}
}

func TestGetNodes(t *testing.T) {
	t.Cleanup(func() {})

	m, _ := New(":memory:", "test-node")
	defer m.Close()

	nodes, err := m.GetNodes()
	if err != nil {
		t.Fatalf("GetNodes() error = %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("GetNodes() returned %d nodes on empty DB, want 0", len(nodes))
	}

	m.RegisterNode(NodeInfo{NodeID: "node-1", Multiaddr: "addr1", Status: "online"})
	m.RegisterNode(NodeInfo{NodeID: "node-2", Multiaddr: "addr2", Status: "offline"})

	nodes, _ = m.GetNodes()
	if len(nodes) != 1 {
		t.Errorf("GetNodes() returned %d nodes, want 1 (only online)", len(nodes))
	}
}

func TestFindNodes(t *testing.T) {
	m, _ := New(":memory:", "test-node")
	defer m.Close()

	m.RegisterNode(NodeInfo{
		NodeID: "node-1",
		Capabilities: NodeCapabilities{
			CPUCores:    4,
			RAMGB:       8,
			WASMEnabled: true,
		},
		Status: "online",
	})

	m.RegisterNode(NodeInfo{
		NodeID: "node-2",
		Capabilities: NodeCapabilities{
			CPUCores:    2,
			RAMGB:       4,
			WASMEnabled: false,
		},
		Status: "online",
	})

	m.RegisterNode(NodeInfo{
		NodeID: "node-3",
		Capabilities: NodeCapabilities{
			CPUCores:    16,
			RAMGB:       64,
			WASMEnabled: true,
		},
		Status: "offline",
	})

	tests := []struct {
		name        string
		minCores    int
		minRAM      int
		wasmEnabled bool
		wantCount   int
	}{
		{"no requirements", 0, 0, false, 2},
		{"min 4 cores", 4, 0, false, 1},
		{"min 8GB RAM", 0, 8, false, 1},
		{"wasm enabled", 0, 0, true, 1},
		{"min specs", 4, 8, true, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := m.FindNodes(tt.minCores, tt.minRAM, tt.wasmEnabled)
			if err != nil {
				t.Fatalf("FindNodes() error = %v", err)
			}
			if len(nodes) != tt.wantCount {
				t.Errorf("FindNodes() returned %d nodes, want %d", len(nodes), tt.wantCount)
			}
		})
	}
}

func TestPublishTask(t *testing.T) {
	m, _ := New(":memory:", "test-node")
	defer m.Close()

	task := TaskListing{
		TaskID:     "task-1",
		Op:         "fib",
		Input:      json.RawMessage(`{"arg":30}`),
		Credits:    50,
		TimeoutSec: 60,
		Status:     "pending",
	}

	if err := m.PublishTask(task); err != nil {
		t.Fatalf("PublishTask() error = %v", err)
	}

	tasks, err := m.GetTask("task-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}

	if tasks.TaskID != "task-1" {
		t.Errorf("tasks.TaskID = %s, want task-1", tasks.TaskID)
	}
	if tasks.Op != "fib" {
		t.Errorf("tasks.Op = %s, want fib", tasks.Op)
	}
}

func TestAssignTask(t *testing.T) {
	m, _ := New(":memory:", "test-node")
	defer m.Close()

	m.PublishTask(TaskListing{
		TaskID: "task-1",
		Op:     "fib",
		Input:  json.RawMessage(`{}`),
		Status: "pending",
	})

	if err := m.AssignTask("task-1", "node-1"); err != nil {
		t.Fatalf("AssignTask() error = %v", err)
	}

	task, err := m.GetTask("task-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}

	if task.Status != "assigned" {
		t.Errorf("task.Status = %s, want assigned", task.Status)
	}
	if task.AssignedTo != "node-1" {
		t.Errorf("task.AssignedTo = %s, want node-1", task.AssignedTo)
	}
}

func TestCompleteTask(t *testing.T) {
	m, _ := New(":memory:", "test-node")
	defer m.Close()

	m.PublishTask(TaskListing{
		TaskID: "task-1",
		Op:     "fib",
		Input:  json.RawMessage(`{}`),
		Status: "pending",
	})

	result := json.RawMessage(`{"result":832040}`)
	if err := m.CompleteTask("task-1", result); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}

	task, err := m.GetTask("task-1")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}

	if task.Status != "completed" {
		t.Errorf("task.Status = %s, want completed", task.Status)
	}
	if string(task.Result) != `{"result":832040}` {
		t.Errorf("task.Result = %s, want {\"result\":832040}", string(task.Result))
	}
}

func TestGetTasks(t *testing.T) {
	m, _ := New(":memory:", "test-node")
	defer m.Close()

	m.PublishTask(TaskListing{TaskID: "task-1", Op: "fib", Status: "pending"})
	m.PublishTask(TaskListing{TaskID: "task-2", Op: "fib", Status: "pending"})
	m.AssignTask("task-1", "node-1")

	tests := []struct {
		name      string
		status    string
		wantCount int
	}{
		{"all", "", 2},
		{"pending only", "pending", 1},
		{"assigned", "assigned", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks, err := m.GetTasks(tt.status)
			if err != nil {
				t.Fatalf("GetTasks() error = %v", err)
			}
			if len(tasks) != tt.wantCount {
				t.Errorf("GetTasks() returned %d tasks, want %d", len(tasks), tt.wantCount)
			}
		})
	}
}

func TestUpdateNodeStatus(t *testing.T) {
	m, _ := New(":memory:", "test-node")
	defer m.Close()

	m.RegisterNode(NodeInfo{NodeID: "node-1", Status: "online"})
	m.UpdateNodeStatus("node-1", "busy")

	nodes, _ := m.GetNodes()
	if len(nodes) != 0 {
		t.Errorf("GetNodes() should return 0 after setting status to busy, got %d", len(nodes))
	}
}

func TestNodeCapabilities(t *testing.T) {
	caps := NodeCapabilities{
		CPUCores:        8,
		CPUArch:         "x86_64",
		RAMGB:           32,
		WASMEnabled:     true,
		DockerAvailable: true,
		GPU: &GPU{
			Present:     true,
			Model:       "NVIDIA RTX 4090",
			VRAMGB:      24,
			CUDAVersion: "12.1",
		},
	}

	data, err := json.Marshal(caps)
	if err != nil {
		t.Fatalf("Failed to marshal NodeCapabilities: %v", err)
	}

	var decoded NodeCapabilities
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal NodeCapabilities: %v", err)
	}

	if decoded.CPUCores != caps.CPUCores {
		t.Errorf("CPUCores mismatch: got %d, want %d", decoded.CPUCores, caps.CPUCores)
	}
	if decoded.GPU == nil || decoded.GPU.Model != caps.GPU.Model {
		t.Errorf("GPU model mismatch")
	}
}

func TestBid(t *testing.T) {
	m, _ := New(":memory:", "test-node")
	defer m.Close()

	ctx := context.Background()
	bid, err := m.PlaceBid(ctx, "task-1", "node-1", "/ip4/192.168.1.1/tcp/9000/p2p/node1", 50, 30)
	if err != nil {
		t.Fatalf("PlaceBid() error = %v", err)
	}

	if bid.TaskID != "task-1" {
		t.Errorf("bid.TaskID = %s, want task-1", bid.TaskID)
	}
	if bid.Credits != 50 {
		t.Errorf("bid.Credits = %d, want 50", bid.Credits)
	}

	bids, err := m.GetBidsForTask("task-1")
	if err != nil {
		t.Fatalf("GetBidsForTask() error = %v", err)
	}
	if len(bids) != 1 {
		t.Errorf("GetBidsForTask() returned %d bids, want 1", len(bids))
	}
}

func TestWinningBid(t *testing.T) {
	m, _ := New(":memory:", "test-node")
	defer m.Close()

	ctx := context.Background()
	m.PlaceBid(ctx, "task-1", "node-1", "addr1", 100, 60)
	m.PlaceBid(ctx, "task-1", "node-2", "addr2", 50, 30)
	m.PlaceBid(ctx, "task-1", "node-3", "addr3", 75, 45)

	winBid, err := m.GetWinningBid("task-1")
	if err != nil {
		t.Fatalf("GetWinningBid() error = %v", err)
	}

	if winBid.Credits != 50 {
		t.Errorf("Winning bid credits = %d, want 50 (lowest price)", winBid.Credits)
	}
	if winBid.NodeID != "node-2" {
		t.Errorf("Winning bid node = %s, want node-2", winBid.NodeID)
	}
}

func TestAutoMatch(t *testing.T) {
	m, _ := New(":memory:", "test-node")
	defer m.Close()

	ctx := context.Background()
	m.PlaceBid(ctx, "task-1", "node-1", "addr1", 100, 60)
	m.PlaceBid(ctx, "task-1", "node-2", "addr2", 50, 30)

	bid, err := m.AutoMatch("task-1")
	if err != nil {
		t.Fatalf("AutoMatch() error = %v", err)
	}

	if bid.Credits != 50 {
		t.Errorf("AutoMatch() credits = %d, want 50", bid.Credits)
	}
}

func TestBidNoBids(t *testing.T) {
	m, _ := New(":memory:", "test-node")
	defer m.Close()

	_, err := m.GetWinningBid("nonexistent-task")
	if err == nil {
		t.Error("GetWinningBid() should return error when no bids")
	}
}
