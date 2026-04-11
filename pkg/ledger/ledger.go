package ledger

import (
	"database/sql"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Ledger tracks credits per node.
type Ledger struct {
	db *sql.DB
	mu sync.RWMutex
}

// New creates or opens a SQLite ledger.
func New(path string) (*Ledger, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for concurrent read/write safety
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, err
	}
	// Prevent writer starvation with busy timeout
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS ledger (
			node_id TEXT PRIMARY KEY,
			credits INTEGER NOT NULL DEFAULT 0,
			tasks_completed INTEGER NOT NULL DEFAULT 0,
			tasks_failed INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id TEXT NOT NULL,
			amount INTEGER NOT NULL,
			reason TEXT,
			ts INTEGER NOT NULL
		);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Ledger{db: db}, nil
}

// AddCredits adds (or subtracts if negative) credits. Returns new balance.
func (l *Ledger) AddCredits(nodeID string, amount int) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	var balance int
	row := l.db.QueryRow("SELECT credits FROM ledger WHERE node_id = ?", nodeID)
	if err := row.Scan(&balance); err == sql.ErrNoRows {
		balance = 0
	}

	balance += amount
	if balance < 0 {
		balance = 0
	}

	ts := time.Now().Unix()
	l.db.Exec(`INSERT INTO ledger (node_id, credits, updated_at)
		VALUES (?, ?, ?) ON CONFLICT(node_id) DO UPDATE SET credits=?, updated_at=?`,
		nodeID, balance, ts, balance, ts)

	l.db.Exec("INSERT INTO history (node_id, amount, ts) VALUES (?, ?, ?)",
		nodeID, amount, ts)

	return balance
}

// GetCredits returns the current balance for a node.
func (l *Ledger) GetCredits(nodeID string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var balance int
	row := l.db.QueryRow("SELECT credits FROM ledger WHERE node_id = ?", nodeID)
	if err := row.Scan(&balance); err == sql.ErrNoRows {
		return 0
	}
	return balance
}

// GetAll returns all ledger entries.
func (l *Ledger) GetAll() []LedgerEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	rows, err := l.db.Query("SELECT node_id, credits, tasks_completed, tasks_failed, updated_at FROM ledger ORDER BY credits DESC")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var entries []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		rows.Scan(&e.NodeID, &e.Credits, &e.TasksCompleted, &e.TasksFailed, &e.UpdatedAt)
		entries = append(entries, e)
	}
	return entries
}

// DeductCredits subtracts credits from a node, clamping to zero.
// Returns the new balance. If the node does not exist, it is created with zero credits first.
func (l *Ledger) DeductCredits(nodeID string, amount int) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.db.Exec("BEGIN IMMEDIATE"); err != nil {
		return 0, err
	}

	var balance int
	err := l.db.QueryRow("SELECT credits FROM ledger WHERE node_id = ?", nodeID).Scan(&balance)
	if err != nil {
		if err == sql.ErrNoRows {
			balance = 0
		} else {
			l.db.Exec("ROLLBACK")
			return 0, err
		}
	}

	balance -= amount
	if balance < 0 {
		balance = 0
	}

	ts := time.Now().Unix()
	_, err = l.db.Exec(`INSERT INTO ledger (node_id, credits, tasks_failed, updated_at)
		VALUES (?, ?, 1, ?) ON CONFLICT(node_id) DO UPDATE SET credits=?, tasks_failed=tasks_failed+1, updated_at=?`,
		nodeID, balance, ts, balance, ts)
	if err != nil {
		l.db.Exec("ROLLBACK")
		return 0, err
	}

	_, err = l.db.Exec("INSERT INTO history (node_id, amount, reason, ts) VALUES (?, ?, ?, ?)",
		nodeID, -amount, "slash", ts)
	if err != nil {
		l.db.Exec("ROLLBACK")
		return 0, err
	}

	if _, err := l.db.Exec("COMMIT"); err != nil {
		return 0, err
	}

	return balance, nil
}

// RewardCredits adds credits to a node and increments tasks_completed.
// Returns the new balance.
func (l *Ledger) RewardCredits(nodeID string, amount int) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.db.Exec("BEGIN IMMEDIATE"); err != nil {
		return 0, err
	}

	var balance int
	err := l.db.QueryRow("SELECT credits FROM ledger WHERE node_id = ?", nodeID).Scan(&balance)
	if err != nil {
		if err == sql.ErrNoRows {
			balance = 0
		} else {
			l.db.Exec("ROLLBACK")
			return 0, err
		}
	}

	balance += amount
	ts := time.Now().Unix()
	_, err = l.db.Exec(`INSERT INTO ledger (node_id, credits, tasks_completed, updated_at)
		VALUES (?, ?, 1, ?) ON CONFLICT(node_id) DO UPDATE SET credits=?, tasks_completed=tasks_completed+1, updated_at=?`,
		nodeID, balance, ts, balance, ts)
	if err != nil {
		l.db.Exec("ROLLBACK")
		return 0, err
	}

	_, err = l.db.Exec("INSERT INTO history (node_id, amount, reason, ts) VALUES (?, ?, ?, ?)",
		nodeID, amount, "reward", ts)
	if err != nil {
		l.db.Exec("ROLLBACK")
		return 0, err
	}

	if _, err := l.db.Exec("COMMIT"); err != nil {
		return 0, err
	}

	return balance, nil
}

// RecordTask records a task completion for a node.
func (l *Ledger) RecordTask(nodeID string, success bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	field := "tasks_completed"
	if !success {
		field = "tasks_failed"
	}
	ts := time.Now().Unix()
	l.db.Exec("UPDATE ledger SET "+field+" = "+field+"+1, updated_at = ? WHERE node_id = ?",
		ts, nodeID)
}

type LedgerEntry struct {
	NodeID         string `json:"node_id"`
	Credits        int    `json:"credits"`
	TasksCompleted int    `json:"tasks_completed"`
	TasksFailed    int    `json:"tasks_failed"`
	UpdatedAt      int64  `json:"updated_at"`
}
