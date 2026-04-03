package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dp229/openpool/pkg/capabilities"
	"github.com/dp229/openpool/pkg/executor"
	"github.com/dp229/openpool/pkg/gpu"
	"github.com/dp229/openpool/pkg/ledger"
	"github.com/dp229/openpool/pkg/marketplace"
	"github.com/dp229/openpool/pkg/metrics"
	"github.com/dp229/openpool/pkg/p2p"
	"github.com/dp229/openpool/pkg/queue"
	"github.com/dp229/openpool/pkg/scheduler"
	"github.com/dp229/openpool/pkg/verification"
	"github.com/dp229/openpool/pkg/wasm"
)

var (
	flagPort       = flag.Int("port", 9000, "TCP port to listen on")
	flagBootstrap  = flag.String("bootstrap", "", "Bootstrap peer multiaddr (repeat for multiple)")
	flagLedger     = flag.String("ledger", "openpool.db", "SQLite ledger path")
	flagCredits    = flag.Int("credits", 100, "Starting credits")
	flagHTTP       = flag.Int("http", 0, "HTTP API port (0=disabled)")
	flagTest       = flag.Bool("test", false, "Run built-in test task locally")
	flagSend       = flag.String("send", "", "Send task to peer ID (use --connect first)")
	flagTaskFile   = flag.String("task", "", "Task JSON file")
	flagInfo       = flag.Bool("info", false, "Print node info and exit")
	flagDHT        = flag.Bool("dht", false, "Enable DHT peer discovery")
	flagDiscover   = flag.Bool("discover", false, "Discover peers via DHT (implies --dht)")
	flagMaxPeers   = flag.Int("max-peers", 5, "Max peers to discover via DHT")
	flagConnect    = flag.String("connect", "", "Connect to a peer multiaddr")
	flagChunked    = flag.Int("chunked", 0, "Split task into N chunks across peers")
	flagWASM       = flag.String("wasm", "", "WASM module path for local execution")
	flagPeerstore  = flag.String("peerstore", "", "Path to peerstore JSON file for persistence")
	flagVerify     = flag.Bool("verify", true, "Enable task verification")
	flagMarket     = flag.Bool("market", false, "Enable task marketplace")
	flagPrice      = flag.Int("price", 10, "Price per task (credits)")
	flagGPU        = flag.Bool("gpu", false, "Enable GPU execution")
	flagNodeID     = flag.String("node-id", "", "Node ID (auto-generated if not set)")
	flagNodeIDFile = flag.String("node-id-file", "", "Path to persist node ID")
)

