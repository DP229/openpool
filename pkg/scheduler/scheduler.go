// Package scheduler handles task chunking and distributed MapReduce-style execution.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dp229/openpool/pkg/p2p"
)

// Chunk represents a slice of a larger task.
type Chunk struct {
	ID       string          `json:"id"`       // e.g. "fib-chunk-0"
	TaskID   string          `json:"task_id"`  // parent task
	Index    int             `json:"index"`    // chunk index 0..N-1
	Params   json.RawMessage `json:"params"`   // chunk-specific params (range, slice, etc.)
	Credits  int             `json:"credits"`  // credits for this chunk
	Timeout  int             `json:"timeout"`  // seconds
}

// ChunkResult is the output of a single chunk execution.
type ChunkResult struct {
	ChunkID  string          `json:"chunk_id"`
	Index    int             `json:"index"`
	Success  bool            `json:"success"`
	Data     json.RawMessage `json:"data,omitempty"`
	Error    string          `json:"error,omitempty"`
	DurationMs int64         `json:"duration_ms"`
}

// MapReduce defines how to split and recombine a task.
type MapReduce struct {
	// Split divides the task into N chunks
	Split func(task *p2p.Task) ([]Chunk, error)
	// Reduce combines chunk results into a final result
	Reduce func(results []ChunkResult) (json.RawMessage, error)
}

// Scheduler distributes chunked tasks across available peers.
type Scheduler struct {
	node     *p2p.Node
	nodeID   string
	mu       sync.RWMutex
	pending  map[string]chan ChunkResult // chunkID -> result channel
}

// New creates a new task scheduler.
func New(node *p2p.Node, nodeID string) *Scheduler {
	return &Scheduler{
		node:    node,
		nodeID: nodeID,
		pending: make(map[string]chan ChunkResult),
	}
}

// SubmitChunked submits a task split into chunks across multiple peers.
// It discovers peers, assigns chunks, collects results, and reduces them.
func (s *Scheduler) SubmitChunked(ctx context.Context, task *p2p.Task, mr MapReduce) (*p2p.Task, error) {
	// 1. Split into chunks
	chunks, err := mr.Split(task)
	if err != nil {
		return nil, fmt.Errorf("split: %w", err)
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks produced")
	}
	log.Printf("[%s] Split task %s into %d chunks", s.nodeID[:6], task.ID, len(chunks))

	// 2. Discover peers
	peers, err := s.discoverPeers(ctx, len(chunks))
	if err != nil {
		log.Printf("[%s] peer discovery: %v — using connected peers", s.nodeID[:6], err)
		peers = s.connectedPeers()
	}
	if len(peers) == 0 {
		return nil, fmt.Errorf("no peers available")
	}
	log.Printf("[%s] Found %d peers for %d chunks", s.nodeID[:6], len(peers), len(chunks))

	// 3. Distribute chunks to peers
	resultCh := make(chan ChunkResult, len(chunks))
	assigned := 0

	for i, chunk := range chunks {
		peerID := peers[i%len(peers)] // round-robin across peers

		// Build the chunk task
		chunkTask := &p2p.Task{
			ID:         chunk.ID,
			Code:       task.Code,
			Lang:       task.Lang,
			Params:     chunk.Params,
			TimeoutSec: chunk.Timeout,
			Credits:    chunk.Credits,
			State:      "pending",
			CreatedAt:  time.Now(),
		}

		// Register for result
		s.mu.Lock()
		s.pending[chunk.ID] = resultCh
		s.mu.Unlock()

		// Submit asynchronously on a dedicated stream
		go func(c Chunk, pt *p2p.Task, pid string) {
			start := time.Now()
			log.Printf("[%s] Submitting chunk %s (index=%d) to %s", s.nodeID[:6], c.ID, c.Index, pid[:16])

			// Each chunk gets its own stream to the same peer — avoids stream multiplexing conflicts
			err := s.node.SubmitTask(ctx, pid, pt)

			if err != nil {
				log.Printf("[%s] Chunk %s failed: %v", s.nodeID[:6], c.ID, err)
				resultCh <- ChunkResult{
					ChunkID: c.ID, Index: c.Index, Success: false,
					Error: fmt.Sprintf("submit: %v", err),
				}
			} else if pt.State == "completed" {
				resultCh <- ChunkResult{
					ChunkID: c.ID, Index: c.Index, Success: true,
					Data: pt.Result, DurationMs: time.Since(start).Milliseconds(),
				}
			} else {
				resultCh <- ChunkResult{
					ChunkID: c.ID, Index: c.Index, Success: false,
					Error: pt.Error, DurationMs: time.Since(start).Milliseconds(),
				}
			}

			// Cleanup
			s.mu.Lock()
			delete(s.pending, c.ID)
			s.mu.Unlock()
		}(chunk, chunkTask, peerID)

		assigned++
	}

	// 4. Collect results
	var results []ChunkResult
	timeout := time.After(2 * time.Minute)
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

	// 5. Reduce
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

// discoverPeers finds peers via DHT or peerstore.
func (s *Scheduler) discoverPeers(ctx context.Context, need int) ([]string, error) {
	peerIDs, err := s.node.FindPeers(ctx, need*2)
	if err != nil || len(peerIDs) == 0 {
		return nil, err
	}
	var peers []string
	for _, pid := range peerIDs {
		peers = append(peers, pid.String())
	}
	return peers, nil
}

// connectedPeers returns currently connected peer IDs.
func (s *Scheduler) connectedPeers() []string {
	connected := s.node.Host.Network().Peers()
	var peers []string
	for _, p := range connected {
		peers = append(peers, p.String())
	}
	return peers
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
