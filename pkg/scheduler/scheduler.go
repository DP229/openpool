package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dp229/openpool/pkg/p2p"
	"github.com/dp229/openpool/pkg/resilience"
	"github.com/libp2p/go-libp2p/core/peer"
)

// ── Configuration ─────────────────────────────────────────────────────────────────

const (
	DefaultMaxRetries     = 3
	DefaultMaxInFlight    = 64
	DefaultRetryBaseMs    = 200
	DefaultRetryMaxMs     = 10000
	DefaultRetryMult      = 2.0
	DefaultCollectTimeout = 5 * time.Minute
)

// SchedulerConfig tunes the fault-tolerance and concurrency behaviour.
type SchedulerConfig struct {
	MaxRetries      int           // max re-queue attempts per chunk (default 3)
	MaxInFlight     int           // semaphore cap for concurrent libp2p streams (default 64)
	RetryBaseMs     int           // initial backoff in milliseconds (default 200)
	RetryMaxMs      int           // backoff ceiling in milliseconds (default 10000)
	RetryMultiplier float64       // backoff multiplier (default 2.0)
	CollectTimeout  time.Duration // wall-clock timeout for the entire job (default 5min)
}

func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		MaxRetries:      DefaultMaxRetries,
		MaxInFlight:     DefaultMaxInFlight,
		RetryBaseMs:     DefaultRetryBaseMs,
		RetryMaxMs:      DefaultRetryMaxMs,
		RetryMultiplier: DefaultRetryMult,
		CollectTimeout:  DefaultCollectTimeout,
	}
}

// ── Legacy types (kept for backward compatibility) ────────────────────────────────

type Chunk struct {
	ID      string          `json:"id"`
	TaskID  string          `json:"task_id"`
	Index   int             `json:"index"`
	Params  json.RawMessage `json:"params"`
	Credits int             `json:"credits"`
	Timeout int             `json:"timeout"`
}

type ChunkResult struct {
	ChunkID    string          `json:"chunk_id"`
	Index      int             `json:"index"`
	Success    bool            `json:"success"`
	Data       json.RawMessage `json:"data,omitempty"`
	Error      string          `json:"error,omitempty"`
	DurationMs int64           `json:"duration_ms"`
}

type MapReduce struct {
	Split  func(task *p2p.Task) ([]Chunk, error)
	Reduce func(results []ChunkResult) (json.RawMessage, error)
}

// ── Checkpoint Store ──────────────────────────────────────────────────────────────

// CheckpointStore persists partial DAG/Job progress so a scheduler restart
// can resume from where it left off. It is backed by the SQLite ledger.
type CheckpointStore struct {
	db checkpointer
	mu sync.RWMutex
}

// Checkpoint captures the resumable state of a distributed job.
type Checkpoint struct {
	JobID     string            `json:"job_id"`
	Completed map[string]bool   `json:"completed"`
	Results   map[string][]byte `json:"results"`
	CreatedAt int64             `json:"created_at"`
	UpdatedAt int64             `json:"updated_at"`
}

type checkpointer interface {
	CheckpointSave(cp *Checkpoint) error
	CheckpointLoad(jobID string) (*Checkpoint, error)
	CheckpointDelete(jobID string) error
}

// NewCheckpointStore creates a checkpoint store backed by any checkpointer.
func NewCheckpointStore(db checkpointer) *CheckpointStore {
	return &CheckpointStore{db: db}
}

func (cs *CheckpointStore) Save(cp *Checkpoint) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.db.CheckpointSave(cp)
}

func (cs *CheckpointStore) Load(jobID string) (*Checkpoint, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.db.CheckpointLoad(jobID)
}

func (cs *CheckpointStore) Delete(jobID string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.db.CheckpointDelete(jobID)
}

// MemoryCheckpointer is an in-memory checkpointer for testing.
type MemoryCheckpointer struct {
	mu   sync.RWMutex
	data map[string]*Checkpoint
}

func NewMemoryCheckpointer() *MemoryCheckpointer {
	return &MemoryCheckpointer{data: make(map[string]*Checkpoint)}
}

func (m *MemoryCheckpointer) CheckpointSave(cp *Checkpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[cp.JobID] = cp
	return nil
}

func (m *MemoryCheckpointer) CheckpointLoad(jobID string) (*Checkpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp, ok := m.data[jobID]
	if !ok {
		return nil, fmt.Errorf("checkpoint not found: %s", jobID)
	}
	return cp, nil
}

