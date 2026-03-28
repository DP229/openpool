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

	"github.com/dp229/openpool/pkg/ledger"
	"github.com/dp229/openpool/pkg/p2p"
)

var (
	flagPort      = flag.Int("port", 9000, "TCP port to listen on")
	flagBootstrap = flag.String("bootstrap", "", "Bootstrap peer multiaddr (repeat for multiple)")
	flagLedger    = flag.String("ledger", "openpool.db", "SQLite ledger path")
	flagCredits   = flag.Int("credits", 100, "Starting credits")
	flagHTTP      = flag.Int("http", 0, "HTTP API port (0=disabled)")
	flagTest      = flag.Bool("test", false, "Run built-in test task locally")
	flagSend      = flag.String("send", "", "Send task to peer ID (use --connect first)")
	flagTaskFile  = flag.String("task", "", "Task JSON file")
	flagInfo      = flag.Bool("info", false, "Print node info and exit")
	flagDHT       = flag.Bool("dht", false, "Enable DHT peer discovery")
	flagDiscover  = flag.Bool("discover", false, "Discover peers via DHT (implies --dht)")
	flagMaxPeers  = flag.Int("max-peers", 5, "Max peers to discover via DHT")
	flagConnect   = flag.String("connect", "", "Connect to a peer multiaddr")
)

func main() {
	flag.Parse()

	// Node ID
	idBytes := make([]byte, 8)
	rand.Read(idBytes)
	nodeID := hex.EncodeToString(idBytes)
	log.SetPrefix(fmt.Sprintf("[%s] ", nodeID[:6]))

	// Ledger
	db, err := ledger.New(*flagLedger)
	if err != nil {
		log.Fatal("Ledger error:", err)
	}
	db.AddCredits(nodeID, *flagCredits)
	fmt.Printf("✓ Ledger: %s | %d credits\n", nodeID[:6], *flagCredits)

	// Create libp2p node
	node := p2p.NewNode(db)
	node.ID = nodeID

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

	// Send task to peer
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
		// Strip any multiaddr prefix if full multiaddr was pasted
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

	// HTTP API
	if *flagHTTP > 0 {
		go serveHTTP(node, db, *flagHTTP)
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

func serveHTTP(node *p2p.Node, db *ledger.Ledger, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"node_id":         node.ID,
			"peer_info":      node.PeerInfo(),
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
		var req struct{ Address string `json:"address"` }
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
	fmt.Printf("🌐 HTTP API: http://localhost:%d/\n", port)
	http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
}
