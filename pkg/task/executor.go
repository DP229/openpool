package task

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dp229/openpool/pkg/ledger"
)

// Executor wraps task handlers with ledger integration
type Executor struct {
	registry *Registry
	ledger   *ledger.Ledger
	nodeID   string
}

// NewExecutor creates a new task executor
func NewExecutor(l *ledger.Ledger, nodeID string) *Executor {
	return &Executor{
		registry: Get(),
		ledger:  l,
		nodeID:  nodeID,
	}
}

// Register registers a task handler
func (e *Executor) Register(handler TaskHandler) {
	e.registry.Register(handler)
}

// Execute runs a task by type
func (e *Executor) Execute(ctx context.Context, taskType string, taskID string, input []byte) (*Result, error) {
	// Check ledger balance
	if e.ledger != nil {
		balance := e.ledger.GetCredits(e.nodeID)
		handler, ok := e.registry.GetHandler(taskType)
		if !ok {
			return nil, fmt.Errorf("unknown task type: %s", taskType)
		}
		cost := handler.EstimateCost(input)
		if balance < cost {
			return nil, fmt.Errorf("insufficient credits: have %d, need %d", balance, cost)
		}
	}
	
	// Execute task
	result, err := e.registry.Execute(ctx, taskType, taskID, e.nodeID, input)
	if err != nil {
		// Deduct partial cost on error
		if e.ledger != nil {
			e.ledger.AddCredits(e.nodeID, -5)
		}
		return result, err
	}
	
	// Deduct credits on success
	if e.ledger != nil {
		e.ledger.AddCredits(e.nodeID, -result.Metrics.CostCredits)
	}
	
	return result, nil
}

// ExecuteSimple runs a task with simple JSON input
func (e *Executor) ExecuteSimple(ctx context.Context, taskType string, taskID string, arg interface{}) (*Result, error) {
	input, err := json.Marshal(arg)
	if err != nil {
		return nil, err
	}
	return e.Execute(ctx, taskType, taskID, input)
}

// Stats returns executor statistics
func (e *Executor) Stats() map[string]interface{} {
	return map[string]interface{}{
		"handlers": e.registry.List(),
		"node_id": e.nodeID,
		"balance":  e.ledger.GetCredits(e.nodeID),
	}
}
