package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// OptimizationResult represents an optimization action
type OptimizationResult struct {
	Timestamp   string                  `json:"timestamp"`
	Commit     string                  `json:"commit"`
	Change     string                  `json:"change"`
	Reason     string                  `json:"reason"`
	Before     BenchmarkSummary         `json:"before"`
	After      *BenchmarkSummary       `json:"after,omitempty"`
	Status     string                  `json:"status"` // applied, rejected, pending
	ScoreDelta float64                 `json:"score_delta"`
}

// BenchmarkSummary represents a benchmark result summary
type BenchmarkSummary struct {
	AvgScore    float64 `json:"avg_score"`
	AvgLatency  int    `json:"avg_latency_ms"`
	SuccessRate float64 `json:"success_rate"`
	TotalTasks  int    `json:"total_tasks"`
}

// OptimizationConfig defines optimization parameters
type OptimizationConfig struct {
	MinImprovement float64 `json:"min_improvement"` // Minimum % improvement to apply change
	MaxRetries    int     `json:"max_retries"`    // Max retries on failure
	ScheduleHours int     `json:"schedule_hours"`  // Hours between optimization runs
}

// Optimizer manages the optimization loop
type Optimizer struct {
	apiURL      string
	config      OptimizationConfig
	results     []OptimizationResult
	benchmarks  []BenchmarkSummary
}

// NewOptimizer creates a new optimizer
func NewOptimizer(apiURL string, config OptimizationConfig) *Optimizer {
	return &Optimizer{
		apiURL:     apiURL,
		config:     config,
		results:    make([]OptimizationResult, 0),
		benchmarks: make([]BenchmarkSummary, 0),
	}
}

// RunOptimizationLoop runs a single optimization iteration
func (o *Optimizer) RunOptimizationLoop() (*OptimizationResult, error) {
	log.Printf("🔄 Starting optimization loop...")

	// 1. Run benchmark
	before := o.runBenchmark()
	o.benchmarks = append(o.benchmarks, before)
	log.Printf("   Before: Score=%.2f, Latency=%dms, Success=%.1f%%",
		before.AvgScore, before.AvgLatency, before.SuccessRate*100)

	// 2. Analyze and propose changes
	change := o.analyzeAndPropose()
	
	result := &OptimizationResult{
		Timestamp: time.Now().Format(time.RFC3339),
		Commit:   getCommit(),
		Change:    change.Name,
		Reason:    change.Reason,
		Before:    before,
		Status:    "pending",
	}

	if change.Name == "no_change" {
		log.Printf("   ✅ No changes needed - system is optimal")
		result.Status = "rejected"
		result.After = before
		result.ScoreDelta = 0
		o.results = append(o.results, *result)
		return result, nil
	}

	// 3. Apply change
	log.Printf("   📝 Applying: %s", change.Name)
	if err := o.applyChange(change); err != nil {
		log.Printf("   ❌ Failed to apply: %v", err)
		result.Status = "rejected"
		o.results = append(o.results, *result)
		return result, nil
	}

	// 4. Wait for deployment
	time.Sleep(30 * time.Second)

	// 5. Run benchmark again
	after := o.runBenchmark()
	log.Printf("   After: Score=%.2f, Latency=%dms, Success=%.1f%%",
		after.AvgScore, after.AvgLatency, after.SuccessRate*100)

	// 6. Evaluate
	scoreDelta := (after.AvgScore - before.AvgScore) / before.AvgScore * 100
	
	result.After = after
	result.ScoreDelta = scoreDelta

	if scoreDelta >= o.config.MinImprovement {
		result.Status = "applied"
		log.Printf("   ✅ Change accepted: +%.1f%% improvement", scoreDelta)
	} else {
		// Revert
		o.revertChange(change)
		result.Status = "rejected"
		log.Printf("   ❌ Change rejected: only %.1f%% improvement (min: %.1f%%)", 
			scoreDelta, o.config.MinImprovement)
	}

	o.results = append(o.results, *result)
	return result, nil
}

func (o *Optimizer) runBenchmark() *BenchmarkSummary {
	// Run a quick benchmark (5 iterations)
	log.Printf("   🧪 Running benchmark...")

	taskTypes := []string{"agent_eval", "agent_optimize", "batch_infer"}
	
	totalScore := 0.0
	totalLatency := 0
	totalSuccess := 0
	totalTasks := 0

	for _, taskType := range taskTypes {
		passed := 0
		score := 0.0
		latency := 0

		for i := 0; i < 3; i++ {
			start := time.Now()
			
			payload := map[string]interface{}{
				"type":  taskType,
				"model": "llama3",
			}
			if taskType == "agent_eval" {
				payload["iterations"] = 5
			} else if taskType == "agent_optimize" {
				payload["iterations"] = 10
			} else {
				payload["batch_size"] = 25
			}

			result, err := o.runTask(payload)
			lat := int(time.Since(start).Milliseconds())

			if err == nil && result["score"] != nil {
				passed++
				if s, ok := result["score"].(float64); ok {
					score += s
				}
			}
			latency += lat
			totalTasks++
		}

		if passed > 0 {
			totalScore += score / float64(passed)
			totalSuccess += passed
		}
		totalLatency += latency / 3
	}

	return &BenchmarkSummary{
		AvgScore:    totalScore / float64(len(taskTypes)),
		AvgLatency:  totalLatency,
		SuccessRate: float64(totalSuccess) / float64(totalTasks),
		TotalTasks:  totalTasks,
	}
}

