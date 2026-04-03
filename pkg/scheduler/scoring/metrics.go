package scoring

import (
	"sync"
	"time"
)

// TaskMetrics tracks performance metrics for a task type
type TaskMetrics struct {
	Type          string    `json:"type"`
	Count         int       `json:"count"`
	SuccessCount  int       `json:"success_count"`
	FailCount    int       `json:"fail_count"`
	TotalLatency int       `json:"total_latency_ms"` // ms
	AvgLatency   int       `json:"avg_latency_ms"`
	MinLatency   int       `json:"min_latency_ms"`
	MaxLatency   int       `json:"max_latency_ms"`
	LastRun      time.Time `json:"last_run"`
}

// NodeStats tracks performance metrics for a node
type NodeStats struct {
	NodeID         string    `json:"node_id"`
	TasksCompleted int       `json:"tasks_completed"`
	SuccessCount   int       `json:"success_count"`
	FailCount      int       `json:"fail_count"`
	TotalLatency   int       `json:"total_latency_ms"`
	AvgLatency     int       `json:"avg_latency_ms"`
	AvgScore       float64   `json:"avg_score"`      // 0.0 - 1.0
	LastSeen       time.Time `json:"last_seen"`
	GPUCapable     bool      `json:"gpu_capable"`
	CPUCores       int       `json:"cpu_cores"`
}

// ReliabilityScore calculates overall node reliability
type ReliabilityScore struct {
	SuccessRate  float64 // 0.0 - 1.0
	SpeedScore   float64 // 0.0 - 1.0 (based on latency)
	Availability float64 // 0.0 - 1.0 (based on uptime)
	Total        float64 // Weighted average
}

// MetricsCollector collects and aggregates metrics
type MetricsCollector struct {
	mu          sync.RWMutex
	taskMetrics map[string]*TaskMetrics
	nodeStats   map[string]*NodeStats
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		taskMetrics: make(map[string]*TaskMetrics),
		nodeStats:   make(map[string]*NodeStats),
	}
}

// RecordTask records task execution metrics
func (m *MetricsCollector) RecordTask(taskType string, latencyMs int, success bool, nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update task metrics
	tm, ok := m.taskMetrics[taskType]
	if !ok {
		tm = &TaskMetrics{
			Type: taskType,
		}
		m.taskMetrics[taskType] = tm
	}

	tm.Count++
	tm.TotalLatency += latencyMs
	tm.AvgLatency = tm.TotalLatency / tm.Count
	tm.LastRun = time.Now()

	if success {
		tm.SuccessCount++
	} else {
		tm.FailCount++
	}

	if tm.MinLatency == 0 || latencyMs < tm.MinLatency {
		tm.MinLatency = latencyMs
	}
	if latencyMs > tm.MaxLatency {
		tm.MaxLatency = latencyMs
	}

	// Update node stats
	ns, ok := m.nodeStats[nodeID]
	if !ok {
		ns = &NodeStats{
			NodeID: nodeID,
		}
		m.nodeStats[nodeID] = ns
	}

	ns.TasksCompleted++
	ns.TotalLatency += latencyMs
	ns.AvgLatency = ns.TotalLatency / ns.TasksCompleted
	ns.LastSeen = time.Now()

	if success {
		ns.SuccessCount++
	} else {
		ns.FailCount++
	}
}

// RecordScore records a task score for a node
func (m *MetricsCollector) RecordScore(nodeID string, score float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ns, ok := m.nodeStats[nodeID]
	if !ok {
		ns = &NodeStats{
			NodeID: nodeID,
		}
		m.nodeStats[nodeID] = ns
	}

	// Running average of scores
	if ns.TasksCompleted == 0 {
		ns.AvgScore = score
	} else {
		ns.AvgScore = (ns.AvgScore*float64(ns.TasksCompleted-1) + score) / float64(ns.TasksCompleted)
	}
}

// CalculateReliability calculates reliability score for a node
func (m *MetricsCollector) CalculateReliability(nodeID string) *ReliabilityScore {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ns, ok := m.nodeStats[nodeID]
	if !ok {
		return &ReliabilityScore{}
	}

	s := &ReliabilityScore{}

	// Success rate (0.0 - 1.0)
	if ns.TasksCompleted > 0 {
		s.SuccessRate = float64(ns.SuccessCount) / float64(ns.TasksCompleted)
	}

	// Speed score (inverse of latency, normalized)
	// Lower latency = higher score
	if ns.AvgLatency > 0 {
		// Assume 10s is poor, 100ms is excellent
		s.SpeedScore = 1.0 - float64(ns.AvgLatency)/10000.0
		if s.SpeedScore < 0 {
			s.SpeedScore = 0
		}
		if s.SpeedScore > 1.0 {
			s.SpeedScore = 1.0
		}
	}

	// Availability (based on how recently seen)
	uptime := time.Since(ns.LastSeen)
	if uptime < time.Minute {
		s.Availability = 1.0
	} else if uptime < 5*time.Minute {
		s.Availability = 0.8
	} else if uptime < 15*time.Minute {
		s.Availability = 0.5
	} else {
		s.Availability = 0.2
	}

	// Weighted total
	s.Total = 0.4*s.SuccessRate + 0.3*s.SpeedScore + 0.3*s.Availability

	return s
}

// GetTaskMetrics returns metrics for a task type
func (m *MetricsCollector) GetTaskMetrics(taskType string) *TaskMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.taskMetrics[taskType]
}

// GetNodeStats returns stats for a node
func (m *MetricsCollector) GetNodeStats(nodeID string) *NodeStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodeStats[nodeID]
}

// GetBestNode returns the node with highest reliability for a task type
func (m *MetricsCollector) GetBestNode(taskType string, requireGPU bool) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var bestNode string
	var bestScore float64

	for nodeID, ns := range m.nodeStats {
		// Skip nodes without required GPU
		if requireGPU && !ns.GPUCapable {
			continue
		}

		// Calculate reliability
		rel := &ReliabilityScore{
			SuccessRate:  float64(ns.SuccessCount) / float64(max(1, ns.TasksCompleted)),
			SpeedScore:   1.0 - float64(ns.AvgLatency)/10000.0,
			Availability: 1.0,
		}
		if rel.SpeedScore < 0 {
			rel.SpeedScore = 0
		}
		rel.Total = 0.4*rel.SuccessRate + 0.3*rel.SpeedScore + 0.3*rel.Availability

		if rel.Total > bestScore {
			bestScore = rel.Total
			bestNode = nodeID
		}
	}

	return bestNode
}

// GetAllNodeStats returns all node stats sorted by reliability
func (m *MetricsCollector) GetAllNodeStats() []*NodeStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make([]*NodeStats, 0, len(m.nodeStats))
	for _, ns := range m.nodeStats {
		stats = append(stats, ns)
	}
	return stats
}

// GetTaskMetricsAll returns all task metrics
func (m *MetricsCollector) GetTaskMetricsAll() []*TaskMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := make([]*TaskMetrics, 0, len(m.taskMetrics))
	for _, tm := range m.taskMetrics {
		metrics = append(metrics, tm)
	}
	return metrics
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
