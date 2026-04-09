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
)

// ── Ghost Cell Support ──────────────────────────────────────────────────────────

// GhostBoundary describes one side of a halo exchange for a CFD subdomain.
// For a 2D finite-volume mesh split into NxM tiles, each tile may need
// one row/column of ghost cells from its north, south, east, west neighbours.
type GhostBoundary struct {
	Direction string          `json:"direction"` // "north", "south", "east", "west" (or "top", "bottom", "left", "right")
	Width     int             `json:"width"`     // number of ghost cell layers (typically 1-2 for FV)
	Data      json.RawMessage `json:"data"`      // serialised boundary data (filled at dispatch)
}

// GhostRegion describes the ghost cell layout for a single DAG node.
// Interior holds the core subdomain data; Boundaries list the halo
// exchanges that must be injected before execution.
type GhostRegion struct {
	InteriorI  int             `json:"interior_i"`  // interior rows
	InteriorJ  int             `json:"interior_j"`  // interior columns
	GhostWidth int             `json:"ghost_width"` // halo layers on each side
	Interior   json.RawMessage `json:"interior"`    // core subdomain payload
	Boundaries []GhostBoundary `json:"boundaries"`  // populated from parent outputs
}

// ── DAG Data Structures ─────────────────────────────────────────────────────────

// DAGNode is a single unit of work within a Directed Acyclic Graph.
// Each node may depend on zero or more parent nodes. Parents produce
// data that flows into this node's Params (including ghost-cell boundaries).
type DAGNode struct {
	ID       string          `json:"id"`
	TaskID   string          `json:"task_id"`
	Index    int             `json:"index"`
	Parents  []string        `json:"parents,omitempty"`
	Children []string        `json:"children,omitempty"`
	Params   json.RawMessage `json:"params,omitempty"`
	Credits  int             `json:"credits"`
	Timeout  int             `json:"timeout"`

	// GhostRegions holds the halo exchange specification for CFD subdomains.
	// When non-nil, the scheduler will inject boundary data from completed
	// parent nodes before dispatching this node.
	GhostRegions []GhostRegion `json:"ghost_regions,omitempty"`
}

// DAGResult is the output of a single DAGNode execution, streamed back
// as soon as the node completes (not batched).
type DAGResult struct {
	NodeID     string          `json:"node_id"`
	Index      int             `json:"index"`
	Success    bool            `json:"success"`
	Data       json.RawMessage `json:"data,omitempty"`
	Error      string          `json:"error,omitempty"`
	DurationMs int64           `json:"duration_ms"`
}

// DAGSpec defines how to build and reduce a DAG task.
type DAGSpec struct {
	// Build constructs the DAG nodes from a root task.
	// Returned nodes must form a valid DAG — no cycles.
	// Parent/child edges should be populated via the Parents/Children fields.
	Build func(task *DAGTask) ([]*DAGNode, error)

	// OnPartial is called incrementally as each DAG node completes.
	// It receives the result of a single node. This enables
	// streaming/pipeline reduction rather than waiting for all N chunks.
	// Return true to continue, false to abort the DAG early.
	OnPartial func(result *DAGResult) bool

	// Finalize is called once after all nodes complete (or after early abort).
	// It receives all partial results collected so far and produces the
	// final aggregated output.
	Finalize func(results []*DAGResult) (json.RawMessage, error)
}

// DAGTask is the user-facing top-level task that seeds a DAG execution.
type DAGTask struct {
	ID       string          `json:"id"`
	Code     string          `json:"code,omitempty"`
	Lang     string          `json:"lang,omitempty"`
	Credits  int             `json:"credits"`
	Timeout  int             `json:"timeout"`
	MeshSpec json.RawMessage `json:"mesh_spec,omitempty"`
}

// ── DAG Engine ───────────────────────────────────────────────────────────────────

// DAGEngine orchestrates DAG-based distributed computation with ghost-cell support.
type DAGEngine struct {
	sched  *Scheduler
	nodeID string
}

func NewDAGEngine(sched *Scheduler, nodeID string) *DAGEngine {
	return &DAGEngine{sched: sched, nodeID: nodeID}
}

// ── Topological Ordering ─────────────────────────────────────────────────────────

