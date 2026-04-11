package marketplace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// NodeCapabilities describes what a node can offer.
type NodeCapabilities struct {
	CPUCores        int    `json:"cpu_cores"`
	CPUArch         string `json:"cpu_arch"`
	RAMGB           int    `json:"ram_gb"`
	GPU             *GPU   `json:"gpu,omitempty"`
	StorageGB       int    `json:"storage_gb"`
	WASMEnabled     bool   `json:"wasm_enabled"`
	DockerAvailable bool   `json:"docker_available"`
}

// GPU describes GPU capabilities.
type GPU struct {
	Present     bool   `json:"present"`
	Model       string `json:"model"`
	VRAMGB      int    `json:"vram_gb"`
	CUDAVersion string `json:"cuda_version,omitempty"`
}

// NodeInfo represents a node in the marketplace.
type NodeInfo struct {
	NodeID       string           `json:"node_id"`
	Multiaddr    string           `json:"multiaddr"`
	Capabilities NodeCapabilities `json:"capabilities"`
	Country      string           `json:"country"`
	City         string           `json:"city"`
	UptimeScore  float64          `json:"uptime_score"`
	PricePerTask int              `json:"price_per_task"` // credits
	Status       string           `json:"status"`         // online, busy, offline
	LastSeen     int64            `json:"last_seen"`
}

