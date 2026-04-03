package task

import (
	"encoding/json"
	"time"
)

// Result represents the standardized output from any task execution
type Result struct {
	ID        string          `json:"id"`
	Output    json.RawMessage `json:"output"`
	Metrics   Metrics         `json:"metrics"`
	Timestamp time.Time       `json:"timestamp"`
	NodeID    string          `json:"node_id"`
	Error     string          `json:"error,omitempty"`
	Success   bool            `json:"success"`
}

// Metrics contains performance and quality data about the task execution
type Metrics struct {
	LatencyMs    int     `json:"latency_ms"`
	CostCredits  int     `json:"cost_credits"`
	Score        float64 `json:"score,omitempty"`   // 0.0-1.0 for optimization
	Steps        []Step  `json:"steps,omitempty"`   // Like ATIF trajectory
	TokensUsed   int     `json:"tokens_used,omitempty"`
	CacheHits    int     `json:"cache_hits,omitempty"`
	GPUUsed      bool    `json:"gpu_used"`
	Instances    int     `json:"instances,omitempty"` // For batch/distributed tasks
}

// Step represents a single step in task execution (for trajectory/audit)
type Step struct {
	ID        int       `json:"step_id"`
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Input     string    `json:"input,omitempty"`
	Output    string    `json:"output,omitempty"`
	Duration  int       `json:"duration_ms,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// NewResult creates a new result with defaults
func NewResult(id, nodeID string) *Result {
	return &Result{
		ID:        id,
		NodeID:    nodeID,
		Timestamp: time.Now(),
		Success:   true,
		Metrics: Metrics{
			LatencyMs:   0,
			CostCredits: 10, // Default cost
		},
	}
}

// AddStep adds a step to the trajectory
func (r *Result) AddStep(action, input, output string) {
	r.Metrics.Steps = append(r.Metrics.Steps, Step{
		ID:        len(r.Metrics.Steps) + 1,
		Timestamp: time.Now(),
		Action:    action,
		Input:     input,
		Output:    output,
	})
}

// SetOutput sets the output and marks success
func (r *Result) SetOutput(output interface{}) error {
	data, err := json.Marshal(output)
	if err != nil {
		return err
	}
	r.Output = data
	r.Success = true
	return nil
}

// SetError marks the result as failed
func (r *Result) SetError(err error) {
	r.Success = false
	r.Error = err.Error()
}

// ToJSON serializes the result to JSON
func (r *Result) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// FromJSON deserializes a result from JSON
func FromJSON(data []byte) (*Result, error) {
	var r Result
	err := json.Unmarshal(data, &r)
	return &r, err
}