// topologicalLevels groups DAG nodes into levels using Kahn's algorithm.
// All nodes within the same level have zero inter-dependencies and can
// execute in parallel. Level 0 contains root nodes (no parents).
// Returns an error if the graph contains a cycle.
func topologicalLevels(nodes []*DAGNode) ([][]*DAGNode, error) {
	index := make(map[string]*DAGNode, len(nodes))
	inDegree := make(map[string]int, len(nodes))
	childrenOf := make(map[string][]string, len(nodes))

	for _, n := range nodes {
		index[n.ID] = n
		if _, ok := inDegree[n.ID]; !ok {
			inDegree[n.ID] = 0
		}
	}
	for _, n := range nodes {
		for _, pid := range n.Parents {
			inDegree[n.ID]++
			childrenOf[pid] = append(childrenOf[pid], n.ID)
		}
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var levels [][]*DAGNode
	visited := 0

	for len(queue) > 0 {
		var level []*DAGNode
		var nextQueue []string

		for _, id := range queue {
			node := index[id]
			level = append(level, node)
			visited++

			for _, childID := range childrenOf[id] {
				inDegree[childID]--
				if inDegree[childID] == 0 {
					nextQueue = append(nextQueue, childID)
				}
			}
		}

		levels = append(levels, level)
		queue = nextQueue
	}

	if visited != len(nodes) {
		return nil, fmt.Errorf("DAG contains a cycle: visited %d of %d nodes", visited, len(nodes))
	}

	return levels, nil
}

// ── Ghost Cell Data Injection ────────────────────────────────────────────────────

// injectGhostData merges completed parent results into a child node's
// ghost region boundaries. It matches parent node IDs to ghost boundary
// directions using a naming convention: if a parent ID ends with a recognized
// direction suffix (e.g. "-north", "-south"), its output data is injected
// into the corresponding boundary. Otherwise, parent data is injected into
// boundaries in the order they appear.
func injectGhostData(node *DAGNode, parentResults map[string]*DAGResult) {
	if len(node.GhostRegions) == 0 {
		return
	}

	directionSuffix := map[string]string{
		"-north": "north", "-south": "south",
		"-east": "east", "-west": "west",
		"-top": "top", "-bottom": "bottom",
		"-left": "left", "-right": "right",
	}

	for pid, result := range parentResults {
		if !result.Success || result.Data == nil {
			continue
		}

		dir := ""
		for suffix, d := range directionSuffix {
			if len(pid) >= len(suffix) && pid[len(pid)-len(suffix):] == suffix {
				dir = d
				break
			}
		}

		for gi := range node.GhostRegions {
			for bi := range node.GhostRegions[gi].Boundaries {
				b := &node.GhostRegions[gi].Boundaries[bi]
				if dir != "" && b.Direction == dir {
					b.Data = result.Data
					break
				}
			}
		}

		if dir == "" {
			boundIdx := 0
			for gi := range node.GhostRegions {
				for bi := range node.GhostRegions[gi].Boundaries {
					if node.GhostRegions[gi].Boundaries[bi].Data == nil {
						node.GhostRegions[gi].Boundaries[bi].Data = result.Data
						boundIdx++
						break
					}
				}
				if boundIdx > 0 {
					break
				}
			}
		}
	}
}

// ── DAG Execution ────────────────────────────────────────────────────────────────

// SubmitDAG executes a DAG task across distributed peers with level-by-level
// scheduling, ghost cell injection, streaming reduce, fault-tolerant retries,
// and stream backpressure.
//
//  1. Build the DAG nodes via spec.Build.
//  2. Compute topological levels (Kahn's algorithm).
//  3. For each level, discover healthy peers and dispatch nodes in parallel.
//     Before dispatch, inject ghost cell data from completed parent nodes.
//     Each dispatch acquires a semaphore slot (backpressure).
//     On failure, retry with exponential backoff on a different peer.
//  4. As each node completes, call spec.OnPartial (streaming reduce)
//     and save a checkpoint for crash recovery.
//  5. After all levels complete (or early abort), call spec.Finalize.
func (e *DAGEngine) SubmitDAG(ctx context.Context, task *DAGTask, spec DAGSpec) (json.RawMessage, error) {
	nodes, err := spec.Build(task)
	if err != nil {
		return nil, fmt.Errorf("build dag: %w", err)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("dag produced no nodes")
	}

	levels, err := topologicalLevels(nodes)
	if err != nil {
		return nil, fmt.Errorf("topological sort: %w", err)
	}

	log.Printf("[%s] DAG %s: %d nodes across %d levels", e.nodeID[:6], task.ID, len(nodes), len(levels))

	nodeMap := make(map[string]*DAGNode, len(nodes))
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	var allResults []*DAGResult
	completed := make(map[string]*DAGResult)
	var mu sync.Mutex
	aborted := false
	cfg := e.sched.Config()

	// Try to load a previous checkpoint for this job
	cp := &Checkpoint{
		JobID:     task.ID,
		Completed: make(map[string]bool),
		Results:   make(map[string][]byte),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
	if existing, loadErr := e.sched.LoadCheckpoint(task.ID); loadErr == nil && existing != nil {
		cp = existing
		for nodeID := range cp.Completed {
			if saved, ok := cp.Results[nodeID]; ok {
				var r DAGResult
				if jsonErr := json.Unmarshal(saved, &r); jsonErr == nil {
					completed[nodeID] = &r
					allResults = append(allResults, &r)
				}
			}
		}
		log.Printf("[%s] DAG %s: restored %d completed nodes from checkpoint", e.nodeID[:6], task.ID, len(cp.Completed))
	}

	for levelIdx, level := range levels {
		if aborted {
			break
		}

		log.Printf("[%s] DAG level %d: dispatching %d nodes", e.nodeID[:6], levelIdx, len(level))

		// Skip nodes already completed from a checkpoint
		var pending []*DAGNode
		for _, node := range level {
			if !cp.Completed[node.ID] {
				pending = append(pending, node)
			}
		}
		if len(pending) == 0 {
			log.Printf("[%s] DAG level %d: all nodes already completed", e.nodeID[:6], levelIdx)
			continue
		}

		peers, err := e.sched.discoverPeers(ctx, len(pending))
		if err != nil {
			log.Printf("[%s] DAG peer discovery: %v — using connected peers", e.nodeID[:6], err)
			peers = e.sched.connectedPeers()
		}
		peers = e.filterHealthyPeers(peers)
		if len(peers) == 0 {
			e.saveCheckpoint(cp)
			return nil, fmt.Errorf("no peers available at DAG level %d", levelIdx)
		}

		resultCh := make(chan *DAGResult, len(pending))

		for i, node := range pending {
			if aborted {
				break
			}

			peerID := peers[i%len(peers)]

			mu.Lock()
			injectGhostData(node, completed)
			mu.Unlock()

			p2pTask := &p2p.Task{
				ID:         node.ID,
				Code:       task.Code,
				Lang:       task.Lang,
				Params:     node.Params,
				TimeoutSec: node.Timeout,
				Credits:    node.Credits,
				State:      "pending",
				CreatedAt:  time.Now(),
			}

			if len(node.GhostRegions) > 0 {
				ghostData, _ := json.Marshal(map[string]interface{}{
					"type":          "cfd_chunk",
					"ghost_regions": node.GhostRegions,
				})
				if p2pTask.Params == nil {
					p2pTask.Params = ghostData
				} else {
					merged, _ := mergeJSON(p2pTask.Params, ghostData)
					p2pTask.Params = merged
				}
			}

			go func(n *DAGNode, pt *p2p.Task, initialPeer string, allPeers []string) {
				result := e.dispatchWithRetry(ctx, n, pt, initialPeer, allPeers, levelIdx, cfg)
				resultCh <- result

				mu.Lock()
				completed[result.NodeID] = result
				allResults = append(allResults, result)
				if result.Success {
					cp.Completed[result.NodeID] = true
					if saved, err := json.Marshal(result); err == nil {
						cp.Results[result.NodeID] = saved
					}
					cp.UpdatedAt = time.Now().Unix()
					e.saveCheckpoint(cp)
				}
				mu.Unlock()

				log.Printf("[%s] DAG node %s done: success=%v", e.nodeID[:6], result.NodeID, result.Success)

				if spec.OnPartial != nil && !aborted {
					if !spec.OnPartial(result) {
						log.Printf("[%s] DAG aborted by OnPartial after node %s", e.nodeID[:6], result.NodeID)
						aborted = true
					}
				}
			}(node, p2pTask, peerID, peers)
		}

		for range pending {
			if aborted {
				break
			}
			select {
			case result := <-resultCh:
				_ = result // already handled in goroutine
			case <-ctx.Done():
				e.saveCheckpoint(cp)
				return nil, ctx.Err()
			}
		}

		if !aborted {
			levelSucceeded := 0
			mu.Lock()
			for _, n := range level {
				if r, ok := completed[n.ID]; ok && r.Success {
					levelSucceeded++
				}
			}
			mu.Unlock()
			log.Printf("[%s] DAG level %d: %d/%d succeeded", e.nodeID[:6], levelIdx, levelSucceeded, len(level))
		}
	}

	e.sched.checkpoint.Delete(task.ID)

	if spec.Finalize == nil {
		return json.RawMessage(`{"status":"completed"}`), nil
	}

	finalData, err := spec.Finalize(allResults)
	if err != nil {
		return nil, fmt.Errorf("finalize: %w", err)
	}
	return finalData, nil
}

// dispatchWithRetry submits a single DAG node to a peer, retrying on failure
// with exponential backoff and peer rotation. Acquires a semaphore slot to
// cap concurrent libp2p streams.
func (e *DAGEngine) dispatchWithRetry(ctx context.Context, node *DAGNode, pt *p2p.Task, initialPeer string, allPeers []string, levelIdx int, cfg SchedulerConfig) *DAGResult {
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

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(cfg.RetryBaseMs) * time.Duration(1<<(attempt-1)) * time.Millisecond
			if delay > time.Duration(cfg.RetryMaxMs)*time.Millisecond {
				delay = time.Duration(cfg.RetryMaxMs) * time.Millisecond
			}
			log.Printf("[%s] DAG retry %d/%d for %s, backoff %v", e.nodeID[:6], attempt, cfg.MaxRetries, node.ID, delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return &DAGResult{NodeID: node.ID, Index: node.Index, Success: false, Error: ctx.Err().Error()}
			}

			healthyPeers := e.filterHealthyPeers(allPeers)
			if len(healthyPeers) == 0 {
				return &DAGResult{NodeID: node.ID, Index: node.Index, Success: false, Error: "no healthy peers for retry"}
			}
			peerIdx = (peerIdx + 1) % len(healthyPeers)
			initialPeer = healthyPeers[peerIdx]
		}

		// Acquire semaphore slot (backpressure)
		select {
		case e.sched.sem <- struct{}{}:
		case <-ctx.Done():
			return &DAGResult{NodeID: node.ID, Index: node.Index, Success: false, Error: ctx.Err().Error()}
		}

		start := time.Now()
		log.Printf("[%s] DAG dispatch %s → %s (level=%d, attempt=%d)", e.nodeID[:6], node.ID, initialPeer[:min(16, len(initialPeer))], levelIdx, attempt)

		err := e.sched.node.SubmitTask(ctx, initialPeer, pt)

		<-e.sched.sem // release semaphore

		if err == nil && pt.State == "completed" {
			return &DAGResult{
				NodeID: node.ID, Index: node.Index, Success: true,
				Data: pt.Result, DurationMs: time.Since(start).Milliseconds(),
			}
		}

		errMsg := ""
		if err != nil {
			errMsg = fmt.Sprintf("submit: %v", err)
		} else {
			errMsg = pt.Error
		}

		if attempt >= cfg.MaxRetries {
			log.Printf("[%s] DAG node %s exhausted retries: %s", e.nodeID[:6], node.ID, errMsg)
			return &DAGResult{
				NodeID: node.ID, Index: node.Index, Success: false,
				Error: errMsg, DurationMs: time.Since(start).Milliseconds(),
			}
		}

		log.Printf("[%s] DAG node %s attempt %d failed: %s — retrying", e.nodeID[:6], node.ID, attempt, errMsg)
	}

	return &DAGResult{NodeID: node.ID, Index: node.Index, Success: false, Error: "exhausted all retries"}
}

