package network

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// PeerInfo represents enhanced peer information
type PeerInfo struct {
	ID           string           `json:"id"`
	Multiaddr    string          `json:"multiaddr"`
	Country     string          `json:"country,omitempty"`
	CPUCores    int             `json:"cpu_cores"`
	HasGPU      bool            `json:"has_gpu"`
	GPUModel    string          `json:"gpu_model,omitempty"`
	Price       int             `json:"price"`
	TaskHistory []TaskMetrics   `json:"task_history,omitempty"`
	Status      string          `json:"status"`
	Score       float64         `json:"score"`
	Reliability float64         `json:"reliability"`
	AvgLatency  int            `json:"avg_latency_ms"`
	LastSeen    time.Time       `json:"last_seen"`
	Registered  time.Time       `json:"registered_at"`
}

// TaskMetrics represents task execution metrics for a peer
type TaskMetrics struct {
	Type        string  `json:"type"`
	Count       int     `json:"count"`
	AvgLatency  int     `json:"avg_latency_ms"`
	AvgScore    float64 `json:"avg_score"`
	SuccessRate float64 `json:"success_rate"`
}

// RegistryServer is an enhanced discovery server with scoring
type RegistryServer struct {
	peers map[string]*PeerInfo
	mu    sync.RWMutex
	stats RegistryStats
}

// RegistryStats tracks registry statistics
type RegistryStats struct {
	TotalPeers    int       `json:"total_peers"`
	TotalTasks    int       `json:"total_tasks"`
	GPUNodes      int       `json:"gpu_nodes"`
	LastUpdated   time.Time `json:"last_updated"`
}

// NewEnhancedRegistry creates a new enhanced registry server
func NewEnhancedRegistry() *RegistryServer {
	return &RegistryServer{
		peers: make(map[string]*PeerInfo),
		stats: RegistryStats{LastUpdated: time.Now()},
	}
}

// Register registers a peer with the registry
func (s *RegistryServer) Register(peer *PeerInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	peer.Registered = time.Now()
	peer.LastSeen = time.Now()
	peer.Status = "online"

	// Calculate initial score based on capabilities
	peer.Score = s.calculateScore(peer)
	peer.Reliability = 1.0 // Start at full reliability

	s.peers[peer.ID] = peer
	s.stats.TotalPeers = len(s.peers)
	s.stats.LastUpdated = time.Now()

	if peer.HasGPU {
		s.stats.GPUNodes++
	}

	log.Printf("📝 Registered peer: %s (score: %.2f, GPU: %v)", peer.ID[:8], peer.Score, peer.HasGPU)
	return nil
}

// Heartbeat updates peer last-seen and recalculates score
func (s *RegistryServer) Heartbeat(peerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	peer, ok := s.peers[peerID]
	if !ok {
		return fmt.Errorf("peer not found")
	}

	peer.LastSeen = time.Now()
	peer.Status = "online"
	peer.Reliability = min(1.0, peer.Reliability+0.01) // Gradually increase reliability

	s.stats.LastUpdated = time.Now()
	return nil
}

// Unregister removes a peer from the registry
func (s *RegistryServer) Unregister(peerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if peer, ok := s.peers[peerID]; ok {
		if peer.HasGPU {
			s.stats.GPUNodes--
		}
		delete(s.peers, peerID)
		s.stats.TotalPeers = len(s.peers)
		s.stats.LastUpdated = time.Now()
	}

	return nil
}

// UpdateTaskMetrics updates peer task history
func (s *RegistryServer) UpdateTaskMetrics(peerID string, metrics TaskMetrics) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	peer, ok := s.peers[peerID]
	if !ok {
		return fmt.Errorf("peer not found")
	}

	// Add to history
	peer.TaskHistory = append(peer.TaskHistory, metrics)
	
	// Keep only last 10 entries
	if len(peer.TaskHistory) > 10 {
		peer.TaskHistory = peer.TaskHistory[len(peer.TaskHistory)-10:]
	}

	// Recalculate score
	peer.Score = s.calculateScore(peer)
	s.stats.TotalTasks++

	return nil
}

// ListPeers returns all online peers sorted by score
func (s *RegistryServer) ListPeers() []*PeerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	peers := make([]*PeerInfo, 0)
	cutoff := time.Now().Add(-2 * time.Minute)

	for _, p := range s.peers {
		// Only show peers seen recently
		if p.LastSeen.After(cutoff) {
			peers = append(peers, p)
		}
	}

	// Sort by score (descending)
	for i := 0; i < len(peers)-1; i++ {
		for j := i + 1; j < len(peers); j++ {
			if peers[j].Score > peers[i].Score {
				peers[i], peers[j] = peers[j], peers[i]
			}
		}
	}

	return peers
}

