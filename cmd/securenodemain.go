package main

import (
	"context"
	"crypto/tls"
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

	"github.com/dp229/openpool/pkg/auth"
	"github.com/dp229/openpool/pkg/ledger"
	"github.com/dp229/openpool/pkg/middleware"
	"github.com/dp229/openpool/pkg/p2p"
	"github.com/dp229/openpool/pkg/security"
)

var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5cb85c"))
	blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("#5bc0de"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#f0ad4e"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("#d9534f"))
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("#337ab7"))
)

var (
	flagPort        = flag.Int("port", 9000, "libp2p TCP port to listen on")
	flagHTTP        = flag.Int("http", 8080, "HTTP API port (0 to disable)")
	flagDB          = flag.String("db", "openpool.db", "SQLite ledger path")
	flagCredits     = flag.Int("credits", 100, "Starting credits")
	flagAuthDB      = flag.String("auth-db", "openpool_auth.db", "Auth database path")
	flagAdminSecret = flag.String("admin-secret", "", "Admin secret for API key management")
	flagRequireAuth = flag.Bool("require-auth", false, "Require authentication for API access")
	flagRateLimit   = flag.Int("rate-limit", 100, "Rate limit requests per minute")
	flagTLSCert     = flag.String("tls-cert", "", "TLS certificate file")
	flagTLSKey      = flag.String("tls-key", "", "TLS private key file")
	flagTest        = flag.Bool("test", false, "Run built-in test task")
	flagSend        = flag.String("send", "", "Send task to peer (multiaddr with /p2p/)")
	flagTaskFile    = flag.String("task", "", "Task JSON file")
	flagBootstrap   = flag.String("bootstrap", "", "Bootstrap peer multiaddr")
	flagDHT         = flag.Bool("dht", false, "Enable DHT peer discovery")
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

	authMgr, err := auth.NewManager(*flagAuthDB)
	if err != nil {
		log.Fatal("Auth manager error:", err)
	}
	defer authMgr.Close()

	sanitizer := security.NewSanitizer()
	secMiddleware := middleware.NewSecurityMiddleware(authMgr, *flagRateLimit, *flagAdminSecret, *flagRequireAuth)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *flagBootstrap != "" {
		node.Bootstrap([]string{*flagBootstrap})
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

		if err := sanitizer.ValidateJSON(data); err != nil {
			log.Fatalf("Invalid task JSON: %v", err)
		}

		fmt.Printf("%s Task %s → %s\n", blue.Render("→"), task.ID[:8], *flagSend)
		if err := node.SubmitTask(ctx, *flagSend, &task); err != nil {
			log.Fatalf("%s Submit failed: %v", red.Render("✗"), err)
		}
		<-ctx.Done()
		os.Exit(0)
	}

	if !(*flagHTTP == 0) {
		go serveHTTP(node, db, authMgr, secMiddleware)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Printf("%s Shutting down...\n", yellow.Render("■"))
	cancel()
	node.Close()
}

func serveHTTP(n *p2p.Node, db *ledger.Ledger, authMgr *auth.Manager, secMiddleware *middleware.SecurityMiddleware) {
	mux := http.NewServeMux()

	mux.HandleFunc("/status", secMiddleware.RateLimit(secMiddleware.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"node_id":     n.ID(),
			"port":        n.Port,
			"credits":     db.GetCredits(n.ID()),
			"cpu_cores":   runtime.NumCPU(),
			"ram_free_mb": getFreeRAM(),
			"peers":       n.PeerCount(),
			"multiaddrs":  n.Multiaddrs(),
		})
	})))

	mux.HandleFunc("/ledger", secMiddleware.RateLimit(secMiddleware.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		apiKey, ok := middleware.APIKeyFromContext(r.Context())
		if *flagRequireAuth && !ok {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		if *flagRequireAuth && apiKey.Credits < 1 {
			http.Error(w, "Insufficient credits", http.StatusForbidden)
			return
		}

		json.NewEncoder(w).Encode(db.GetAll())

		if *flagRequireAuth && ok {
			authMgr.UseCredits(apiKey.Key, 1)
		}
	})))

	mux.HandleFunc("/connect", secMiddleware.RateLimit(secMiddleware.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Address string `json:"address"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		ctx := context.Background()
		if err := n.Connect(ctx, req.Address); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "connected", "address": req.Address})
	})))

	mux.HandleFunc("/auth/apikey", secMiddleware.RequireAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			OwnerName  string   `json:"owner_name"`
			OwnerEmail string   `json:"owner_email"`
			Credits    int      `json:"credits"`
			Scopes     []string `json:"scopes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if req.OwnerName == "" || req.OwnerEmail == "" {
			http.Error(w, "owner_name and owner_email required", http.StatusBadRequest)
			return
		}

		if req.Credits == 0 {
			req.Credits = 1000
		}

		if len(req.Scopes) == 0 {
			req.Scopes = []string{"submit", "query"}
		}

		apiKey, err := authMgr.GenerateAPIKey(req.OwnerName, req.OwnerEmail, req.Credits, req.Scopes, 365*24*time.Hour)
		if err != nil {
			http.Error(w, "Failed to generate API key", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         apiKey.ID,
			"api_key":    apiKey.Key,
			"credits":    apiKey.Credits,
			"expires_at": apiKey.ExpiresAt.Format(time.RFC3339),
			"scopes":     apiKey.Scopes,
		})
	}))

	mux.HandleFunc("/auth/revoke", secMiddleware.RequireAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if req.ID == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}

		if err := authMgr.RevokeKey(req.ID); err != nil {
			http.Error(w, "Failed to revoke key", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
	}))

	mux.HandleFunc("/auth/keys", secMiddleware.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		apiKey, ok := middleware.APIKeyFromContext(r.Context())
		if !ok {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		keys, err := authMgr.ListKeys(apiKey.OwnerEmail)
		if err != nil {
			http.Error(w, "Failed to list keys", http.StatusInternalServerError)
			return
		}

		for i := range keys {
			keys[i].Key = "***" + keys[i].Key[len(keys[i].Key)-8:]
		}

		json.NewEncoder(w).Encode(keys)
	}))

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "# HELP openpool_uptime_seconds Time since node started\n")
		fmt.Fprintf(w, "# TYPE openpool_uptime_seconds gauge\n")
		fmt.Fprintf(w, "openpool_uptime_seconds 0\n")
	})

	addr := fmt.Sprintf(":%d", *flagHTTP)

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	tlsEnabled := *flagTLSCert != "" && *flagTLSKey != ""

	if tlsEnabled {
		cfg := &tls.Config{
			MinVersion: tls.VersionTLS12,
			CurvePreferences: []tls.CurveID{
				tls.CurveP521,
				tls.CurveP384,
				tls.CurveP256,
			},
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			},
		}
		server.TLSConfig = cfg
	}

	fmt.Printf("%s HTTP API: http", cyan.Render("🌐"))
	if tlsEnabled {
		fmt.Printf("s")
	}
	fmt.Printf("://localhost:%d/\n", *flagHTTP)

	var srvErr error
	if tlsEnabled {
		srvErr = server.ListenAndServeTLS(*flagTLSCert, *flagTLSKey)
	} else {
		srvErr = server.ListenAndServe()
	}

	if srvErr != nil && srvErr != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", srvErr)
	}
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