func main() {
	flag.Parse()

	nodeIDFile := *flagNodeIDFile
	if nodeIDFile == "" && *flagLedger != "" {
		nodeIDFile = *flagLedger + ".nodeid"
	}

	var nodeID string
	if *flagNodeID != "" {
		nodeID = *flagNodeID
	} else if nodeIDFile != "" {
		if data, err := os.ReadFile(nodeIDFile); err == nil {
			nodeID = strings.TrimSpace(string(data))
		}
	}

	if nodeID == "" {
		idBytes := make([]byte, 8)
		rand.Read(idBytes)
		nodeID = hex.EncodeToString(idBytes)
		if nodeIDFile != "" {
			os.WriteFile(nodeIDFile, []byte(nodeID), 0600)
		}
	}
	log.SetPrefix(fmt.Sprintf("[%s] ", nodeID[:6]))

	// Ledger
	db, err := ledger.New(*flagLedger)
	if err != nil {
		log.Fatal("Ledger error:", err)
	}

	existingCredits := db.GetCredits(nodeID)
	if existingCredits == 0 {
		db.AddCredits(nodeID, *flagCredits)
		fmt.Printf("✓ Ledger: %s | %d credits (initialized)\n", nodeID[:6], *flagCredits)
	} else {
		fmt.Printf("✓ Ledger: %s | %d credits\n", nodeID[:6], existingCredits)
	}

	// Create libp2p node
	node := p2p.NewNode(db)
	node.ID = nodeID
	node.PeerstorePath = *flagPeerstore

	if err := node.Listen(*flagPort); err != nil {
		log.Fatal("Listen error:", err)
	}

	// Print peer info
	fmt.Println("\n🔗 Share this to connect:")
	for _, addr := range node.Multiaddrs() {
		fmt.Printf("  %s\n", addr)
	}
	fmt.Println()

	// Bootstrap peers
	var bootstrapAddrs []string
	if *flagBootstrap != "" {
		for _, addr := range strings.Split(*flagBootstrap, ",") {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				bootstrapAddrs = append(bootstrapAddrs, addr)
			}
		}
		if len(bootstrapAddrs) > 0 {
			fmt.Printf("→ Connecting to %d bootstrap peer(s)...\n", len(bootstrapAddrs))
			node.Bootstrap(bootstrapAddrs)
		}
	}

	// DHT peer discovery
	enableDHT := *flagDHT || *flagDiscover
	if enableDHT {
		fmt.Println("→ Starting DHT client...")
		if err := node.StartDHT(bootstrapAddrs); err != nil {
			fmt.Printf("⚠ DHT start failed: %v\n", err)
		} else {
			fmt.Println("✓ DHT client ready")
		}

		// Discover peers immediately
		if *flagDiscover {
			go discoverPeers(node, *flagMaxPeers)
		}
	}

	// Direct connect
	if *flagConnect != "" {
		fmt.Printf("→ Connecting to: %s\n", *flagConnect)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := node.Connect(ctx, *flagConnect); err != nil {
			fmt.Printf("⚠ Connect failed: %v\n", err)
		} else {
			fmt.Println("✓ Connected")
		}
		cancel()
	}

	// Info mode
	if *flagInfo {
		fmt.Printf("ID:        %s\n", node.ID)
		fmt.Printf("Multiaddr: %s\n", node.PeerInfo())
		fmt.Printf("Credits:   %d\n", db.GetCredits(nodeID))
		fmt.Printf("CPU:       %d cores\n", runtime.NumCPU())
		fmt.Printf("RAM:       %d MB free\n", getFreeRAM())
		fmt.Printf("DHT:       %v\n", enableDHT)
		fmt.Printf("Connected peers: %d\n", len(node.Host.Network().Peers()))
		os.Exit(0)
	}

	// Test mode
	if *flagTest {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		task := &p2p.Task{ID: "builtin-test", TimeoutSec: 15, Credits: 10}
		result, err := runTask(ctx, task)
		if err != nil {
			fmt.Printf("✗ Test failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Result:\n%s\n", string(result))
		db.AddCredits(nodeID, 10)
		fmt.Printf("💰 +10 credits | balance: %d\n", db.GetCredits(nodeID))
		os.Exit(0)
	}

	// Chunked task submission (MapReduce)
	if *flagChunked > 0 && *flagTaskFile != "" {
		data, err := os.ReadFile(*flagTaskFile)
		if err != nil {
			log.Fatal("Task file:", err)
		}
		var task p2p.Task
		json.Unmarshal(data, &task)
		if task.ID == "" {
			task.ID = nodeID + "-chunked-task"
		}
		task.Credits = 10
		task.State = "pending"
		task.CreatedAt = time.Now()

		sched := scheduler.New(node, nodeID)

		// MapReduce: split fib(1M) into N chunks, reduce by summing
		mr := scheduler.MapReduce{
			Split: func(t *p2p.Task) ([]scheduler.Chunk, error) {
				total := 20000 // 20k total, split into chunks
				chunkSize := total / *flagChunked
				var chunks []scheduler.Chunk
				for i := 0; i < *flagChunked; i++ {
					start := i * chunkSize
					end := start + chunkSize
					if i == *flagChunked-1 {
						end = total
					}
					params, _ := json.Marshal(map[string]interface{}{
						"type": "range", "start": start, "end": end,
					})
					chunks = append(chunks, scheduler.Chunk{
						ID: fmt.Sprintf("%s-chunk-%d", t.ID, i), TaskID: t.ID, Index: i,
						Params: params, Credits: 10, Timeout: 60,
					})
				}
				return chunks, nil
			},
			Reduce: func(results []scheduler.ChunkResult) (json.RawMessage, error) {
				var total int64
				success := 0
				var errors []string
				for _, r := range results {
					fmt.Printf("DEBUG Reduce: chunkID=%s success=%v data_len=%d\n", r.ChunkID, r.Success, len(r.Data))
					if r.Success && len(r.Data) > 0 {
						var p map[string]interface{}
						if err := json.Unmarshal(r.Data, &p); err != nil {
							fmt.Printf("DEBUG: unmarshal failed: %v\n", err)
						} else if sumStr, ok := p["chunk_sum"].(string); ok {
							// Parse large integer from string (approximate via float for demo)
							var sum float64
							fmt.Sscanf(sumStr, "%f", &sum)
							total += int64(sum)
							success++
						} else if sumNum, ok := p["chunk_sum"].(float64); ok {
							total += int64(sumNum)
							success++
						}
					} else {
						errors = append(errors, r.Error)
					}
				}
				out, _ := json.Marshal(map[string]interface{}{
					"sum_squares": total, "status": "completed",
					"chunks_success": success, "chunks_total": len(results),
					"errors": errors, "parallelism": *flagChunked,
				})
				return out, nil
			},
		}

		fmt.Printf("→ Chunking into %d parts...\n", *flagChunked)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		result, err := sched.SubmitChunked(ctx, &task, mr)
		if err != nil {
			fmt.Printf("✗ Failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Done! Result:\n%s\n", string(result.Result))
		os.Exit(0)
	}

	// Single task submission
	if *flagSend != "" && *flagTaskFile != "" {
		data, err := os.ReadFile(*flagTaskFile)
		if err != nil {
			log.Fatal("Task file:", err)
		}
		var task p2p.Task
		json.Unmarshal(data, &task)
		if task.ID == "" {
			task.ID = nodeID + "-task"
		}
		task.Credits = 10
		task.State = "pending"
		task.CreatedAt = time.Now()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		peerID := extractPeerID(*flagSend)
		peerID = strings.TrimPrefix(peerID, "/p2p/")
		if i := strings.Index(peerID, "/"); i >= 0 {
			peerID = peerID[:i]
		}

		fmt.Printf("→ Task %s → %s\n", task.ID[:8], peerID[:16])
		if err := node.SubmitTask(ctx, peerID, &task); err != nil {
			fmt.Printf("✗ Failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Done! Result:\n%s\n", string(task.Result))
		fmt.Printf("Credits: %d\n", db.GetCredits(nodeID))
		os.Exit(0)
	}

	// Verifier (independent of WASM)
	var v *verification.Verifier
	if *flagVerify {
		v, err = verification.NewWithDefaults(*flagLedger)
		if err != nil {
			log.Printf("⚠ Verifier init error: %v (continuing without)", err)
			v = nil
		} else {
			log.Printf("✓ Task verifier ready")
		}
	}

	// WASM executor (optional)
	var exec *executor.Executor
	if *flagWASM != "" {
		r, err := wasm.New()
		if err != nil {
			log.Printf("WASM init error: %v", err)
		} else {
			exec = executor.New(r, db, v)
			exec.SetNodeID(nodeID)
			log.Printf("✓ WASM executor ready (native mode)")
		}
	}

	// GPU Execution
	var gpupool *gpu.Pool
	if *flagGPU {
		gpupool = gpu.New()
		if err := gpupool.Detect(); err != nil {
			log.Printf("⚠ GPU detection: %v (running in CPU mode)", err)
		} else {
			devs := gpupool.Devices()
			log.Printf("✓ GPU enabled: %d device(s)", len(devs))
			for _, d := range devs {
				log.Printf("  - %s (%s, %dMB VRAM)", d.Name, d.Vendor, d.VRAMMB)
			}
		}
	}

	// Marketplace
	var market *marketplace.Marketplace
	if *flagMarket {
		market, err = marketplace.New(*flagLedger, nodeID)
		if err != nil {
			log.Printf("⚠ Marketplace init error: %v", err)
		} else {
			multiaddr := ""
			if len(node.Multiaddrs()) > 0 {
				multiaddr = node.Multiaddrs()[0]
			}

			hwInfo, err := capabilities.Detect()
			if err != nil {
				log.Printf("⚠ Hardware detection error: %v", err)
			}

			caps := marketplace.NodeCapabilities{
				CPUCores:    hwInfo.CPU.Cores,
				CPUArch:     hwInfo.CPU.Arch,
				RAMGB:       int(hwInfo.Memory.TotalGB),
				StorageGB:   int(hwInfo.Storage.TotalGB),
				WASMEnabled: *flagWASM != "",
			}
			if *flagGPU && gpupool != nil && gpupool.IsEnabled() {
				devs := gpupool.Devices()
				if len(devs) > 0 {
					caps.GPU = &marketplace.GPU{
						Present: true,
						Model:   devs[0].Name,
						VRAMGB:  devs[0].VRAMMB / 1024,
					}
				}
			}

			market.RegisterNode(marketplace.NodeInfo{
				NodeID:       nodeID,
				Multiaddr:    multiaddr,
				Capabilities: caps,
				Country:      hwInfo.Network.Country,
				City:         hwInfo.Network.City,
				PricePerTask: *flagPrice,
				Status:       "online",
			})
			log.Printf("✓ Marketplace enabled (price: %d credits/task)", *flagPrice)
			log.Printf("  Hardware: %d cores, %dGB RAM, %dGB storage", caps.CPUCores, caps.RAMGB, caps.StorageGB)
		}
	}

	// Task Queue (always enabled)
	taskQueue := queue.NewWorkerPool(100, runtime.NumCPU())
	if *flagWASM != "" {
		taskQueue.SetExecutor(func(ctx context.Context, task *queue.Task) (json.RawMessage, error) {
			if exec == nil {
				return nil, fmt.Errorf("no WASM runtime")
			}
			execTask := &executor.Task{
				ID:         task.ID,
				NodeID:     nodeID,
				WASMPath:   *flagWASM,
				RawInput:   task.Input,
				TimeoutSec: task.Timeout,
				Credits:    task.Credits,
			}
			result, err := exec.Execute(ctx, execTask)
			if err != nil {
				return nil, err
			}
			return result.Result, nil
		})
		log.Printf("✓ Task queue ready (%d workers, 100 task depth)", runtime.NumCPU())
	}

	// Metrics collector
	mc := metrics.NewCollector()
	mc.Set("openpool_cpu_cores", "Number of CPU cores", float64(runtime.NumCPU()), nil)
	mc.Set("openpool_free_ram_mb", "Available RAM in MB", float64(getFreeRAM()), nil)

	// HTTP API
	if *flagHTTP > 0 {
		go serveHTTP(node, db, nodeID, exec, v, *flagHTTP, market, gpupool, taskQueue, mc)
	}

	// Wait for shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Println("■ Shutting down...")
	node.Close()
}

// discoverPeers queries the DHT for peers and prints what it finds.
func discoverPeers(node *p2p.Node, maxPeers int) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("→ Discovering up to %d peers via DHT...\n", maxPeers)

	// DHT GetClosestPeers returns peers closest to the key
	peerIDs, err := node.FindPeers(ctx, maxPeers)
	if err != nil {
		fmt.Printf("⚠ DHT query failed: %v\n", err)
		return
	}

	connected := node.Host.Network().Peers()
	if len(peerIDs) == 0 && len(connected) == 0 {
		fmt.Println("⚠ No peers found — network may need bootstrap peers")
		return
	}

	if len(peerIDs) > 0 {
		fmt.Printf("✓ Found %d peer(s) via DHT:\n", len(peerIDs))
		for _, pid := range peerIDs {
			fmt.Printf("  • %s\n", pid.String())
		}
	}

	if len(connected) > 0 {
		fmt.Printf("✓ %d connected peer(s):\n", len(connected))
		for _, p := range connected {
			fmt.Printf("  • %s\n", p.String())
		}
	}
}

func extractPeerID(addr string) string {
	if i := strings.Index(addr, "/p2p/"); i >= 0 {
		rest := addr[i+len("/p2p/"):]
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	// Also handle bare peer IDs
	if len(addr) == 52 && strings.HasPrefix(addr, "Qm") {
		return addr
	}
	return addr
}

func runTask(ctx context.Context, task *p2p.Task) (json.RawMessage, error) {
	script := `import json
def fib(n):
    a,b=0,1
    for _ in range(n): a,b=b,a+b
    return a
result={"fib_20":fib(20),"fib_35":fib(35),"status":"ok","runtime":"python3","node":"openpool"}
print(json.dumps(result))`
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("python: %v | %s", err, string(out))
	}
	return json.RawMessage(out), nil
}

func getFreeRAM() int {
	data, _ := os.ReadFile("/proc/meminfo")
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.Atoi(fields[1])
				return kb / 1024
			}
		}
	}
	return 0
}

