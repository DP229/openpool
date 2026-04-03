package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dp229/openpool/pkg/task"
)

// AgentTaskInput represents AI agent task input
type AgentTaskInput struct {
	Type        string          `json:"type"`        // agent_train, agent_eval, agent_optimize, agent_infer, rlhf, batch_infer
	Config      json.RawMessage `json:"config"`      // Agent configuration
	Task        string          `json:"task"`        // Task description
	Iterations  int             `json:"iterations"`  // Number of optimization iterations
	BatchSize   int             `json:"batch_size"`  // For batch inference
	Model       string          `json:"model"`       // LLM model to use
	MaxCredits  int             `json:"max_credits"`  // Budget limit
}

// AgentResult represents AI agent task output
type AgentResult struct {
	Trajectory []task.Step `json:"trajectory"`
	Score      float64       `json:"score"`      // 0.0 - 1.0
	Output     string        `json:"output"`     // Final output
	Iterations int           `json:"iterations"` // Actual iterations run
}

// AgentHandler handles AI agent tasks (like AutoAgent)
type AgentHandler struct {
	task.BaseHandler
}

// NewAgentHandler creates a new AI agent handler
func NewAgentHandler() *AgentHandler {
	return &AgentHandler{
		BaseHandler: task.BaseHandler{
			Name_:       "agent",
			CostCredits: task.DefaultCostAgent,
		},
	}
}

// Validate checks if the agent task input is valid
func (h *AgentHandler) Validate(input []byte) error {
	if len(input) == 0 {
		return fmt.Errorf("input is required")
	}
	
	var data AgentTaskInput
	if err := json.Unmarshal(input, &data); err != nil {
		return fmt.Errorf("invalid input format: %w", err)
	}
	
	if data.Type == "" {
		return fmt.Errorf("task type is required")
	}
	
	validTypes := map[string]bool{
		"agent_train":     true,
		"agent_eval":      true,
		"agent_optimize":  true,
		"agent_infer":     true,
		"rlhf":           true,
		"batch_infer":     true,
	}
	
	if !validTypes[data.Type] {
		return fmt.Errorf("invalid task type: %s", data.Type)
	}
	
	return nil
}

// EstimateCost estimates cost based on task type and iterations
func (h *AgentHandler) EstimateCost(input []byte) int {
	var data AgentTaskInput
	json.Unmarshal(input, &data)
	
	baseCost := task.DefaultCostAgent
	
	// Scale by iterations
	if data.Iterations > 0 {
		baseCost += data.Iterations * 5
	}
	
	// Batch inference costs more
	if data.Type == "batch_infer" && data.BatchSize > 0 {
		baseCost += data.BatchSize / 10
	}
	
	return baseCost
}

// Execute runs the AI agent task
func (h *AgentHandler) Execute(ctx context.Context, input []byte) (*task.Result, error) {
	start := time.Now()
	steps := []task.Step{
		{
			ID:        1,
			Timestamp: start,
			Action:    "agent_init",
			Input:     "Initializing agent task",
		},
	}
	
	var data AgentTaskInput
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}
	
	// Simulate agent task based on type
	result, agentSteps := h.runAgentTask(data, steps, start)
	steps = append(steps, agentSteps...)
	
	elapsed := time.Since(start)
	
	// Calculate score based on results
	score := h.calculateScore(result, data)
	
	res := &task.Result{
		Output: must(json.Marshal(result)),
		Success: true,
		Metrics: task.Metrics{
			LatencyMs:    int(elapsed.Milliseconds()),
			CostCredits: h.EstimateCost(input),
			Score:        score,
			Steps:        steps,
			TokensUsed:   result.Iterations * 1000, // Estimate
			Instances:    1,
		},
	}
	
	return res, nil
}

func (h *AgentHandler) runAgentTask(data AgentTaskInput, steps []task.Step, start time.Time) (*AgentResult, []task.Step) {
	result := &AgentResult{
		Trajectory: make([]task.Step, 0),
		Iterations: data.Iterations,
	}
	
	switch data.Type {
	case "agent_train":
		return h.agentTraining(data, steps, start)
		
	case "agent_eval":
		return h.agentEvaluation(data, steps, start)
		
	case "agent_optimize":
		return h.agentOptimization(data, steps, start)
		
	case "agent_infer":
		return h.agentInference(data, steps, start)
		
	case "rlhf":
		return h.rlhfTraining(data, steps, start)
		
	case "batch_infer":
		return h.batchInference(data, steps, start)
	}
	
	return result, steps
}

func (h *AgentHandler) agentTraining(data AgentTaskInput, steps []task.Step, start time.Time) (*AgentResult, []task.Step) {
	now := time.Now()
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: now,
		Action:    "agent_training",
		Input:     fmt.Sprintf("Training agent with config: %s", string(data.Config)),
		Output:    "Training completed",
		Duration:  500,
	})
	
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: time.Now(),
		Action:    "training_complete",
		Output:    "Fine-tuning successful",
	})
	
	return &AgentResult{
		Trajectory: steps,
		Score:     0.85,
		Output:    "Agent trained successfully",
		Iterations: data.Iterations,
	}, steps
}

