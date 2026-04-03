package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Peer info stored in registry
type Peer struct {
	ID         string    `json:"id"`
	Multiaddr  string    `json:"multiaddr"`
	Country    string    `json:"country,omitempty"`
	CPUCores   int       `json:"cpu_cores"`
	HasGPU     bool      `json:"has_gpu"`
	Price      int       `json:"price"`
	Status     string    `json:"status"`
	Registered time.Time `json:"registered_at"`
	LastSeen   time.Time `json:"last_seen"`
}

// Registry holds all peers
type Registry struct {
	mu    sync.RWMutex
	peers map[string]*Peer
}

var registry = &Registry{
	peers: make(map[string]*Peer),
}

func main() {
	port := flag.Int("port", 8080, "HTTP port")
	flag.Parse()

	mux := http.NewServeMux()
	
	// CORS headers
	withCORS := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			h(w, r)
		}
	}

	// Register a new peer
	mux.HandleFunc("/api/register", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var peer Peer
		if err := json.NewDecoder(r.Body).Decode(&peer); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		registry.mu.Lock()
		peer.Registered = time.Now()
		peer.LastSeen = time.Now()
		peer.Status = "online"
		registry.peers[peer.ID] = &peer
		registry.mu.Unlock()

		log.Printf("Registered peer: %s (%s)", peer.ID, peer.Multiaddr)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	// Unregister a peer
	mux.HandleFunc("/api/unregister", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		registry.mu.Lock()
		if peer, ok := registry.peers[req.ID]; ok {
			peer.Status = "offline"
			log.Printf("Unregistered peer: %s", req.ID)
		}
		registry.mu.Unlock()

		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	// Heartbeat to keep peer alive
	mux.HandleFunc("/api/heartbeat", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		registry.mu.Lock()
		if peer, ok := registry.peers[req.ID]; ok {
			peer.LastSeen = time.Now()
			peer.Status = "online"
		}
		registry.mu.Unlock()

		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	// List all online peers
	mux.HandleFunc("/api/peers", withCORS(func(w http.ResponseWriter, r *http.Request) {
		registry.mu.RLock()
		peers := make([]*Peer, 0)
		for _, p := range registry.peers {
			// Only show peers seen in last 2 minutes
			if time.Since(p.LastSeen) < 2*time.Minute {
				peers = append(peers, p)
			}
		}
		registry.mu.RUnlock()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"peers": peers,
			"count": len(peers),
		})
	}))

	// Health check
	mux.HandleFunc("/api/health", withCORS(func(w http.ResponseWriter, r *http.Request) {
		registry.mu.RLock()
		count := len(registry.peers)
		registry.mu.RUnlock()
		
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"peers":  count,
		})
	}))

	// Clean up stale peers every minute
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		for range ticker.C {
			registry.mu.Lock()
			for id, p := range registry.peers {
				if time.Since(p.LastSeen) > 2*time.Minute {
					p.Status = "offline"
					log.Printf("Peer went offline: %s", id)
				}
			}
			registry.mu.Unlock()
		}
	}()

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("OpenPool Discovery Server starting on %s", addr)
	log.Printf("Endpoints:")
	log.Printf("  POST /api/register   - Register a peer")
	log.Printf("  POST /api/unregister - Unregister a peer")
	log.Printf("  POST /api/heartbeat - Keep peer alive")
	log.Printf("  GET  /api/peers     - List online peers")
	log.Printf("  GET  /api/health    - Health check")

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
