package main

import (
	"context"
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
	"github.com/dp229/openpool/pkg/p2p"
)

var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5cb85c"))
	blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("#5bc0de"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#f0ad4e"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("#d9534f"))
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("#337ab7"))
)

var (
	flagPort      = flag.Int("port", 9000, "libp2p TCP port to listen on")
	flagConnect   = flag.String("connect", "", "Connect to a peer (multiaddr)")
	flagBootstrap = flag.String("bootstrap", "", "Connect to bootstrap peer (multiaddr)")
	flagDB        = flag.String("db", "openpool.db", "SQLite ledger path")
	flagCredits   = flag.Int("credits", 100, "Starting credits")
	flagHTTP      = flag.Bool("no-http", false, "Disable HTTP API")
	flagTest      = flag.Bool("test", false, "Run built-in test task")
	flagSend      = flag.String("send", "", "Send task to peer (multiaddr with /p2p/)")
	flagTaskFile  = flag.String("task", "", "Task JSON file")
	flagDHT       = flag.Bool("dht", false, "Enable DHT peer discovery")
)

func main() {
	flag.Parse()

	db, err := ledger.New(*flagDB)
	if err != nil {
		log.Fatal("Ledger error:", err)
	}

	node := p2p.NewNode(db)
	node.Port = *flagPort

	if err := node.Listen(*flagPort); err != nil {
		log.Fatal("libp2p listen error:", err)
	}

	nodeID := node.ID()
	db.AddCredits(nodeID, *flagCredits)
	log.SetPrefix(fmt.Sprintf("[%s] ", nodeID[:6]))
	fmt.Printf("%s Ledger: %s | %d credits\n", green.Render("✓"), nodeID[:6], *flagCredits)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *flagTest {
		task := &p2p.Task{
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

		fmt.Printf("%s Task %s → %s\n", blue.Render("→"), task.ID[:8], *flagSend)
		if err := node.SubmitTask(ctx, *flagSend, &task); err != nil {
			log.Fatalf("%s Submit failed: %v", red.Render("✗"), err)
		}
		<-ctx.Done()
		os.Exit(0)
	}

	if *flagBootstrap != "" {
		node.Bootstrap([]string{*flagBootstrap})
	}
	if *flagConnect != "" {
		if err := node.Connect(ctx, *flagConnect); err != nil {
			fmt.Printf("%s Connect: %v\n", yellow.Render("⚠"), err)
		}
	}

	if *flagDHT {
		bootstrapAddrs := []string{}
		if *flagBootstrap != "" {
			bootstrapAddrs = append(bootstrapAddrs, *flagBootstrap)
		}
		if err := node.StartDHT(bootstrapAddrs); err != nil {
			log.Printf("DHT start: %v", err)
		} else {
			if err := node.AdvertiseCapabilities(ctx, []p2p.CapabilityNamespace{p2p.CapCPU}); err != nil {
				log.Printf("Advertise: %v", err)
			}
		}
	}

	if !*flagHTTP {
		go serveHTTP(node, db)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Printf("%s Shutting down...\n", yellow.Render("■"))
	cancel()
	node.Close()
}

func serveHTTP(n *p2p.Node, db *ledger.Ledger) {
	mux := http.NewServeMux()

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"node_id":     n.ID(),
			"port":        n.Port,
			"credits":     db.GetCredits(n.ID()),
			"cpu_cores":   runtime.NumCPU(),
			"ram_free_mb": getFreeRAM(),
			"peers":       n.PeerCount(),
			"multiaddrs":  n.Multiaddrs(),
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
		ctx := context.Background()
		if err := n.Connect(ctx, req.Address); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "connected", "address": req.Address})
	})

	addr := fmt.Sprintf(":%d", n.Port+1)
	fmt.Printf("%s HTTP API: http://localhost%s/\n", cyan.Render("🌐"), addr)

	http.ListenAndServe(addr, mux)
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
