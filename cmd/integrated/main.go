package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
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

	"github.com/dp229/openpool/pkg/auth"
	"github.com/dp229/openpool/pkg/executor"
	"github.com/dp229/openpool/pkg/ledger"
	"github.com/dp229/openpool/pkg/middleware"
	"github.com/dp229/openpool/pkg/net"
	"github.com/dp229/openpool/pkg/resilience"
	"github.com/dp229/openpool/pkg/security"
	"github.com/dp229/openpool/pkg/shutdown"
	"github.com/dp229/openpool/pkg/worker"
)

var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5cb85c"))
	blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("#5bc0de"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#f0ad4e"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("#d9534f"))
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("#337ab7"))
)

var (
	flagPort        = flag.Int("port", 9000, "TCP port to listen on")
	flagHTTP        = flag.Int("http", 8080, "HTTP API port (0 to disable)")
	flagDB          = flag.String("db", "openpool.db", "SQLite ledger path")
	flagCredits     = flag.Int("credits", 100, "Starting credits")
	flagAuthDB      = flag.String("auth-db", "openpool_auth.db", "Auth database path")
	flagAdminSecret = flag.String("admin-secret", "", "Admin secret for API key management")
	flagRequireAuth = flag.Bool("require-auth", false, "Require authentication for API access")
	flagRateLimit   = flag.Int("rate-limit", 100, "Rate limit requests per minute")
	flagTLSCert     = flag.String("tls-cert", "", "TLS certificate file")
	flagTLSKey      = flag.String("tls-key", "", "TLS private key file")
	flagWorkers     = flag.Int("workers", 4, "Number of worker pool workers")
	flagQueueSize   = flag.Int("queue", 100, "Task queue size")
	flagTest        = flag.Bool("test", false, "Run built-in test task")
	flagSend        = flag.String("send", "", "Send task to peer (peer_ip:port)")
	flagTaskFile    = flag.String("task", "", "Task JSON file")
	flagShutTimeout = flag.Int("shutdown-timeout", 30, "Shutdown timeout in seconds")
	flagMaxFailures = flag.Int("max-failures", 5, "Circuit breaker max failures")
)

type IntegratedNode struct {
	Node           *net.Node
	DB             *ledger.Ledger
	AuthMgr        *auth.Manager
	Executor       *executor.IntegratedExecutor
	Sanitizer      *security.Sanitizer
	SecMiddleware  *middleware.SecurityMiddleware
	CircuitBreaker *resilience.CircuitBreaker
	ShutdownMgr    *shutdown.GracefulShutdown
}

