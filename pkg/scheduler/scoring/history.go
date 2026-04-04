package scoring

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Trajectory represents a task execution trajectory (like ATIF format)
type Trajectory struct {
	ID          string     `json:"id"`
	TaskType    string     `json:"task_type"`
	TaskInput   string     `json:"task_input"`
	Steps       []TrajStep `json:"steps"`
	FinalScore  float64    `json:"score"`
	LatencyMs   int        `json:"latency_ms"`
	CostCredits int        `json:"cost_credits"`
	NodeID      string     `json:"node_id"`
	Success     bool       `json:"success"`
	CreatedAt   time.Time  `json:"created_at"`
}

// TrajStep represents a single step in a trajectory
type TrajStep struct {
	ID        int       `json:"step_id"`
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Input     string    `json:"input,omitempty"`
	Output    string    `json:"output,omitempty"`
	Duration  int       `json:"duration_ms,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// TaskHistory stores task execution history
type TaskHistory struct {
	mu           sync.RWMutex
	trajectories []Trajectory
	maxSize      int
	byTaskType   map[string][]*Trajectory
	byNode       map[string][]*Trajectory
}

// NewTaskHistory creates a new task history
func NewTaskHistory(maxSize int) *TaskHistory {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &TaskHistory{
		trajectories: make([]Trajectory, 0, maxSize),
		maxSize:      maxSize,
		byTaskType:   make(map[string][]*Trajectory),
		byNode:       make(map[string][]*Trajectory),
	}
}

// Add adds a trajectory to history
func (h *TaskHistory) Add(t Trajectory) {
	h.mu.Lock()
	defer h.mu.Unlock()

	t.CreatedAt = time.Now()
	h.trajectories = append(h.trajectories, t)

	// Index by task type
	h.byTaskType[t.TaskType] = append(h.byTaskType[t.TaskType], &h.trajectories[len(h.trajectories)-1])

	// Index by node
	h.byNode[t.NodeID] = append(h.byNode[t.NodeID], &h.trajectories[len(h.trajectories)-1])

	// Trim if over max size
	if len(h.trajectories) > h.maxSize {
		h.trim()
	}
}

// trim removes oldest entries
func (h *TaskHistory) trim() {
	remove := len(h.trajectories) - h.maxSize
	if remove <= 0 {
		return
	}

	h.trajectories = h.trajectories[remove:]

	// Rebuild indices
	h.byTaskType = make(map[string][]*Trajectory)
	h.byNode = make(map[string][]*Trajectory)

	for i := range h.trajectories {
		t := &h.trajectories[i]
		h.byTaskType[t.TaskType] = append(h.byTaskType[t.TaskType], t)
		h.byNode[t.NodeID] = append(h.byNode[t.NodeID], t)
	}
}

// GetRecent returns the most recent trajectories
func (h *TaskHistory) GetRecent(n int) []*Trajectory {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if n > len(h.trajectories) {
		n = len(h.trajectories)
	}

	result := make([]*Trajectory, n)
	for i := 0; i < n; i++ {
		idx := len(h.trajectories) - n + i
		result[i] = &h.trajectories[idx]
	}
	return result
}

// GetByTaskType returns trajectories for a task type
func (h *TaskHistory) GetByTaskType(taskType string) []*Trajectory {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if trajs, ok := h.byTaskType[taskType]; ok {
		result := make([]*Trajectory, len(trajs))
		for i, t := range trajs {
			result[i] = t
		}
		return result
	}
	return nil
}

// GetByNode returns trajectories for a node
func (h *TaskHistory) GetByNode(nodeID string) []*Trajectory {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if trajs, ok := h.byNode[nodeID]; ok {
		result := make([]*Trajectory, len(trajs))
		for i, t := range trajs {
			result[i] = t
		}
		return result
	}
	return nil
}

// BestByTaskType returns best scoring trajectories for a task type
func (h *TaskHistory) BestByTaskType(taskType string, n int) []*Trajectory {
	trajs := h.GetByTaskType(taskType)
	if trajs == nil {
		return nil
	}

	// Sort by score (descending)
	for i := 0; i < len(trajs)-1; i++ {
		for j := i + 1; j < len(trajs); j++ {
			if trajs[j].FinalScore > trajs[i].FinalScore {
				trajs[i], trajs[j] = trajs[j], trajs[i]
			}
		}
	}

	if n > len(trajs) {
		n = len(trajs)
	}
	return trajs[:n]
}

// GetStats returns statistics about task history
func (h *TaskHistory) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := map[string]interface{}{
		"total_trajectories": len(h.trajectories),
		"max_size":           h.maxSize,
		"task_types":         len(h.byTaskType),
		"nodes":              len(h.byNode),
	}

	// Per-task-type stats
	typeStats := make(map[string]interface{})
	for taskType, trajs := range h.byTaskType {
		totalScore := 0.0
		totalLatency := 0
		successCount := 0
		for _, t := range trajs {
			totalScore += t.FinalScore
			totalLatency += t.LatencyMs
			if t.Success {
				successCount++
			}
		}
		typeStats[taskType] = map[string]interface{}{
			"count":        len(trajs),
			"avg_score":    totalScore / float64(len(trajs)),
			"avg_latency":  totalLatency / len(trajs),
			"success_rate": float64(successCount) / float64(len(trajs)),
		}
	}
	stats["per_type"] = typeStats

	return stats
}

// Save saves history to a file
func (h *TaskHistory) Save(path string) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	data, err := json.MarshalIndent(h.trajectories, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// Load loads history from a file
func (h *TaskHistory) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var trajs []Trajectory
	if err := json.Unmarshal(data, &trajs); err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.trajectories = trajs
	h.byTaskType = make(map[string][]*Trajectory)
	h.byNode = make(map[string][]*Trajectory)

	for i := range h.trajectories {
		t := &h.trajectories[i]
		h.byTaskType[t.TaskType] = append(h.byTaskType[t.TaskType], t)
		h.byNode[t.NodeID] = append(h.byNode[t.NodeID], t)
	}

	return nil
}

// Clear clears all history
func (h *TaskHistory) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.trajectories = make([]Trajectory, 0, h.maxSize)
	h.byTaskType = make(map[string][]*Trajectory)
	h.byNode = make(map[string][]*Trajectory)
}
