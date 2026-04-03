package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// BenchmarkResult represents a single benchmark result
type BenchmarkResult struct {
	Commit    string  `json:"commit"`
	TaskType  string  `json:"task_type"`
	Score     float64 `json:"score"`
	LatencyMs int     `json:"latency_ms"`
	Cost      int     `json:"cost"`
	Success   bool    `json:"success"`
	Timestamp string  `json:"timestamp"`
}

// BenchmarkResultSet represents a complete benchmark result
type BenchmarkResultSet struct {
	Commit      string             `json:"commit"`
	Timestamp   string             `json:"timestamp"`
	TotalTasks  int               `json:"total_tasks"`
	PassedTasks int               `json:"passed_tasks"`
	FailedTasks int               `json:"failed_tasks"`
	AvgScore    float64            `json:"avg_score"`
	AvgLatency  int               `json:"avg_latency_ms"`
	AvgCost     int               `json:"avg_cost"`
	Results     []BenchmarkResult `json:"results"`
}

func main() {
	// Flags
	apiURL := flag.String("api", "http://localhost:9000", "API URL")
	taskType := flag.String("task", "", "Task type to benchmark")
	iterations := flag.Int("iterations", 10, "Number of iterations")
	timeout := flag.Int("timeout", 60, "Task timeout in seconds")
	output := flag.String("output", "benchmark_results.json", "Output file")
	flag.Parse()

	if *taskType == "" {
		fmt.Println("Usage: benchmark --api http://localhost:9000 --task agent_eval --iterations 10")
		os.Exit(1)
	}

	log.Printf("Starting benchmark: task=%s, iterations=%d, api=%s", *taskType, *iterations, *apiURL)

	results := []BenchmarkResult{}
	totalScore := 0.0
	totalLatency := 0
	totalCost := 0
	passed := 0

	for i := 0; i < *iterations; i++ {
		log.Printf("Running iteration %d/%d...", i+1, *iterations)

		// Build task payload
		payload := map[string]interface{}{
			"type":  *taskType,
			"model": "llama3",
		}

		// Add task-specific args
		switch *taskType {
		case "agent_train":
			payload["iterations"] = 5
		case "agent_eval":
			payload["iterations"] = 10
			payload["batch_size"] = 20
		case "agent_optimize":
			payload["iterations"] = 20
		case "rlhf":
			payload["iterations"] = 10
		case "batch_infer":
			payload["batch_size"] = 50
		default:
			payload["task"] = "Benchmark test task"
		}

		// Run task
		start := time.Now()
		result, err := runTask(*apiURL, payload, *timeout)
		latency := int(time.Since(start).Milliseconds())

		if err != nil {
			log.Printf("  ❌ Iteration %d failed: %v", i+1, err)
			results = append(results, BenchmarkResult{
				Commit:    getCommit(),
				TaskType:  *taskType,
				Score:     0,
				LatencyMs: latency,
				Cost:      0,
				Success:   false,
				Timestamp: time.Now().Format(time.RFC3339),
			})
			continue
		}

		// Extract metrics
		score := 0.0
		if result["score"] != nil {
			if s, ok := result["score"].(float64); ok {
				score = s
			}
		}

		cost := 0
		if result["cost"] != nil {
			if c, ok := result["cost"].(float64); ok {
				cost = int(c)
			}
		}

		results = append(results, BenchmarkResult{
			Commit:    getCommit(),
			TaskType:  *taskType,
			Score:     score,
			LatencyMs: latency,
			Cost:      cost,
			Success:   true,
			Timestamp: time.Now().Format(time.RFC3339),
		})

		totalScore += score
		totalLatency += latency
		totalCost += cost
		passed++

		log.Printf("  ✅ Score: %.2f, Latency: %dms", score, latency)
	}

	// Calculate averages
	avgScore := 0.0
	avgLatency := 0
	avgCost := 0
	if passed > 0 {
		avgScore = totalScore / float64(passed)
		avgLatency = totalLatency / passed
		avgCost = totalCost / passed
	}

	resultSet := BenchmarkResultSet{
		Commit:      getCommit(),
		Timestamp:   time.Now().Format(time.RFC3339),
		TotalTasks:  *iterations,
		PassedTasks:  passed,
		FailedTasks: *iterations - passed,
		AvgScore:    avgScore,
		AvgLatency:  avgLatency,
		AvgCost:     avgCost,
		Results:     results,
	}

	// Write results
	data, _ := json.MarshalIndent(resultSet, "", "  ")
	os.WriteFile(*output, data, 0644)

	log.Printf("\n📊 Benchmark Results:")
	log.Printf("   Tasks: %d/%d passed", passed, *iterations)
	log.Printf("   Avg Score: %.2f", avgScore)
	log.Printf("   Avg Latency: %dms", avgLatency)
	log.Printf("   Avg Cost: %d credits", avgCost)
	log.Printf("   Results saved to: %s", *output)
}

func runTask(apiURL string, payload map[string]interface{}, timeout int) (map[string]interface{}, error) {
	body, _ := json.Marshal(payload)

	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	resp, err := client.Post(apiURL+"/agent", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result["error"] != nil {
		return nil, fmt.Errorf("task error: %v", result["error"])
	}

	return result, nil
}

func getCommit() string {
	return time.Now().Format("20060102150405")
}