func main() {
	flag.Parse()

	idBytes := make([]byte, 8)
	rand.Read(idBytes)
	nodeID := hex.EncodeToString(idBytes)
	log.SetPrefix(fmt.Sprintf("[%s] ", nodeID[:6]))

	db, err := ledger.New(*flagDB)
	if err != nil {
		log.Fatal("Ledger error:", err)
	}
	db.AddCredits(nodeID, *flagCredits)
	fmt.Printf("%s Ledger: %s | %d credits\n", green.Render("✓"), nodeID[:6], *flagCredits)

	authMgr, err := auth.NewManager(*flagAuthDB)
	if err != nil {
		log.Fatal("Auth manager error:", err)
	}
	defer authMgr.Close()

	_executorConfig := executor.IntegratedConfig{
		Workers:        *flagWorkers,
		QueueSize:      *flagQueueSize,
		TaskTimeout:    30 * time.Second,
		MaxFailures:    *flagMaxFailures,
		CircuitTimeout: time.Duration(*flagShutTimeout) * time.Second,
	}
	integExecutor := executor.NewIntegratedExecutor(_executorConfig)

	circuitBreaker := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:          "node-tasks",
		MaxFailures:   *flagMaxFailures,
		Timeout:       60 * time.Second,
		HalfOpenLimit: 3,
	})

	sanitizer := security.NewSanitizer()
	secMiddleware := middleware.NewSecurityMiddleware(authMgr, *flagRateLimit, *flagAdminSecret, *flagRequireAuth)

	node := net.New(*flagPort)
	node.ID = nodeID
	node.DB = db

	shutdownMgr := shutdown.New(
		shutdown.WithTimeout(time.Duration(*flagShutTimeout)*time.Second),
		shutdown.WithOnShuttingDown(func() {
			fmt.Printf("%s Initiating graceful shutdown...\n", yellow.Render("■"))
		}),
	)

	integNode := &IntegratedNode{
		Node:           node,
		DB:             db,
		AuthMgr:        authMgr,
		Executor:       integExecutor,
		Sanitizer:      sanitizer,
		SecMiddleware:  secMiddleware,
		CircuitBreaker: circuitBreaker,
		ShutdownMgr:    shutdownMgr,
	}

	shutdownMgr.Register("http-server", func(ctx context.Context) error {
		fmt.Printf("%s Shutting down HTTP server...\n", blue.Render("→"))
		return nil
	})

	shutdownMgr.Register("p2p-node", func(ctx context.Context) error {
		fmt.Printf("%s Shutting down P2P node...\n", blue.Render("→"))
		return node.Close()
	})

	integExecutor.RegisterShutdownHandler(shutdownMgr)

	shutdownMgr.Register("ledger", func(ctx context.Context) error {
		fmt.Printf("%s Closing ledger...\n", blue.Render("→"))
		return nil
	})

	taskHandler := func(ctx context.Context, task *worker.Task) ([]byte, error) {
		result, err := node.RunTask(ctx, &net.Task{
			ID:         task.ID,
			Input:      task.Data,
			TimeoutSec: int(task.Timeout.Seconds()),
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	if err := integExecutor.Start(taskHandler); err != nil {
		log.Fatal("Executor error:", err)
	}
	fmt.Printf("%s Worker pool started: %d workers, queue size %d\n", green.Render("✓"), *flagWorkers, *flagQueueSize)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	if err := node.Listen(ctx); err != nil {
		log.Fatal("Listen error:", err)
	}

	if !(*flagHTTP == 0) {
		go serveHTTP(integNode)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	cancel()

	shutdownMgr.Wait()
}

func serveHTTP(n *IntegratedNode) {
	mux := http.NewServeMux()

	mux.HandleFunc("/status", n.SecMiddleware.RateLimit(n.SecMiddleware.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		health := n.Executor.HealthCheck()
		health["node_id"] = n.Node.ID
		health["port"] = n.Node.Port
		health["credits"] = n.DB.GetCredits(n.Node.ID)
		health["cpu_cores"] = runtime.NumCPU()
		health["ram_free_mb"] = getFreeRAM()
		health["peers"] = n.Node.PeerCount()
		json.NewEncoder(w).Encode(health)
	})))

	mux.HandleFunc("/ledger", n.SecMiddleware.RateLimit(n.SecMiddleware.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		apiKey, ok := middleware.APIKeyFromContext(r.Context())
		if *flagRequireAuth && !ok {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		if *flagRequireAuth && apiKey.Credits < 1 {
			http.Error(w, "Insufficient credits", http.StatusForbidden)
			return
		}

		json.NewEncoder(w).Encode(n.DB.GetAll())

		if *flagRequireAuth && ok {
			n.AuthMgr.UseCredits(apiKey.Key, 1)
		}
	})))

	mux.HandleFunc("/submit", n.SecMiddleware.RateLimit(n.SecMiddleware.Authenticate(n.SecMiddleware.ValidateTaskInput(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		apiKey, ok := middleware.APIKeyFromContext(r.Context())
		if *flagRequireAuth && !ok {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		var task net.Task
		json.NewDecoder(r.Body).Decode(&task)
		if task.ID == "" {
			idB := make([]byte, 8)
			rand.Read(idB)
			task.ID = hex.EncodeToString(idB)
		}

		if *flagRequireAuth {
			if err := security.ValidateCredits(task.Credits); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if ok && apiKey.Credits < task.Credits {
				http.Error(w, "Insufficient credits", http.StatusForbidden)
				return
			}
		}

		task.Credits = 10
		task.State = "pending"
		task.CreatedAt = time.Now()

		peerAddr := r.URL.Query().Get("peer")
		ctx := context.Background()
		if peerAddr != "" {
			if err := n.Node.SubmitTask(ctx, peerAddr, &task); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fmt.Printf("%s HTTP: task %s → %s\n", blue.Render("→"), task.ID[:8], peerAddr)
		} else {
			result, err := n.Executor.ExecuteWithProtection(ctx, &executor.Task{
				ID:         task.ID,
				TimeoutSec: task.TimeoutSec,
				Credits:    task.Credits,
			})
			task.State = "done"
			if err != nil {
				task.State = "failed"
				task.Error = err.Error()
			}
			if result != nil {
				task.Result = result.Result
			}
			n.DB.AddCredits(n.Node.ID, task.Credits)
			fmt.Printf("%s HTTP: task %s local +%d credits\n", green.Render("✓"), task.ID[:8], task.Credits)
		}

		if *flagRequireAuth && ok {
			n.AuthMgr.UseCredits(apiKey.Key, task.Credits)
		}

		json.NewEncoder(w).Encode(task)
	}))))

	mux.HandleFunc("/connect", n.SecMiddleware.RateLimit(n.SecMiddleware.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Address string `json:"address"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		ctx := context.Background()
		if err := n.Node.Connect(ctx, req.Address); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "connected", "address": req.Address})
	})))

	mux.HandleFunc("/auth/apikey", n.SecMiddleware.RequireAdmin(func(w http.ResponseWriter, r *http.Request) {
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

		apiKey, err := n.AuthMgr.GenerateAPIKey(req.OwnerName, req.OwnerEmail, req.Credits, req.Scopes, 365*24*time.Hour)
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

	mux.HandleFunc("/auth/revoke", n.SecMiddleware.RequireAdmin(func(w http.ResponseWriter, r *http.Request) {
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

		if err := n.AuthMgr.RevokeKey(req.ID); err != nil {
			http.Error(w, "Failed to revoke key", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
	}))

	mux.HandleFunc("/auth/keys", n.SecMiddleware.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		apiKey, ok := middleware.APIKeyFromContext(r.Context())
		if !ok {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		keys, err := n.AuthMgr.ListKeys(apiKey.OwnerEmail)
		if err != nil {
			http.Error(w, "Failed to list keys", http.StatusInternalServerError)
			return
		}

		for i := range keys {
			keys[i].Key = "***" + keys[i].Key[len(keys[i].Key)-8:]
		}

		json.NewEncoder(w).Encode(keys)
	}))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		health := n.Executor.HealthCheck()
		health["circuit_breaker_state"] = n.CircuitBreaker.State().String()
		health["shutdown_in_progress"] = n.Executor.IsShuttingDown()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(health)
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
	fmt.Printf("   curl http")
	if tlsEnabled {
		fmt.Printf("s")
	}
	fmt.Printf("://localhost:%d/status\n", *flagHTTP)
	fmt.Printf("   curl http")
	if tlsEnabled {
		fmt.Printf("s")
	}
	fmt.Printf("://localhost:%d/health\n", *flagHTTP)
	fmt.Printf("   curl -X POST http")
	if tlsEnabled {
		fmt.Printf("s")
	}
	fmt.Printf("://localhost:%d/submit -d '{\"wasm_path\":\"\"}'\n", *flagHTTP)

	var err error
	if tlsEnabled {
		err = server.ListenAndServeTLS(*flagTLSCert, *flagTLSKey)
	} else {
		err = server.ListenAndServe()
	}

	if err != nil && err != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", err)
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
