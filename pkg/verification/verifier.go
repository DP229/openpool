package verification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// VerificationMethod defines how work is verified.
type VerificationMethod int

const (
	MethodNone       VerificationMethod = iota // No verification
	MethodRedundant                            // Run same task on multiple nodes
	MethodCheckpoint                           // Verify partial results at intervals
	MethodVDF                                  // Verifiable Delay Function (future)
)

// VerificationResult stores the outcome of task verification.
type VerificationResult struct {
	TaskID       string             `json:"task_id"`
	Method       VerificationMethod `json:"method"`
	PrimaryNode  string             `json:"primary_node"`
	VerifierNode string             `json:"verifier_node"`
	InputHash    string             `json:"input_hash"`
	OutputHash   string             `json:"output_hash"`
	Match        bool               `json:"match"`
	DurationMs   int64              `json:"duration_ms"`
	Timestamp    int64              `json:"timestamp"`
	Error        string             `json:"error,omitempty"`
}

// Verifier handles task verification.
type Verifier struct {
	db  *sql.DB
	mu  sync.RWMutex
	cfg Config
}

// Config holds verification settings.
type Config struct {
	// Redundant verification: how many nodes verify each task
	RedundantCount int `json:"redundant_count"`
	// Accept threshold: minimum match ratio to accept result
	AcceptThreshold float64 `json:"accept_threshold"`
	// Enable checkpoint verification
	CheckpointVerification bool `json:"checkpoint_verification"`
	// Checkpoint interval (seconds)
	CheckpointInterval int `json:"checkpoint_interval"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		RedundantCount:         2,     // Verify on 2 additional nodes
		AcceptThreshold:        1.0,   // 100% match required
		CheckpointVerification: false, // Disabled for now
		CheckpointInterval:     60,    // 60 seconds
	}
}

// New creates a new Verifier.
func New(dbPath string, cfg Config) (*Verifier, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS verification (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			method INTEGER NOT NULL,
			primary_node TEXT NOT NULL,
			verifier_node TEXT,
			input_hash TEXT NOT NULL,
			output_hash TEXT NOT NULL,
			match INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL,
			timestamp INTEGER NOT NULL,
			error TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_verification_task ON verification(task_id);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	if cfg.RedundantCount == 0 {
		cfg = DefaultConfig()
	}

	return &Verifier{db: db, cfg: cfg}, nil
}

// HashInput computes SHA-256 hash of input data.
func HashInput(data json.RawMessage) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// HashOutput computes SHA-256 hash of output data.
func HashOutput(data json.RawMessage) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// RecordVerification stores a verification result.
func (v *Verifier) RecordVerification(ctx context.Context, res VerificationResult) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	_, err := v.db.Exec(`
		INSERT INTO verification (task_id, method, primary_node, verifier_node, 
			input_hash, output_hash, match, duration_ms, timestamp, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		res.TaskID, res.Method, res.PrimaryNode, res.VerifierNode,
		res.InputHash, res.OutputHash, res.Match, res.DurationMs,
		res.Timestamp, res.Error,
	)
	return err
}