func (o *Optimizer) runTask(payload map[string]interface{}) (map[string]interface{}, error) {
	data, _ := json.Marshal(payload)
	
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(o.apiURL+"/agent", "application/json", 
		&reader{data: string(data)})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result["error"] != nil {
		return nil, fmt.Errorf("task error: %v", result["error"])
	}

	return result, nil
}

type reader struct {
	data string
}

func (r *reader) Read(p []byte) (n int, err error) {
	if len(r.data) == 0 {
		return 0, nil
	}
	n = copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// SuggestedChange represents a suggested optimization
type SuggestedChange struct {
	Name   string  `json:"name"`
	Reason string  `json:"reason"`
	Impact float64 `json:"impact"` // Expected % improvement
}

func (o *Optimizer) analyzeAndPropose() *SuggestedChange {
	// Analyze current benchmarks and suggest changes
	// This is a simplified version - in production, ML could suggest changes
	
	if len(o.benchmarks) < 2 {
		return &SuggestedChange{Name: "no_change", Reason: "Need more data"}
	}

	latest := o.benchmarks[len(o.benchmarks)-1]

	// Simple heuristics
	if latest.AvgLatency > 2000 {
		return &SuggestedChange{
			Name:   "optimize_latency",
			Reason: fmt.Sprintf("High latency detected: %dms", latest.AvgLatency),
			Impact: 15.0,
		}
	}

	if latest.SuccessRate < 0.9 {
		return &SuggestedChange{
			Name:   "improve_reliability",
			Reason: fmt.Sprintf("Low success rate: %.1f%%", latest.SuccessRate*100),
			Impact: 10.0,
		}
	}

	if latest.AvgScore < 0.8 {
		return &SuggestedChange{
			Name:   "improve_accuracy",
			Reason: fmt.Sprintf("Low accuracy: %.2f", latest.AvgScore),
			Impact: 12.0,
		}
	}

	return &SuggestedChange{Name: "no_change", Reason: "System performing well"}
}

func (o *Optimizer) applyChange(change *SuggestedChange) error {
	// In a real implementation, this would:
	// 1. Generate patch/config change
	// 2. Commit to git
	// 3. Trigger deployment
	
	log.Printf("      Applying: %s - %s", change.Name, change.Reason)
	
	// Simulate applying change
	time.Sleep(2 * time.Second)
	
	return nil
}

func (o *Optimizer) revertChange(change *SuggestedChange) {
	log.Printf("      Reverting: %s", change.Name)
	// In a real implementation, this would git revert
}

func (o *Optimizer) SaveResults(path string) error {
	data, _ := json.MarshalIndent(map[string]interface{}{
		"results":    o.results,
		"benchmarks": o.benchmarks,
	}, "", "  ")
	return os.WriteFile(path, data, 0644)
}

// ContinuousOptimizer runs optimization on a schedule
type ContinuousOptimizer struct {
	*Optimizer
	interval time.Duration
	stopCh   chan struct{}
}

// Start starts the continuous optimization loop
func (c *ContinuousOptimizer) Start() {
	c.stopCh = make(chan struct{})
	
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	log.Printf("🚀 Starting continuous optimizer (interval: %v)", c.interval)

	for {
		select {
		case <-ticker.C:
			c.RunOptimizationLoop()
		case <-c.stopCh:
			log.Printf("🛑 Stopping continuous optimizer")
			return
		}
	}
}

// Stop stops the continuous optimizer
func (c *ContinuousOptimizer) Stop() {
	close(c.stopCh)
}

func getCommit() string {
	return time.Now().Format("20060102150405")
}

func main() {
	// Flags
	apiURL := flag.String("api", "http://localhost:9000", "API URL")
	mode := flag.String("mode", "once", "Mode: once, continuous, analyze")
	interval := flag.Duration("interval", 24*time.Hour, "Interval between optimization runs")
	minImprovement := flag.Float64("min-improvement", 5.0, "Minimum improvement % to apply change")
	output := flag.String("output", "optimization_results.json", "Output file")
	
	flag.Parse()

	config := OptimizationConfig{
		MinImprovement: *minImprovement,
		MaxRetries:    3,
		ScheduleHours: int(interval.Hours()),
	}

	optimizer := NewOptimizer(*apiURL, config)

	switch *mode {
	case "once":
		result, err := optimizer.RunOptimizationLoop()
		if err != nil {
			log.Printf("❌ Optimization failed: %v", err)
			os.Exit(1)
		}
		
		if result.Status == "applied" {
			log.Printf("✅ Optimization applied: %s (+%.1f%%)", result.Change, result.ScoreDelta)
		} else {
			log.Printf("ℹ️  No changes applied")
		}
		
		optimizer.SaveResults(*output)

	case "continuous":
		continuous := &ContinuousOptimizer{
			Optimizer: optimizer,
			interval:  *interval,
		}
		continuous.Start()

	case "analyze":
		// Just run benchmark and analyze
		summary := optimizer.runBenchmark()
		data, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Printf("%s\n", data)

	default:
		fmt.Printf("Unknown mode: %s\n", *mode)
		fmt.Printf("Usage: optimizer --api http://localhost:9000 --mode once|continuous|analyze")
	}
}
