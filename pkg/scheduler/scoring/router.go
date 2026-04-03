package scoring

import (
	"sync"
)

// TaskRequirements specifies requirements for a task
type TaskRequirements struct {
	Type       string `json:"type"`
	RequireGPU bool  `json:"require_gpu"`
	MinCPU     int    `json:"min_cpu"`
	MaxLatency int    `json:"max_latency_ms"` // ms
	MaxCost    int    `json:"max_cost"`
}

// NodeCandidate represents a node that can execute a task
type NodeCandidate struct {
	NodeID         string  `json:"node_id"`
	Address        string  `json:"address"`
	Reliability    float64 `json:"reliability"`    // 0.0 - 1.0
	AvgLatency     int     `json:"avg_latency_ms"`
	SuccessRate    float64 `json:"success_rate"`  // 0.0 - 1.0
	CostPerTask    int     `json:"cost_per_task"`
	GPUCapable     bool    `json:"gpu_capable"`
	CPUCores       int     `json:"cpu_cores"`
	CurrentLoad    int     `json:"current_load"`  // tasks in flight
	Score          float64 `json:"score"`        // calculated
}

// Router handles task routing to nodes
type Router struct {
	mu        sync.RWMutex
	nodes     map[string]*NodeCandidate
	collector *MetricsCollector
}

// NewRouter creates a new router
func NewRouter(collector *MetricsCollector) *Router {
	return &Router{
		nodes:     make(map[string]*NodeCandidate),
		collector: collector,
	}
}

// RegisterNode registers a node for routing
func (r *Router) RegisterNode(node *NodeCandidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[node.NodeID] = node
}

// UnregisterNode removes a node from routing
func (r *Router) UnregisterNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nodes, nodeID)
}

// UpdateNode updates node info
func (r *Router) UpdateNode(node *NodeCandidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[node.NodeID] = node
}

// SelectNodes selects the best nodes for a task
func (r *Router) SelectNodes(req *TaskRequirements, count int) []*NodeCandidate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	candidates := r.filterAndScore(req)
	
	// Sort by score (descending)
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Score > candidates[i].Score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Return top N
	if count > len(candidates) {
		count = len(candidates)
	}
	return candidates[:count]
}

// SelectBestNode selects the single best node for a task
func (r *Router) SelectBestNode(req *TaskRequirements) *NodeCandidate {
	nodes := r.SelectNodes(req, 1)
	if len(nodes) == 0 {
		return nil
	}
	return nodes[0]
}

func (r *Router) filterAndScore(req *TaskRequirements) []*NodeCandidate {
	candidates := make([]*NodeCandidate, 0, len(r.nodes))

	for _, node := range r.nodes {
		// Filter by requirements
		if req.RequireGPU && !node.GPUCapable {
			continue
		}
		if req.MinCPU > 0 && node.CPUCores < req.MinCPU {
			continue
		}
		if req.MaxLatency > 0 && node.AvgLatency > req.MaxLatency {
			continue
		}
		if req.MaxCost > 0 && node.CostPerTask > req.MaxCost {
			continue
		}

		// Calculate score
		node.Score = r.calculateScore(node)
		candidates = append(candidates, node)
	}

	return candidates
}

func (r *Router) calculateScore(node *NodeCandidate) float64 {
	// Weights
	const (
		ReliabilityWeight = 0.35
		LatencyWeight    = 0.25
		CostWeight      = 0.20
		LoadWeight      = 0.20
	)

	// Reliability score (0.0 - 1.0)
	relScore := node.Reliability
	if relScore < 0 {
		relScore = 0
	}
	if relScore > 1 {
		relScore = 1
	}

	// Latency score (inverse)
	// Lower latency = higher score
	// 100ms = 0.9, 1000ms = 0.0
	latScore := 1.0 - float64(node.AvgLatency)/1000.0
	if latScore < 0 {
		latScore = 0
	}
	if latScore > 1 {
		latScore = 1
	}

	// Cost score (inverse)
	// Lower cost = higher score
	// 5 credits = 0.9, 100 credits = 0.0
	costScore := 1.0 - float64(node.CostPerTask)/100.0
	if costScore < 0 {
		costScore = 0
	}
	if costScore > 1 {
		costScore = 1
	}

	// Load score (inverse)
	// Lower load = higher score
	// 0 tasks = 1.0, 10+ tasks = 0.0
	loadScore := 1.0 - float64(node.CurrentLoad)/10.0
	if loadScore < 0 {
		loadScore = 0
	}
	if loadScore > 1 {
		loadScore = 1
	}

	return ReliabilityWeight*relScore + LatencyWeight*latScore + CostWeight*costScore + LoadWeight*loadScore
}

// GetNode returns a node by ID
func (r *Router) GetNode(nodeID string) *NodeCandidate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.nodes[nodeID]
}

// ListNodes returns all registered nodes
func (r *Router) ListNodes() []*NodeCandidate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make([]*NodeCandidate, 0, len(r.nodes))
	for _, n := range r.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

// GetStats returns router statistics
func (r *Router) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	totalLoad := 0
	gpuCapable := 0
	for _, n := range r.nodes {
		totalLoad += n.CurrentLoad
		if n.GPUCapable {
			gpuCapable++
		}
	}

	return map[string]interface{}{
		"total_nodes":    len(r.nodes),
		"gpu_capable":    gpuCapable,
		"total_load":     totalLoad,
		"avg_load":       float64(totalLoad) / float64(max(1, len(r.nodes))),
	}
}