func (e *DAGEngine) saveCheckpoint(cp *Checkpoint) {
	if err := e.sched.SaveCheckpoint(cp); err != nil {
		log.Printf("[%s] checkpoint save failed: %v", e.nodeID[:6], err)
	}
}

// ── CFD Mesh DAG Builder ─────────────────────────────────────────────────────────

// CFDMeshSpec describes a finite-volume domain split into tiles for CFD solvers.
type CFDMeshSpec struct {
	Rows       int             `json:"rows"`        // number of tile rows
	Cols       int             `json:"cols"`        // number of tile columns
	GhostWidth int             `json:"ghost_width"` // halo layers (1 or 2)
	MeshData   json.RawMessage `json:"mesh_data"`   // full mesh data (partitioned by Build)
}

// BuildCFDMeshDAG constructs a DAG of CFD tile nodes with proper parent/child
// and ghost-region dependencies for a finite-volume discretization.
//
// Tile naming: "tile-{row}-{col}". Each tile has ghost regions pointing to
// its cardinal neighbours. Interior tiles have 4 neighbours; edge tiles
// have 2-3. Parent edges encode the dependency direction (e.g. "tile-0-1-north"
// means this boundary needs data from the tile to the north).
func BuildCFDMeshDAG(task *DAGTask) ([]*DAGNode, error) {
	var spec CFDMeshSpec
	if task.MeshSpec != nil {
		if err := json.Unmarshal(task.MeshSpec, &spec); err != nil {
			return nil, fmt.Errorf("parse mesh spec: %w", err)
		}
	}
	if spec.Rows <= 0 {
		spec.Rows = 1
	}
	if spec.Cols <= 0 {
		spec.Cols = 1
	}
	if spec.GhostWidth <= 0 {
		spec.GhostWidth = 1
	}

	nodes := make([][]*DAGNode, spec.Rows)
	for r := 0; r < spec.Rows; r++ {
		nodes[r] = make([]*DAGNode, spec.Cols)
		for c := 0; c < spec.Cols; c++ {
			id := fmt.Sprintf("tile-%d-%d", r, c)
			node := &DAGNode{
				ID:      id,
				TaskID:  task.ID,
				Index:   r*spec.Cols + c,
				Parents: nil,
				Credits: task.Credits,
				Timeout: task.Timeout,
			}

			if spec.GhostWidth > 0 {
				var boundaries []GhostBoundary
				if r > 0 {
					boundaries = append(boundaries, GhostBoundary{
						Direction: "north",
						Width:     spec.GhostWidth,
					})
				}
				if r < spec.Rows-1 {
					boundaries = append(boundaries, GhostBoundary{
						Direction: "south",
						Width:     spec.GhostWidth,
					})
				}
				if c > 0 {
					boundaries = append(boundaries, GhostBoundary{
						Direction: "west",
						Width:     spec.GhostWidth,
					})
				}
				if c < spec.Cols-1 {
					boundaries = append(boundaries, GhostBoundary{
						Direction: "east",
						Width:     spec.GhostWidth,
					})
				}

				node.GhostRegions = []GhostRegion{{
					GhostWidth: spec.GhostWidth,
					Boundaries: boundaries,
				}}
			}

			nodes[r][c] = node
		}
	}

	for r := 0; r < spec.Rows; r++ {
		for c := 0; c < spec.Cols; c++ {
			node := nodes[r][c]
			if r > 0 {
				parentID := fmt.Sprintf("tile-%d-%d", r-1, c)
				node.Parents = append(node.Parents, parentID)
				nodes[r-1][c].Children = append(nodes[r-1][c].Children, node.ID)
			}
			if c > 0 {
				parentID := fmt.Sprintf("tile-%d-%d", r, c-1)
				node.Parents = append(node.Parents, parentID)
				nodes[r][c-1].Children = append(nodes[r][c-1].Children, node.ID)
			}
		}
	}

	var flat []*DAGNode
	for r := 0; r < spec.Rows; r++ {
		for c := 0; c < spec.Cols; c++ {
			flat = append(flat, nodes[r][c])
		}
	}

	return flat, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────────

func (e *DAGEngine) filterHealthyPeers(peers []string) []string {
	var healthy []string
	for _, pid := range peers {
		cb := e.sched.node.PeerBreakers.Get(pid)
		if cb.State() != resilience.StateOpen {
			healthy = append(healthy, pid)
		} else {
			log.Printf("[%s] skipping peer %s: circuit open", e.nodeID[:6], pid[:min(16, len(pid))])
		}
	}
	return healthy
}

func mergeJSON(a, b json.RawMessage) (json.RawMessage, error) {
	var m1, m2 map[string]interface{}
	if err := json.Unmarshal(a, &m1); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m2); err != nil {
		return nil, err
	}
	for k, v := range m2 {
		m1[k] = v
	}
	return json.Marshal(m1)
}