func (m *MemoryCheckpointer) CheckpointDelete(jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, jobID)
	return nil
}

// ── Scheduler ──────────────────────────────────────────────────────────────────────

type Scheduler struct {
	node    *p2p.Node
	nodeID  string
	mu      sync.RWMutex
	pending map[string]chan ChunkResult

	config     SchedulerConfig
	sem        chan struct{} // stream backpressure semaphore
	checkpoint *CheckpointStore
}

func New(node *p2p.Node, nodeID string) *Scheduler {
	return NewWithConfig(node, nodeID, DefaultSchedulerConfig())
}

func NewWithConfig(node *p2p.Node, nodeID string, cfg SchedulerConfig) *Scheduler {
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = DefaultMaxInFlight
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.RetryBaseMs <= 0 {
		cfg.RetryBaseMs = DefaultRetryBaseMs
	}
	if cfg.RetryMaxMs <= 0 {
		cfg.RetryMaxMs = DefaultRetryMaxMs
	}
	if cfg.RetryMultiplier <= 0 {
		cfg.RetryMultiplier = DefaultRetryMult
	}
	if cfg.CollectTimeout <= 0 {
		cfg.CollectTimeout = DefaultCollectTimeout
	}
	return &Scheduler{
		node:       node,
		nodeID:     nodeID,
		pending:    make(map[string]chan ChunkResult),
		config:     cfg,
		sem:        make(chan struct{}, cfg.MaxInFlight),
		checkpoint: NewCheckpointStore(NewMemoryCheckpointer()),
	}
}

func (s *Scheduler) SetCheckpointStore(store *CheckpointStore) {
	s.checkpoint = store
}

func (s *Scheduler) Config() SchedulerConfig {
	return s.config
}

// ── SubmitChunked (legacy, now with retries + backpressure) ───────────────────────