// FindBestPeers finds the best peers for a task
func (s *RegistryServer) FindBestPeers(requireGPU bool, minCount int) []*PeerInfo {
	peers := s.ListPeers()

	if minCount <= 0 {
		minCount = 1
	}

	// Filter by GPU requirement
	if requireGPU {
		gpuPeers := make([]*PeerInfo, 0)
		for _, p := range peers {
			if p.HasGPU {
				gpuPeers = append(gpuPeers, p)
			}
		}
		if len(gpuPeers) >= minCount {
			return gpuPeers[:minCount]
		}
		return gpuPeers
	}

	if len(peers) < minCount {
		return peers
	}
	return peers[:minCount]
}

// GetPeer returns a specific peer
func (s *RegistryServer) GetPeer(peerID string) *PeerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.peers[peerID]
}

// GetStats returns registry statistics
func (s *RegistryServer) GetStats() RegistryStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

func (s *RegistryServer) calculateScore(peer *PeerInfo) float64 {
	// Weights
	const (
		ReliabilityWeight = 0.30
		SpeedWeight     = 0.25
		GPUWeight      = 0.25
		PriceWeight    = 0.20
	)

	// Reliability score (0.0 - 1.0)
	relScore := peer.Reliability
	if relScore < 0 {
		relScore = 0
	}

	// Speed score (inverse of latency)
	// Lower latency = higher score
	// 100ms = 0.9, 5000ms = 0.1
	latScore := 1.0 - float64(peer.AvgLatency)/5000.0
	if latScore < 0.1 {
		latScore = 0.1
	}
	if latScore > 1.0 {
		latScore = 1.0
	}

	// GPU score
	gpuScore := 0.0
	if peer.HasGPU {
		gpuScore = 1.0
	}

	// Price score (inverse)
	// Lower price = higher score
	priceScore := 1.0 - float64(peer.Price)/100.0
	if priceScore < 0 {
		priceScore = 0
	}

	return ReliabilityWeight*relScore + SpeedWeight*latScore + GPUWeight*gpuScore + PriceWeight*priceScore
}

// HTTP Handlers for the enhanced registry

// HandleRegister handles peer registration
func (s *RegistryServer) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var peer PeerInfo
	if err := json.NewDecoder(r.Body).Decode(&peer); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.Register(&peer); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleUnregister handles peer unregistration
func (s *RegistryServer) HandleUnregister(w http.ResponseWriter, r *http.Request) {
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

	if err := s.Unregister(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleHeartbeat handles peer heartbeat
func (s *RegistryServer) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
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

	if err := s.Heartbeat(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleListPeers returns all online peers
func (s *RegistryServer) HandleListPeers(w http.ResponseWriter, r *http.Request) {
	peers := s.ListPeers()

	// Remove sensitive data for public API
	publicPeers := make([]map[string]interface{}, len(peers))
	for i, p := range peers {
		publicPeers[i] = map[string]interface{}{
			"id":          p.ID,
			"country":     p.Country,
			"cpu_cores":   p.CPUCores,
			"has_gpu":     p.HasGPU,
			"gpu_model":    p.GPUModel,
			"price":       p.Price,
			"status":      p.Status,
			"score":       p.Score,
			"avg_latency": p.AvgLatency,
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"peers": publicPeers,
		"count": len(peers),
		"stats": s.GetStats(),
	})
}

// HandleFindBest finds the best peers for a task
func (s *RegistryServer) HandleFindBest(w http.ResponseWriter, r *http.Request) {
	gpu := r.URL.Query().Get("gpu") == "true"
	count := 3 // default

	fmt.Sscanf(r.URL.Query().Get("count"), "%d", &count)

	peers := s.FindBestPeers(gpu, count)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"peers": peers,
		"count": len(peers),
	})
}

// HandleUpdateMetrics updates peer task metrics
func (s *RegistryServer) HandleUpdateMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID      string       `json:"id"`
		Metrics TaskMetrics `json:"metrics"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.UpdateTaskMetrics(req.ID, req.Metrics); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleStats returns registry statistics
func (s *RegistryServer) HandleStats(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(s.GetStats())
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

var _ = flag.String("", "", "") // suppress unused warning