// TaskListing represents a task available for execution.
type TaskListing struct {
	TaskID      string          `json:"task_id"`
	Op          string          `json:"op"`
	Input       json.RawMessage `json:"input"`
	Credits     int             `json:"credits"`
	TimeoutSec  int             `json:"timeout_sec"`
	Status      string          `json:"status"` // pending, assigned, completed, failed
	AssignedTo  string          `json:"assigned_to,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	CreatedAt   int64           `json:"created_at"`
	CompletedAt *int64          `json:"completed_at,omitempty"`
}

// Marketplace coordinates task distribution.
type Marketplace struct {
	db     *sql.DB
	mu     sync.RWMutex
	nodeID string
}

// New creates a new marketplace.
func New(dbPath string, nodeID string) (*Marketplace, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS nodes (
			node_id TEXT PRIMARY KEY,
			multiaddr TEXT NOT NULL,
			capabilities TEXT NOT NULL,
			country TEXT,
			city TEXT,
			uptime_score REAL DEFAULT 0.5,
			price_per_task INTEGER DEFAULT 10,
			status TEXT DEFAULT 'offline',
			last_seen INTEGER
		);
		
CREATE TABLE IF NOT EXISTS tasks (
		task_id TEXT PRIMARY KEY,
		op TEXT NOT NULL,
		input TEXT NOT NULL,
		credits INTEGER NOT NULL,
		timeout_sec INTEGER DEFAULT 30,
		status TEXT DEFAULT 'pending',
		assigned_to TEXT,
		result TEXT,
		created_at INTEGER,
		completed_at INTEGER,
		publisher_id TEXT
	);
		
		CREATE TABLE IF NOT EXISTS bids (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			node_addr TEXT NOT NULL,
			credits INTEGER NOT NULL,
			eta_sec INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		);
		
		CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
		CREATE INDEX IF NOT EXISTS idx_nodes_status ON nodes(status);
		CREATE INDEX IF NOT EXISTS idx_bids_task ON bids(task_id);
		CREATE INDEX IF NOT EXISTS idx_bids_node ON bids(node_id);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Marketplace{db: db, nodeID: nodeID}, nil
}

// RegisterNode adds or updates a node in the marketplace.
func (m *Marketplace) RegisterNode(info NodeInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	capJSON, err := json.Marshal(info.Capabilities)
	if err != nil {
		return err
	}

	_, err = m.db.Exec(`
		INSERT INTO nodes (node_id, multiaddr, capabilities, country, city, uptime_score, price_per_task, status, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			multiaddr = excluded.multiaddr,
			capabilities = excluded.capabilities,
			country = excluded.country,
			city = excluded.city,
			uptime_score = excluded.uptime_score,
			price_per_task = excluded.price_per_task,
			status = excluded.status,
			last_seen = excluded.last_seen`,
		info.NodeID, info.Multiaddr, string(capJSON), info.Country, info.City,
		info.UptimeScore, info.PricePerTask, info.Status, time.Now().Unix(),
	)
	return err
}

// GetNodes returns all online nodes.
func (m *Marketplace) GetNodes() ([]NodeInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.Query(`
		SELECT node_id, multiaddr, capabilities, country, city, uptime_score, price_per_task, status, last_seen
		FROM nodes WHERE status = 'online' ORDER BY uptime_score DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []NodeInfo
	for rows.Next() {
		var n NodeInfo
		var capJSON string
		err := rows.Scan(&n.NodeID, &n.Multiaddr, &capJSON, &n.Country, &n.City,
			&n.UptimeScore, &n.PricePerTask, &n.Status, &n.LastSeen)
		if err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(capJSON), &n.Capabilities)
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// FindNodes finds nodes matching requirements.
func (m *Marketplace) FindNodes(minCores, minRAM int, wasmEnabled bool) ([]NodeInfo, error) {
	allNodes, err := m.GetNodes()
	if err != nil {
		return nil, err
	}

	var matching []NodeInfo
	for _, n := range allNodes {
		if n.Capabilities.CPUCores >= minCores && n.Capabilities.RAMGB >= minRAM {
			if wasmEnabled && !n.Capabilities.WASMEnabled {
				continue
			}
			matching = append(matching, n)
		}
	}
	return matching, nil
}

// PublishTask creates a new task listing.
func (m *Marketplace) PublishTask(task TaskListing) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task.CreatedAt = time.Now().Unix()
	_, err := m.db.Exec(`
		INSERT INTO tasks (task_id, op, input, credits, timeout_sec, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		task.TaskID, task.Op, string(task.Input), task.Credits, task.TimeoutSec,
		task.Status, task.CreatedAt,
	)
	return err
}

// Publisher defines the interface for credit operations needed by PublishWithEscrow.
type Publisher interface {
	DeductCredits(nodeID string, amount int) (int, error)
	RewardCredits(nodeID string, amount int) (int, error)
}

// PublishWithEscrow creates a new task listing and atomically deducts credits
// from the publisher as an escrow deposit. Uses BEGIN IMMEDIATE transaction
// for atomicity.
func (m *Marketplace) PublishWithEscrow(task TaskListing, publisherID string, ledger Publisher) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Start atomic transaction
	if _, err := m.db.Exec("BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Deduct credits as escrow from publisher's account
	_, err := ledger.DeductCredits(publisherID, task.Credits)
	if err != nil {
		m.db.Exec("ROLLBACK")
		return fmt.Errorf("escrow deduct: %w", err)
	}

	// Insert task
	task.CreatedAt = time.Now().Unix()
	_, err = m.db.Exec(`
		INSERT INTO tasks (task_id, op, input, credits, timeout_sec, status, created_at, publisher_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		task.TaskID, task.Op, string(task.Input), task.Credits, task.TimeoutSec,
		task.Status, task.CreatedAt, publisherID,
	)
	if err != nil {
		m.db.Exec("ROLLBACK")
		return fmt.Errorf("insert task: %w", err)
	}

	// Commit the transaction
	if _, err := m.db.Exec("COMMIT"); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// ReleaseEscrow releases the escrowed credits back to the publisher.
// Called when a task expires or is cancelled without execution.
func (m *Marketplace) ReleaseEscrow(taskID, publisherID string, ledger Publisher) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get task credits to refund
	var credits int
	err := m.db.QueryRow("SELECT credits FROM tasks WHERE task_id = ?", taskID).Scan(&credits)
	if err != nil {
		return fmt.Errorf("get task credits: %w", err)
	}

	// Refund to publisher
	if _, err := ledger.RewardCredits(publisherID, credits); err != nil {
		return fmt.Errorf("refund: %w", err)
	}

	// Update task status
	_, err = m.db.Exec("UPDATE tasks SET status = 'expired_escrow_released' WHERE task_id = ?", taskID)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}

// AwardEscrow transfers escrowed credits to the winning node after task completion.
func (m *Marketplace) AwardEscrow(taskID, winnerNodeID string, ledger Publisher) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get credits from task
	var credits int
	err := m.db.QueryRow("SELECT credits FROM tasks WHERE task_id = ?", taskID).Scan(&credits)
	if err != nil {
		return fmt.Errorf("get credits: %w", err)
	}

	// Award credits to winner (RewardCredits adds to balance)
	if _, err := ledger.RewardCredits(winnerNodeID, credits); err != nil {
		return fmt.Errorf("award winner: %w", err)
	}

	// Mark task completed
	_, err = m.db.Exec("UPDATE tasks SET status = 'completed_escrow_awarded' WHERE task_id = ?", taskID)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}

// AssignTask assigns a task to a node.
func (m *Marketplace) AssignTask(taskID, nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.Exec(`
		UPDATE tasks SET status = 'assigned', assigned_to = ? WHERE task_id = ?`,
		nodeID, taskID,
	)
	return err
}

// CompleteTask marks a task as completed.
func (m *Marketplace) CompleteTask(taskID string, result json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().Unix()
	_, err := m.db.Exec(`
		UPDATE tasks SET status = 'completed', result = ?, completed_at = ? WHERE task_id = ?`,
		string(result), now, taskID,
	)
	return err
}

// GetTasks returns tasks with optional filters.
func (m *Marketplace) GetTasks(status string) ([]TaskListing, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := "SELECT task_id, op, input, credits, timeout_sec, status, assigned_to, result, created_at, completed_at FROM tasks"
	args := []interface{}{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []TaskListing
	for rows.Next() {
		var t TaskListing
		var inputStr string
		var assignedTo, resultStr sql.NullString
		var completedAt sql.NullInt64
		err := rows.Scan(&t.TaskID, &t.Op, &inputStr, &t.Credits, &t.TimeoutSec,
			&t.Status, &assignedTo, &resultStr, &t.CreatedAt, &completedAt)
		if err != nil {
			return nil, err
		}
		t.Input = json.RawMessage(inputStr)
		if assignedTo.Valid {
			t.AssignedTo = assignedTo.String
		}
		if resultStr.Valid && resultStr.String != "" {
			t.Result = json.RawMessage(resultStr.String)
		}
		if completedAt.Valid {
			ts := completedAt.Int64
			t.CompletedAt = &ts
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// GetTask returns a specific task.
func (m *Marketplace) GetTask(taskID string) (*TaskListing, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var t TaskListing
	var inputStr string
	var assignedTo, resultStr sql.NullString
	var completedAt sql.NullInt64
	err := m.db.QueryRow(`
		SELECT task_id, op, input, credits, timeout_sec, status, assigned_to, result, created_at, completed_at
		FROM tasks WHERE task_id = ?`, taskID).
		Scan(&t.TaskID, &t.Op, &inputStr, &t.Credits, &t.TimeoutSec,
			&t.Status, &assignedTo, &resultStr, &t.CreatedAt, &completedAt)
	if err != nil {
		return nil, err
	}
	t.Input = json.RawMessage(inputStr)
	if assignedTo.Valid {
		t.AssignedTo = assignedTo.String
	}
	if resultStr.Valid && resultStr.String != "" {
		t.Result = json.RawMessage(resultStr.String)
	}
	if completedAt.Valid {
		ts := completedAt.Int64
		t.CompletedAt = &ts
	}
	return &t, nil
}

// UpdateNodeStatus updates a node's status.
func (m *Marketplace) UpdateNodeStatus(nodeID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.Exec(`
		UPDATE nodes SET status = ?, last_seen = ? WHERE node_id = ?`,
		status, time.Now().Unix(), nodeID,
	)
	return err
}

// Close closes the marketplace.
func (m *Marketplace) Close() error {
	return m.db.Close()
}

// PlaceBid places a bid on a task (delegates to BiddingSystem).
func (m *Marketplace) PlaceBid(ctx context.Context, taskID, nodeID, nodeAddr string, credits, etaSec int) (Bid, error) {
	b := &BiddingSystem{db: m.db, nodeID: m.nodeID}
	return b.PlaceBid(ctx, taskID, nodeID, nodeAddr, credits, etaSec)
}

// GetBidsForTask gets all bids for a task.
func (m *Marketplace) GetBidsForTask(taskID string) ([]Bid, error) {
	b := &BiddingSystem{db: m.db, nodeID: m.nodeID}
	return b.GetBidsForTask(taskID)
}

// GetWinningBid gets the winning bid for a task.
func (m *Marketplace) GetWinningBid(taskID string) (*Bid, error) {
	b := &BiddingSystem{db: m.db, nodeID: m.nodeID}
	return b.GetWinningBid(taskID)
}

// AutoMatch auto-matches a task to a bid.
func (m *Marketplace) AutoMatch(taskID string) (*Bid, error) {
	b := &BiddingSystem{db: m.db, nodeID: m.nodeID}
	return b.AutoMatch(taskID)
}