func serveHTTP(node *p2p.Node, db *ledger.Ledger, nodeID string, exec *executor.Executor, v *verification.Verifier, port int, market *marketplace.Marketplace, gpupool *gpu.Pool, taskQueue *queue.WorkerPool, mc *metrics.Collector) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"node_id":         node.ID,
			"peer_info":       node.PeerInfo(),
			"multiaddrs":      node.Multiaddrs(),
			"credits":         db.GetCredits(node.ID),
			"cpu_cores":       runtime.NumCPU(),
			"ram_mb":          getFreeRAM(),
			"connected_peers": len(node.Host.Network().Peers()),
		})
	})
	mux.HandleFunc("/ledger", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(db.GetAll())
	})
	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		var req struct {
			Address string `json:"address"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := node.Connect(ctx, req.Address)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "connected"})
	})
	mux.HandleFunc("/discover", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		peers, _ := node.FindPeers(ctx, 10)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"peer_count": len(peers),
			"peers":      peers,
		})
	})

	// Task execution endpoint
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		if exec == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "no WASM runtime"})
			return
		}
		// Read raw JSON body
		var rawReq json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&rawReq); err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}

		// Parse for credits (optional)
		var req struct {
			Op         string `json:"op"`
			Arg        int    `json:"arg"`
			Credits    int    `json:"credits"`
			TimeoutSec int    `json:"timeout_sec"`
		}
		if err := json.Unmarshal(rawReq, &req); err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}

		timeoutSec := req.TimeoutSec
		if timeoutSec == 0 {
			timeoutSec = 30
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
		defer cancel()

		// Create task with raw input
		task := &executor.Task{
			ID:         "http-" + time.Now().Format("150405.000"),
			NodeID:     nodeID,
			WASMPath:   *flagWASM,
			RawInput:   rawReq,
			TimeoutSec: timeoutSec,
			Credits:    req.Credits,
		}

		result, err := exec.Execute(ctx, task)
		if err != nil {
			mc.Inc("openpool_tasks_failed_total", "Total failed tasks", map[string]string{"op": req.Op})
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}

		mc.Inc("openpool_tasks_completed_total", "Total completed tasks", map[string]string{"op": req.Op})
		mc.Observe("openpool_task_duration_ms", "Task execution duration", float64(result.DurationMs), map[string]string{"op": req.Op})

		// Deduct credits
		if task.Credits > 0 {
			db.AddCredits(nodeID, -task.Credits)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":           "ok",
			"result":           string(result.Result),
			"credits_deducted": task.Credits,
			"duration_ms":      result.DurationMs,
			"verified":         result.Verified,
		})
	})
	fmt.Printf("🌐 HTTP API: http://localhost:%d/\n", port)
	fmt.Printf("   Endpoints:\n")
	fmt.Printf("   - GET  /status    - Node status\n")
	fmt.Printf("   - GET  /ledger   - Credit ledger\n")
	fmt.Printf("   - POST /connect  - Connect to peer\n")
	fmt.Printf("   - GET  /discover - Discover peers\n")
	fmt.Printf("   - POST /submit   - Submit task\n")
	fmt.Printf("   - POST /run     - Run task locally (no P2P)\n")
	fmt.Printf("   - GET  /verify  - Get verification history\n")
	fmt.Printf("   - GET  /stats   - Node reliability stats\n")

	// Verification history endpoint
	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		if v == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "verification disabled"})
			return
		}
		taskID := r.URL.Query().Get("task_id")
		if taskID == "" {
			// Return all verification history
			stats, err := v.GetAllNodeStats()
			if err != nil {
				json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"node_stats": stats})
			return
		}
		results, err := v.GetVerificationHistory(taskID)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"task_id": taskID, "results": results})
	})

	// Slashing history endpoint
	mux.HandleFunc("/slashing", func(w http.ResponseWriter, r *http.Request) {
		if v == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "verification disabled"})
			return
		}
		nodeID := r.URL.Query().Get("node_id")
		if nodeID == "" {
			nodeID = nodeID
		}
		results, err := v.GetSlashingHistory(nodeID)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"node_id": nodeID, "slashing_events": results})
	})

	// Slashing history endpoint
	mux.HandleFunc("/slashing", func(w http.ResponseWriter, r *http.Request) {
		if v == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "verification disabled"})
			return
		}
		nodeID := r.URL.Query().Get("node_id")
		if nodeID == "" {
			nodeID = nodeID
		}
		results, err := v.GetSlashingHistory(nodeID)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"node_id": nodeID, "slashing_events": results})
	})

	// Node stats endpoint
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		if market == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "marketplace not enabled"})
			return
		}

		nodes, err := market.GetNodes()
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"node_id":         nodeID,
			"marketplace":     "enabled",
			"available_nodes": len(nodes),
		})
	})

	// GPU stats endpoint
	mux.HandleFunc("/gpu", func(w http.ResponseWriter, r *http.Request) {
		if gpupool == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"devices": []interface{}{},
				"message": "Use --gpu flag to enable GPU execution",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled": gpupool.IsEnabled(),
			"devices": gpupool.Devices(),
		})
	})

	// GPU execute endpoint
	mux.HandleFunc("/gpu/run", func(w http.ResponseWriter, r *http.Request) {
		if gpupool == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "GPU not enabled"})
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}

		var req struct {
			Op    string          `json:"op"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		result, err := gpupool.Execute(context.Background(), req.Op, req.Input)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(result)
	})

	// Marketplace: List available nodes
	mux.HandleFunc("/nodes", func(w http.ResponseWriter, r *http.Request) {
		if market == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "marketplace not enabled"})
			return
		}
		nodes, err := market.GetNodes()
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"nodes": nodes, "count": len(nodes)})
	})

	// Marketplace: Publish task
	mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		if market == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "marketplace not enabled"})
			return
		}

		if r.Method == http.MethodPost {
			var task marketplace.TaskListing
			if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if err := market.PublishTask(task); err != nil {
				json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"status": "ok", "task_id": task.TaskID})
			return
		}

		// GET: List tasks
		status := r.URL.Query().Get("status")
		tasks, err := market.GetTasks(status)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"tasks": tasks, "count": len(tasks)})
	})

	// Marketplace: Get task result
	mux.HandleFunc("/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if market == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "marketplace not enabled"})
			return
		}

		taskID := r.URL.Path[len("/tasks/"):]
		task, err := market.GetTask(taskID)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(task)
	})

	// Bidding: Place a bid on a task
	mux.HandleFunc("/bids", func(w http.ResponseWriter, r *http.Request) {
		if market == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "marketplace not enabled"})
			return
		}

		if r.Method == http.MethodPost {
			var req struct {
				TaskID   string `json:"task_id"`
				NodeID   string `json:"node_id"`
				NodeAddr string `json:"node_addr"`
				Credits  int    `json:"credits"`
				ETASec   int    `json:"eta_sec"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}

			bid, err := market.PlaceBid(context.Background(), req.TaskID, req.NodeID, req.NodeAddr, req.Credits, req.ETASec)
			if err != nil {
				json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "bid": bid})
			return
		}

		// GET: List bids for a task
		taskID := r.URL.Query().Get("task_id")
		if taskID == "" {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "task_id required"})
			return
		}

		bids, err := market.GetBidsForTask(taskID)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"bids": bids, "count": len(bids)})
	})

	// Bidding: Get winning bid for a task
	mux.HandleFunc("/bids/", func(w http.ResponseWriter, r *http.Request) {
		if market == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "marketplace not enabled"})
			return
		}

		taskID := r.URL.Path[len("/bids/"):]

		// Get winning bid
		bid, err := market.GetWinningBid(taskID)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"winning_bid": bid})
	})

	// Bidding: Auto-match best bid
	mux.HandleFunc("/match", func(w http.ResponseWriter, r *http.Request) {
		if market == nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "marketplace not enabled"})
			return
		}

		taskID := r.URL.Query().Get("task_id")
		if taskID == "" {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "task_id required"})
			return
		}

		bid, err := market.AutoMatch(taskID)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "matched_bid": bid})
	})
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}

		var req struct {
			Op  string `json:"op"`
			Arg int    `json:"arg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		opID := wasm.OpToID(req.Op)
		if opID < 0 {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "unknown op"})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Use executor if available, otherwise create new runtime
		var result []byte
		var err error

		if exec != nil && exec.Runtime() != nil {
			result, err = exec.Runtime().Run(ctx, opID, req.Arg)
		} else {
			// Create runtime on-the-fly
			rt, err := wasm.New()
			if err != nil {
				json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
				return
			}
			defer rt.Close(ctx)
			result, err = rt.Run(ctx, opID, req.Arg)
		}

		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(result)
	})

	// Large payload endpoint - streams POST body without URL argument limits
	mux.HandleFunc("/run/large", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}

		// Limit body to 100MB
		r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

		var req struct {
			Op         string          `json:"op"`
			Input      json.RawMessage `json:"input"`
			Credits    int             `json:"credits"`
			TimeoutSec int             `json:"timeout_sec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON or payload too large (max 100MB): "+err.Error(), 400)
			return
		}

		timeoutSec := req.TimeoutSec
		if timeoutSec == 0 {
			timeoutSec = 60
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
		defer cancel()

		task := &queue.Task{
			ID:       "large-" + time.Now().Format("150405.000"),
			Op:       req.Op,
			Input:    req.Input,
			Credits:  req.Credits,
			Timeout:  timeoutSec,
			Priority: 1,
		}

		result, err := taskQueue.DequeueAndRun(ctx, task)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}

		if req.Credits > 0 {
			db.AddCredits(nodeID, -req.Credits)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":           "ok",
			"result_size":      len(result),
			"credits_deducted": req.Credits,
		})
	})

	// Queue status endpoint
	mux.HandleFunc("/queue", func(w http.ResponseWriter, r *http.Request) {
		stats := taskQueue.Stats()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"workers":   runtime.NumCPU(),
			"max_queue": 100,
			"stats":     stats,
		})
	})

	// Hardware info endpoint
	mux.HandleFunc("/hardware", func(w http.ResponseWriter, r *http.Request) {
		hwInfo, err := capabilities.Detect()
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hwInfo)
	})

	// Prometheus metrics endpoint
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if mc == nil {
			http.Error(w, "metrics not enabled", 503)
			return
		}

		// Update dynamic gauges
		mc.Set("openpool_free_ram_mb", "Available RAM in MB", float64(getFreeRAM()), nil)
		mc.Set("openpool_connected_peers", "Number of connected peers", float64(len(node.Host.Network().Peers())), nil)
		mc.Set("openpool_credits", "Current node credits", float64(db.GetCredits(nodeID)), nil)

		if taskQueue != nil {
			stats := taskQueue.Stats()
			if queueSize, ok := stats["queue_size"].(int); ok {
				mc.Set("openpool_queue_size", "Current queue depth", float64(queueSize), nil)
			}
			if workers, ok := stats["workers"].(int); ok {
				mc.Set("openpool_workers", "Number of workers", float64(workers), nil)
			}
			if running, ok := stats["running"].(bool); ok {
				val := 0.0
				if running {
					val = 1.0
				}
				mc.Set("openpool_queue_running", "Whether queue is running", val, nil)
			}
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Write([]byte(mc.FormatPrometheus()))
	})

	addr := fmt.Sprintf(":%d", port)

	// Serve static UI files
	fs := http.FileServer(http.Dir("ui"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			fs.ServeHTTP(w, r)
			return
		}
		// Check if file exists, otherwise serve index.html for SPA
		http.ServeFile(w, r, "ui"+r.URL.Path)
	})

	server := &http.Server{Addr: addr, Handler: mux}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Printf("⚠ HTTP server error: %v\n", err)
	}
}