func (s *Scheduler) SubmitChunked(ctx context.Context, task *p2p.Task, mr MapReduce) (*p2p.Task, error) {
	chunks, err := mr.Split(task)
	if err != nil {
		return nil, fmt.Errorf("split: %w", err)
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks produced")
	}
	log.Printf("[%s] Split task %s into %d chunks", s.nodeID[:6], task.ID, len(chunks))

	peers, err := s.discoverPeers(ctx, len(chunks))
	if err != nil {
		log.Printf("[%s] peer discovery: %v — using connected peers", s.nodeID[:6], err)
		peers = s.connectedPeers()
	}
	peers = s.filterHealthyPeers(peers)
	if len(peers) == 0 {
		return nil, fmt.Errorf("no peers available")
	}

	localPeers, globalPeers := s.getLANFirstPeers(peers)

	// Hardware-aware filtering: Prevent Tier1_Native tasks from running on browser peers
	hwReq := task.HardwareRequirement
	if hwReq == "Tier1_Native" {
		// Filter out browser peers
		var nativeLocal, nativeGlobal []string
		for _, p := range localPeers {
			if !s.isBrowserPeer(p) {
				nativeLocal = append(nativeLocal, p)
			}
		}
		for _, p := range globalPeers {
			if !s.isBrowserPeer(p) {
				nativeGlobal = append(nativeGlobal, p)
			}
		}
		localPeers = nativeLocal
		globalPeers = nativeGlobal
		log.Printf("[%s] Hardware filter: removed browser peers for Tier1_Native task", s.nodeID[:6])
	}

	if len(localPeers) == 0 && len(globalPeers) == 0 {
		return nil, fmt.Errorf("no compatible peers available for hardware requirement: %s", hwReq)
	}

	strategy := task.Strategy
	if strategy == "" {
		strategy = p2p.StrategyHybridAuto
	}

	log.Printf("[%s] Scheduling %d chunks with %s (local:%d global:%d)",
		s.nodeID[:6], len(chunks), strategy, len(localPeers), len(globalPeers))

	resultCh := make(chan ChunkResult, len(chunks))

	for i, chunk := range chunks {
		var peerID string
		var pool string

		switch strategy {
		case p2p.StrategyLANOnly:
			if len(localPeers) == 0 {
				return nil, fmt.Errorf("LANOnly: no local peers")
			}
			peerID = localPeers[i%len(localPeers)]
			pool = "LAN"
		case p2p.StrategyWANOnly:
			if len(globalPeers) == 0 {
				return nil, fmt.Errorf("WANOnly: no global peers")
			}
			peerID = globalPeers[i%len(globalPeers)]
			pool = "WAN"
		default: // HybridAuto
			if i < len(localPeers) && len(localPeers) > 0 {
				peerID = localPeers[i%len(localPeers)]
				pool = "LAN"
			} else if len(globalPeers) > 0 {
				peerID = globalPeers[(i-len(localPeers))%len(globalPeers)]
				pool = "WAN"
			} else {
				peerID = localPeers[i%len(localPeers)]
				pool = "LAN"
			}
		}

		log.Printf("[%s] Chunk %d -> %s", s.nodeID[:6], i, pool)

		chunkTask := &p2p.Task{
			ID:         chunk.ID,
			Code:       task.Code,
			Lang:       task.Lang,
			Params:     chunk.Params,
			TimeoutSec: chunk.Timeout,
			Credits:    chunk.Credits,
			Strategy:   strategy,
			State:      "pending",
			CreatedAt:  time.Now(),
		}

		s.mu.Lock()
		s.pending[chunk.ID] = resultCh
		s.mu.Unlock()

		go func(c Chunk, pt *p2p.Task, initialPeer string) {
			s.submitWithRetry(ctx, c, pt, initialPeer, peers, resultCh)
		}(chunk, chunkTask, peerID)
	}

	var results []ChunkResult
	timeout := time.After(s.config.CollectTimeout)
	for range chunks {
		select {
		case r := <-resultCh:
			results = append(results, r)
			log.Printf("[%s] Chunk %s result: success=%v", s.nodeID[:6], r.ChunkID, r.Success)
		case <-timeout:
			log.Printf("[%s] Timeout waiting for chunk results", s.nodeID[:6])
			break
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	log.Printf("[%s] All chunks collected (%d/%d succeeded)", s.nodeID[:6],
		countSuccess(results), len(chunks))

	finalData, err := mr.Reduce(results)
	if err != nil {
		return nil, fmt.Errorf("reduce: %w", err)
	}

	task.Result = finalData
	task.State = "completed"
	task.CompletedAt = timePtr(time.Now())

	return task, nil
}

// submitWithRetry attempts to submit a chunk to a peer. On failure it
// re-queues to a different healthy peer with exponential backoff, up to
// MaxRetries times. The semaphore caps concurrent libp2p streams.
func (s *Scheduler) submitWithRetry(ctx context.Context, c Chunk, pt *p2p.Task, initialPeer string, allPeers []string, resultCh chan<- ChunkResult) {
	peerIdx := -1
	for i, p := range allPeers {
		if p == initialPeer {
			peerIdx = i
			break
		}
	}
	if peerIdx == -1 {
		peerIdx = 0
	}

	for attempt := 0; attempt <= s.config.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(s.config.RetryBaseMs) * time.Duration(1<<(attempt-1)) * time.Millisecond
			if delay > time.Duration(s.config.RetryMaxMs)*time.Millisecond {
				delay = time.Duration(s.config.RetryMaxMs) * time.Millisecond
			}
			log.Printf("[%s] Retry %d/%d for chunk %s, backoff %v", s.nodeID[:6], attempt, s.config.MaxRetries, c.ID, delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				resultCh <- ChunkResult{ChunkID: c.ID, Index: c.Index, Success: false, Error: ctx.Err().Error()}
				s.mu.Lock()
				delete(s.pending, c.ID)
				s.mu.Unlock()
				return
			}

			healthyPeers := s.filterHealthyPeers(allPeers)
			if len(healthyPeers) == 0 {
				resultCh <- ChunkResult{ChunkID: c.ID, Index: c.Index, Success: false, Error: "no healthy peers for retry"}
				s.mu.Lock()
				delete(s.pending, c.ID)
				s.mu.Unlock()
				return
			}
			peerIdx = (peerIdx + 1) % len(healthyPeers)
			initialPeer = healthyPeers[peerIdx]
			pt.ID = c.ID
		}

		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			resultCh <- ChunkResult{ChunkID: c.ID, Index: c.Index, Success: false, Error: ctx.Err().Error()}
			s.mu.Lock()
			delete(s.pending, c.ID)
			s.mu.Unlock()
			return
		}

		start := time.Now()
		log.Printf("[%s] Submitting chunk %s (index=%d) to %s (attempt=%d)", s.nodeID[:6], c.ID, c.Index, initialPeer[:min(16, len(initialPeer))], attempt)

		err := s.node.SubmitTask(ctx, initialPeer, pt)

		<-s.sem // release semaphore

		if err == nil && pt.State == "completed" {
			resultCh <- ChunkResult{
				ChunkID: c.ID, Index: c.Index, Success: true,
				Data: pt.Result, DurationMs: time.Since(start).Milliseconds(),
			}
			s.mu.Lock()
			delete(s.pending, c.ID)
			s.mu.Unlock()
			return
		}

		errMsg := ""
		if err != nil {
			errMsg = fmt.Sprintf("submit: %v", err)
		} else {
			errMsg = pt.Error
		}

		if attempt >= s.config.MaxRetries {
			log.Printf("[%s] Chunk %s exhausted retries: %s", s.nodeID[:6], c.ID, errMsg)
			resultCh <- ChunkResult{
				ChunkID: c.ID, Index: c.Index, Success: false,
				Error: errMsg, DurationMs: time.Since(start).Milliseconds(),
			}
			s.mu.Lock()
			delete(s.pending, c.ID)
			s.mu.Unlock()
			return
		}

		log.Printf("[%s] Chunk %s attempt %d failed: %s — retrying", s.nodeID[:6], c.ID, attempt, errMsg)
	}
}