// GetVerificationHistory returns verification results for a task.
func (v *Verifier) GetVerificationHistory(taskID string) ([]VerificationResult, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	rows, err := v.db.Query(`
		SELECT task_id, method, primary_node, verifier_node, input_hash, 
			output_hash, match, duration_ms, timestamp, error
		FROM verification WHERE task_id = ? ORDER BY timestamp DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []VerificationResult
	for rows.Next() {
		var r VerificationResult
		var match int
		err := rows.Scan(&r.TaskID, &r.Method, &r.PrimaryNode, &r.VerifierNode,
			&r.InputHash, &r.OutputHash, &match, &r.DurationMs, &r.Timestamp, &r.Error)
		if err != nil {
			return nil, err
		}
		r.Match = match == 1
		results = append(results, r)
	}
	return results, nil
}

// GetNodeScore returns a node's reliability score.
func (v *Verifier) GetNodeScore(nodeID string) (float64, int, int, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	// Get total verifications and matching results
	var total, matches sql.NullInt64
	err := v.db.QueryRow(`
		SELECT COUNT(*), SUM(match) FROM verification 
		WHERE primary_node = ? OR verifier_node = ?`, nodeID, nodeID).
		Scan(&total, &matches)
	if err != nil {
		return 0, 0, 0, err
	}

	if !total.Valid || total.Int64 == 0 {
		return 0.5, 0, 0, nil // Default score for new nodes
	}

	// Convert NULL matches to 0
	matchCount := 0
	if matches.Valid {
		matchCount = int(matches.Int64)
	}

	score := float64(matchCount) / float64(total.Int64)
	return score, matchCount, int(total.Int64), nil
}

// VerifyResult compares two results for consistency.
func VerifyResult(a, b json.RawMessage) bool {
	// Simple comparison: hash both and compare
	return HashOutput(a) == HashOutput(b)
}

// ShouldVerify returns true if a task should be verified.
func (v *Verifier) ShouldVerify(taskCredits int) bool {
	// Only verify tasks above a credit threshold
	return taskCredits >= v.cfg.RedundantCount*5
}

// GetConfig returns current config.
func (v *Verifier) GetConfig() Config {
	return v.cfg
}

// Close closes the verifier.
func (v *Verifier) Close() error {
	return v.db.Close()
}

// String returns human-readable verification method.
func (m VerificationMethod) String() string {
	switch m {
	case MethodNone:
		return "none"
	case MethodRedundant:
		return "redundant"
	case MethodCheckpoint:
		return "checkpoint"
	case MethodVDF:
		return "vdf"
	default:
		return "unknown"
	}
}

// NodeStats holds verification statistics for a node.
type NodeStats struct {
	NodeID           string  `json:"node_id"`
	ReliabilityScore float64 `json:"reliability_score"`
	TasksVerified    int     `json:"tasks_verified"`
	TasksMatched     int     `json:"tasks_matched"`
	LastVerified     int64   `json:"last_verified"`
}

// GetAllNodeStats returns stats for all nodes.
func (v *Verifier) GetAllNodeStats() ([]NodeStats, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	rows, err := v.db.Query(`
		SELECT primary_node as node_id, 
			COUNT(*) as total,
			SUM(match) as matches,
			MAX(timestamp) as last
		FROM verification 
		GROUP BY primary_node
		ORDER BY total DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []NodeStats
	for rows.Next() {
		var s NodeStats
		var total, matches int
		err := rows.Scan(&s.NodeID, &total, &matches, &s.LastVerified)
		if err != nil {
			return nil, err
		}
		s.TasksVerified = total
		s.TasksMatched = matches
		if total > 0 {
			s.ReliabilityScore = float64(matches) / float64(total)
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// FormatDurationMs formats duration for display.
func FormatDurationMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.2fs", float64(ms)/1000)
}

// NewWithDefaults creates a verifier with default config.
func NewWithDefaults(dbPath string) (*Verifier, error) {
	return New(dbPath, DefaultConfig())
}

// SlashNode deducts credits from a node for bad behavior.
func (v *Verifier) SlashNode(nodeID string, amount int, reason string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	_, err := v.db.Exec(`
		INSERT INTO verification (task_id, method, primary_node, verifier_node,
			input_hash, output_hash, match, duration_ms, timestamp, error)
		VALUES ('slash-'||strftime('%s','now'), 0, ?, 'system', '', '', 0, 0, ?, ?)`,
		nodeID, reason, time.Now().Unix(),
	)
	if err != nil {
		return err
	}

	_, err = v.db.Exec(`
		UPDATE ledger SET credits = MAX(0, credits - ?),
			tasks_failed = tasks_failed + 1,
			updated_at = ?
		WHERE node_id = ?`,
		amount, time.Now().Unix(), nodeID,
	)
	return err
}

// RewardNode adds credits to a node for good verification results.
func (v *Verifier) RewardNode(nodeID string, amount int) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	_, err := v.db.Exec(`
		UPDATE ledger SET credits = credits + ?,
			tasks_completed = tasks_completed + 1,
			updated_at = ?
		WHERE node_id = ?`,
		amount, time.Now().Unix(), nodeID,
	)
	return err
}

// GetSlashingHistory returns all slashing events for a node.
func (v *Verifier) GetSlashingHistory(nodeID string) ([]VerificationResult, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	rows, err := v.db.Query(`
		SELECT task_id, method, primary_node, verifier_node, input_hash,
			output_hash, match, duration_ms, timestamp, error
		FROM verification WHERE primary_node = ? AND task_id LIKE 'slash-%'
		ORDER BY timestamp DESC`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []VerificationResult
	for rows.Next() {
		var r VerificationResult
		var match int
		err := rows.Scan(&r.TaskID, &r.Method, &r.PrimaryNode, &r.VerifierNode,
			&r.InputHash, &r.OutputHash, &match, &r.DurationMs, &r.Timestamp, &r.Error)
		if err != nil {
			return nil, err
		}
		r.Match = match == 1
		results = append(results, r)
	}
	return results, nil
}

// VerifyAndDecide compares multiple results and determines if the task passed verification.
func (v *Verifier) VerifyAndDecide(results []json.RawMessage) (bool, string) {
	if len(results) < 2 {
		return true, "single result - no verification needed"
	}

	matchCount := 1
	for i := 1; i < len(results); i++ {
		if VerifyResult(results[0], results[i]) {
			matchCount++
		}
	}

	ratio := float64(matchCount) / float64(len(results))
	if ratio >= v.cfg.AcceptThreshold {
		return true, fmt.Sprintf("%d/%d results matched (%.0f%%)", matchCount, len(results), ratio*100)
	}
	return false, fmt.Sprintf("only %d/%d results matched (%.0f%% < %.0f%% threshold)",
		matchCount, len(results), ratio*100, v.cfg.AcceptThreshold*100)
}
