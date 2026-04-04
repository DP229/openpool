package task

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// TaskHandler is the interface all task handlers must implement
type TaskHandler interface {
	// Execute runs the task and returns the result
	Execute(ctx context.Context, input []byte) (*Result, error)

	// Name returns the task type name (e.g., "fib", "matrix_mul")
	Name() string

	// Validate checks if the input is valid for this handler
	Validate(input []byte) error

	// EstimateCost estimates the cost in credits for this task
	EstimateCost(input []byte) int
}

// Registry manages all task handlers
type Registry struct {
	handlers map[string]TaskHandler
}

// Global registry instance
var globalRegistry *Registry

// Init creates the global registry and registers default handlers
func Init() *Registry {
	globalRegistry = &Registry{
		handlers: make(map[string]TaskHandler),
	}
	return globalRegistry
}

// Get returns the global registry
func Get() *Registry {
	if globalRegistry == nil {
		return Init()
	}
	return globalRegistry
}

// Register adds a handler to the registry
func (r *Registry) Register(handler TaskHandler) {
	r.handlers[handler.Name()] = handler
}

// GetHandler returns a handler by name
func (r *Registry) GetHandler(name string) (TaskHandler, bool) {
	handler, ok := r.handlers[name]
	return handler, ok
}

// Execute runs a task by name
func (r *Registry) Execute(ctx context.Context, taskType string, taskID, nodeID string, input []byte) (*Result, error) {
	handler, ok := r.GetHandler(taskType)
	if !ok {
		return nil, fmt.Errorf("unknown task type: %s", taskType)
	}

	if err := handler.Validate(input); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	start := time.Now()
	result, err := handler.Execute(ctx, input)
	latency := time.Since(start)

	if err != nil {
		result = &Result{
			ID:        taskID,
			NodeID:    nodeID,
			Success:   false,
			Error:     err.Error(),
			Timestamp: time.Now(),
			Metrics: Metrics{
				LatencyMs:   int(latency.Milliseconds()),
				CostCredits: handler.EstimateCost(input),
			},
		}
	} else {
		result.Metrics.LatencyMs = int(latency.Milliseconds())
	}

	// Ensure result has required fields
	if result.ID == "" {
		result.ID = taskID
	}
	if result.NodeID == "" {
		result.NodeID = nodeID
	}
	if result.Metrics.CostCredits == 0 {
		result.Metrics.CostCredits = handler.EstimateCost(input)
	}

	return result, nil
}

// List returns all registered task types
func (r *Registry) List() []string {
	types := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		types = append(types, name)
	}
	return types
}

// Base handler for common functionality
type BaseHandler struct {
	Name_       string
	CostCredits int
}

func (h *BaseHandler) Name() string                  { return h.Name_ }
func (h *BaseHandler) EstimateCost(input []byte) int { return h.CostCredits }
func (h *BaseHandler) Validate(input []byte) error   { return nil }
func (h *BaseHandler) Execute(ctx context.Context, input []byte) (*Result, error) {
	return nil, fmt.Errorf("not implemented")
}

// Default cost estimates
const (
	DefaultCostCPU   = 5
	DefaultCostGPU   = 20
	DefaultCostBatch = 100
)

// TaskInput is a common input format for tasks
type TaskInput struct {
	Op   string          `json:"op"`
	Arg  json.RawMessage `json:"arg"`
	Data json.RawMessage `json:"data,omitempty"`
}

// ParseInput parses common task input
func ParseInput(input []byte) (*TaskInput, error) {
	var ti TaskInput
	if err := json.Unmarshal(input, &ti); err != nil {
		return nil, err
	}
	return &ti, nil
}