// ── Checkpoint Save/Load ───────────────────────────────────────────────────────────

// SaveCheckpoint writes partial DAG progress so a scheduler restart can resume.
func (s *Scheduler) SaveCheckpoint(cp *Checkpoint) error {
	return s.checkpoint.Save(cp)
}

// LoadCheckpoint restores previously saved job state.
func (s *Scheduler) LoadCheckpoint(jobID string) (*Checkpoint, error) {
	return s.checkpoint.Load(jobID)
}

// ── Peer Discovery Helpers ─────────────────────────────────────────────────────────

func (s *Scheduler) discoverPeers(ctx context.Context, need int) ([]string, error) {
	addrs, err := s.node.DiscoverWorkers(ctx, p2p.CapCPU, need*2)
	if err != nil || len(addrs) == 0 {
		peerIDs, perr := s.node.FindPeers(ctx, need*2)
		if perr != nil || len(peerIDs) == 0 {
			return nil, err
		}
		var peers []string
		for _, pid := range peerIDs {
			peers = append(peers, pid.String())
		}
		return peers, nil
	}
	var peers []string
	for _, info := range addrs {
		peers = append(peers, info.ID.String())
	}
	return peers, nil
}

func (s *Scheduler) connectedPeers() []string {
	if s.node.Host == nil {
		return nil
	}
	connected := s.node.Host.Network().Peers()
	var peers []string
	for _, p := range connected {
		peers = append(peers, p.String())
	}
	return peers
}

func (s *Scheduler) filterHealthyPeers(peers []string) []string {
	var healthy []string
	for _, pid := range peers {
		cb := s.node.PeerBreakers.Get(pid)
		if cb.State() != resilience.StateOpen {
			healthy = append(healthy, pid)
		} else {
			log.Printf("[%s] skipping peer %s: circuit open", s.nodeID[:6], pid[:min(16, len(pid))])
		}
	}
	return healthy
}

// getLANFirstPeers partitions peers into local (LAN) and global (WAN) pools
func (s *Scheduler) getLANFirstPeers(peers []string) (local, remote []string) {
	if s.node.Host == nil {
		return nil, peers
	}

	for _, pidStr := range peers {
		pid, err := peer.Decode(pidStr)
		if err != nil {
			continue
		}
		if s.node.IsLANPeer(pid) {
			local = append(local, pidStr)
		} else {
			remote = append(remote, pidStr)
		}
	}

	log.Printf("[%s] peer tiering: %d local, %d remote", s.nodeID[:6], len(local), len(remote))
	return local, remote
}

// isBrowserPeer checks if a peer is tagged as a browser node
func (s *Scheduler) isBrowserPeer(pidStr string) bool {
	pid, err := peer.Decode(pidStr)
	if err != nil {
		return false
	}
	clientType, _ := s.node.Host.Peerstore().Get(pid, "client_type")
	return clientType == "browser"
}

func countSuccess(results []ChunkResult) int {
	n := 0
	for _, r := range results {
		if r.Success {
			n++
		}
	}
	return n
}

func timePtr(t time.Time) *time.Time { return &t }
