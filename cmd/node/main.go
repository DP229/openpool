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
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/dp229/openpool/pkg/ledger"
	"github.com/dp229/openpool/pkg/net"
)

// ── Styles ──────────────────────────────────────────────────────────────
var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5cb85c"))
	blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("#5bc0de"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#f0ad4e"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("#d9534f"))
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("#337ab7"))
)

// ── Flags ──────────────────────────────────────────────────────────────
var (
	flagPort      = flag.Int("port", 9000, "TCP port to listen on")
	flagConnect   = flag.String("connect", "", "Connect to a peer (ip:port)")
	flagBootstrap = flag.String("bootstrap", "", "Connect to bootstrap peer (ip:port)")
	flagDB        = flag.String("db", "openpool.db", "SQLite ledger path")
	flagCredits   = flag.Int("credits", 100, "Starting credits")
	flagHTTP      = flag.Bool("no-http", false, "Disable HTTP API")
	flagTest      = flag.Bool("test", false, "Run built-in test task")
	flagSend      = flag.String("send", "", "Send task to peer (peer_ip:port)")
	flagTaskFile  = flag.String("task", "", "Task JSON file")
)

func main() {
	flag.Parse()

	// ── Node ID ────────────────────────────────────────────────────────
	idBytes := make([]byte, 8)
	rand.Read(idBytes)
	nodeID := hex.EncodeToString(idBytes)
	log.SetPrefix(fmt.Sprintf("[%s] ", nodeID[:6]))

	// ── Ledger ─────────────────────────────────────────────────────────
	db, err := ledger.New(*flagDB)
	if err != nil {
		log.Fatal("Ledger error:", err)
	}
	db.AddCredits(nodeID, *flagCredits)
	fmt.Printf("%s Ledger: %s | %d credits\n", green.Render("✓"), nodeID[:6], *flagCredits)

	// ── Build Node ────────────────────────────────────────────────────
	node := net.New(*flagPort)
	node.ID = nodeID
	node.DB = db

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Test mode ───────────────────────────────────────────────────
	if *flagTest {
		task := &net.Task{
			ID:         "builtin-test",
			TimeoutSec: 15,
			Credits:    10,
			State:      "pending",
			CreatedAt:  time.Now(),
		}
		result, err := node.RunTask(ctx, task)
		if err != nil {
			fmt.Printf("%s Test failed: %v\n", red.Render("✗"), err)
			os.Exit(1)
		}
		fmt.Printf("%s Result:\n%s\n", green.Render("✓"), string(result))
		db.AddCredits(nodeID, 10)
		fmt.Printf("%s +10 credits earned | balance: %d\n", green.Render("💰"), db.GetCredits(nodeID))
		os.Exit(0)
	}

	// ── Send task to peer ──────────────────────────────────────────
	if *flagSend != "" && *flagTaskFile != "" {
		data, err := os.ReadFile(*flagTaskFile)
		if err != nil {
			log.Fatal("Task file:", err)
		}
		var task net.Task
		json.Unmarshal(data, &task)
		if task.ID == "" {
			task.ID = nodeID + "-task"
		}
		task.Credits = 10
		task.State = "pending"
		task.CreatedAt = time.Now()

		fmt.Printf("%s Task %s → %s\n", blue.Render("→"), task.ID[:8], *flagSend)
		if err := node.SubmitTask(ctx, *flagSend, &task); err != nil {
			log.Fatalf("%s Submit failed: %v", red.Render("✗"), err)
		}
		<-ctx.Done()
		os.Exit(0)
	}

	// ── Start listening ──────────────────────────────────────────────
	if err := node.Listen(ctx); err != nil {
		log.Fatal("Listen error:", err)
	}

	// ── Connect ────────────────────────────────────────────────────
	if *flagBootstrap != "" {
		if err := node.Connect(ctx, *flagBootstrap); err != nil {
			fmt.Printf("%s Bootstrap connect: %v\n", yellow.Render("⚠"), err)
		}
	}
	if *flagConnect != "" {
		if err := node.Connect(ctx, *flagConnect); err != nil {
			fmt.Printf("%s Connect: %v\n", yellow.Render("⚠"), err)
		}
	}

	// ── HTTP API ───────────────────────────────────────────────────
	if !*flagHTTP {
		go serveHTTP(node, db)
	}

	// ── Shutdown ───────────────────────────────────────────────────
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Printf("%s Shutting down...\n", yellow.Render("■"))
	cancel()
	node.Close()
}

// ── HTTP API ───────────────────────────────────────────────────────────

func serveHTTP(n *net.Node, db *ledger.Ledger) {
	mux := http.NewServeMux()

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"node_id":     n.ID,
			"port":       n.Port,
			"credits":    db.GetCredits(n.ID),
			"cpu_cores":  runtime.NumCPU(),
			"ram_free_mb": getFreeRAM(),
			"wasm_ready": true,
			"tasks":     n.Tasks(),
			"peers":     n.PeerCount(),
			"peer_ids":  n.PeerIDs(),
		})
	})

	mux.HandleFunc("/ledger", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(db.GetAll())
	})

	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		var task net.Task
		json.NewDecoder(r.Body).Decode(&task)
		if task.ID == "" {
			idB := make([]byte, 8)
			rand.Read(idB)
			task.ID = hex.EncodeToString(idB)
		}
		task.Credits = 10
		task.State = "pending"
		task.CreatedAt = time.Now()

		peerAddr := r.URL.Query().Get("peer")
		ctx := context.Background()
		if peerAddr != "" {
			if err := n.SubmitTask(ctx, peerAddr, &task); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			fmt.Printf("%s HTTP: task %s → %s\n", blue.Render("→"), task.ID[:8], peerAddr)
		} else {
			result, err := n.RunTask(ctx, &task)
			task.State = "done"
			if err != nil {
				task.State = "failed"
				task.Error = err.Error()
			}
			task.Result = result
			db.AddCredits(n.ID, task.Credits)
			fmt.Printf("%s HTTP: task %s local +%d credits\n", green.Render("✓"), task.ID[:8], task.Credits)
		}
		json.NewEncoder(w).Encode(task)
	})

	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		var req struct{ Address string `json:"address"` }
		json.NewDecoder(r.Body).Decode(&req)
		ctx := context.Background()
		if err := n.Connect(ctx, req.Address); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "connected", "address": req.Address})
	})

	port := n.Port + 1
	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("%s HTTP API: http://localhost:%d/\n", cyan.Render("🌐"), port)
	fmt.Printf("   curl http://localhost:%d/status\n", port)
	fmt.Printf("   curl http://localhost:%d/ledger\n", port)
	fmt.Printf("   curl -X POST http://localhost:%d/submit -d '{\"wasm_path\":\"\"}'\n", port)

	http.ListenAndServe(addr, mux)
}

// ── Helpers ──────────────────────────────────────────────────────────

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