func (h *AgentHandler) agentEvaluation(data AgentTaskInput, steps []task.Step, start time.Time) (*AgentResult, []task.Step) {
	now := time.Now()
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: now,
		Action:    "agent_evaluation",
		Input:     fmt.Sprintf("Evaluating agent on benchmark: %s", data.Task),
		Output:    "Running test suite...",
	})
	
	// Simulate benchmark results
	score := 0.75 + float64(data.Iterations%30)/100.0
	
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: time.Now(),
		Action:    "benchmark_complete",
		Output:    fmt.Sprintf("Score: %.2f", score),
	})
	
	return &AgentResult{
		Trajectory: steps,
		Score:     score,
		Output:    fmt.Sprintf("Benchmark score: %.2f", score),
		Iterations: data.Iterations,
	}, steps
}

func (h *AgentHandler) agentOptimization(data AgentTaskInput, steps []task.Step, start time.Time) (*AgentResult, []task.Step) {
	now := time.Now()
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: now,
		Action:    "agent_optimization_start",
		Input:     fmt.Sprintf("AutoML optimization, iterations: %d", data.Iterations),
		Output:    "Starting optimization loop...",
	})
	
	// Simulate AutoML iterations
	bestScore := 0.5
	for i := 0; i < data.Iterations && i < 100; i++ {
		iterScore := 0.5 + float64(i%50)/100.0
		if iterScore > bestScore {
			bestScore = iterScore
		}
		
		if i%10 == 0 {
			steps = append(steps, task.Step{
				ID:        len(steps) + 1,
				Timestamp: time.Now(),
				Action:    "optimization_iteration",
				Input:     fmt.Sprintf("Iteration %d", i),
				Output:    fmt.Sprintf("Current best: %.2f", bestScore),
			})
		}
	}
	
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: time.Now(),
		Action:    "optimization_complete",
		Output:    fmt.Sprintf("Best score achieved: %.2f", bestScore),
	})
	
	return &AgentResult{
		Trajectory: steps,
		Score:     bestScore,
		Output:    fmt.Sprintf("Optimization complete. Best score: %.2f", bestScore),
		Iterations: data.Iterations,
	}, steps
}

func (h *AgentHandler) agentInference(data AgentTaskInput, steps []task.Step, start time.Time) (*AgentResult, []task.Step) {
	now := time.Now()
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: now,
		Action:    "agent_inference",
		Input:     data.Task,
		Output:    "Running agent on task...",
	})
	
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: time.Now(),
		Action:    "inference_complete",
		Output:    "Task completed successfully",
	})
	
	return &AgentResult{
		Trajectory: steps,
		Score:     0.9,
		Output:    "Agent inference complete",
		Iterations: 1,
	}, steps
}

func (h *AgentHandler) rlhfTraining(data AgentTaskInput, steps []task.Step, start time.Time) (*AgentResult, []task.Step) {
	now := time.Now()
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: now,
		Action:    "rlhf_training",
		Input:     "Collecting human feedback...",
		Output:    "Training with RLHF...",
	})
	
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: time.Now(),
		Action:    "rlhf_complete",
		Output:    "RLHF training complete",
	})
	
	return &AgentResult{
		Trajectory: steps,
		Score:     0.88,
		Output:    "RLHF training complete",
		Iterations: data.Iterations,
	}, steps
}

func (h *AgentHandler) batchInference(data AgentTaskInput, steps []task.Step, start time.Time) (*AgentResult, []task.Step) {
	now := time.Now()
	batchSize := data.BatchSize
	if batchSize == 0 {
		batchSize = 100
	}
	
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: now,
		Action:    "batch_inference_start",
		Input:     fmt.Sprintf("Processing %d samples", batchSize),
		Output:    "Starting batch processing...",
	})
	
	// Simulate batch processing
	processed := 0
	for i := 0; i < batchSize; i += 10 {
		processed += 10
		if processed%50 == 0 {
			steps = append(steps, task.Step{
				ID:        len(steps) + 1,
				Timestamp: time.Now(),
				Action:    "batch_progress",
				Input:     fmt.Sprintf("Processed %d/%d", processed, batchSize),
				Output:    fmt.Sprintf("%.0f%% complete", float64(processed)/float64(batchSize)*100),
			})
		}
	}
	
	steps = append(steps, task.Step{
		ID:        len(steps) + 1,
		Timestamp: time.Now(),
		Action:    "batch_inference_complete",
		Output:    fmt.Sprintf("Processed %d samples", batchSize),
	})
	
	return &AgentResult{
		Trajectory: steps,
		Score:     0.92,
		Output:    fmt.Sprintf("Batch inference complete. %d samples processed.", batchSize),
		Iterations: batchSize,
	}, steps
}

func (h *AgentHandler) calculateScore(result *AgentResult, data AgentTaskInput) float64 {
	if result == nil {
		return 0.0
	}
	
	// Base score from result
	baseScore := result.Score
	
	// Bonus for completing requested iterations
	if data.Iterations > 0 && result.Iterations >= data.Iterations {
		baseScore += 0.05
	}
	
	// Cap at 1.0
	if baseScore > 1.0 {
		baseScore = 1.0
	}
	
	return baseScore
}
