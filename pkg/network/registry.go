package network

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type Peer struct {
	ID          string    `json:"id"`
	Multiaddr   string    `json:"multiaddr"`
	Country     string    `json:"country,omitempty"`
	CPUCores    int       `json:"cpu_cores"`
	HasGPU      bool      `json:"has_gpu"`
	Price       int       `json:"price"`
	Status      string    `json:"status"` // online, offline
	LastSeen    time.Time `json:"last_seen"`
	Registered  time.Time `json:"registered_at"`
}

type Registry struct {
	mu      sync.RWMutex
	peers   map[string]*Peer
	server  string // Registry server URL
}

var globalRegistry *Registry

func InitRegistry(server string) *Registry {
	if globalRegistry == nil {
		globalRegistry = &Registry{
			peers:  make(map[string]*Peer),
			server: server,
		}
	}
	return globalRegistry
}

func GetRegistry() *Registry {
	return globalRegistry
}

func (r *Registry) Register(peer *Peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	peer.Registered = time.Now()
	peer.LastSeen = time.Now()
	peer.Status = "online"
	r.peers[peer.ID] = peer
}

func (r *Registry) Unregister(peerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if peer, ok := r.peers[peerID]; ok {
		peer.Status = "offline"
		peer.LastSeen = time.Now()
	}
}

func (r *Registry) Heartbeat(peerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if peer, ok := r.peers[peerID]; ok {
		peer.LastSeen = time.Now()
		peer.Status = "online"
	}
}

func (r *Registry) ListPeers() []*Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	peers := make([]*Peer, 0, len(r.peers))
	for _, p := range r.peers {
		// Only show peers seen in last 5 minutes
		if time.Since(p.LastSeen) < 5*time.Minute {
			peers = append(peers, p)
		}
	}
	return peers
}

func (r *Registry) GetPeer(peerID string) *Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.peers[peerID]
}

// HTTP Handlers for Registry Server

type RegistryServer struct {
	registry *Registry
	mu      sync.RWMutex
	peers   map[string]*Peer
}

func NewRegistryServer() *RegistryServer {
	return &RegistryServer{
		peers: make(map[string]*Peer),
	}
}

func (s *RegistryServer) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var peer Peer
	if err := json.NewDecoder(r.Body).Decode(&peer); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	peer.LastSeen = time.Now()
	peer.Status = "online"

	s.mu.Lock()
	s.peers[peer.ID] = &peer
	s.mu.Unlock()

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

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

	s.mu.Lock()
	if peer, ok := s.peers[req.ID]; ok {
		peer.Status = "offline"
	}
	s.mu.Unlock()

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

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

	s.mu.Lock()
	if peer, ok := s.peers[req.ID]; ok {
		peer.LastSeen = time.Now()
		peer.Status = "online"
	}
	s.mu.Unlock()

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *RegistryServer) HandleListPeers(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	peers := make([]*Peer, 0, len(s.peers))
	for _, p := range s.peers {
		// Only show online peers (seen in last 2 minutes)
		if time.Since(p.LastSeen) < 2*time.Minute {
			peers = append(peers, p)
		}
	}
	s.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"peers": peers,
		"count": len(peers),
	})
}

// Client-side registry registration

type RegistryClient struct {
	serverURL string
	peerID   string
	stopCh   chan struct{}
}

func NewRegistryClient(serverURL, peerID, multiaddr string, cpuCores int, hasGPU bool, price int) *RegistryClient {
	return &RegistryClient{
		serverURL: serverURL,
		peerID:   peerID,
		stopCh:   make(chan struct{}),
	}
}

func (c *RegistryClient) Register(multiaddr string, country string, cpuCores int, hasGPU bool, price int) error {
	peer := &Peer{
		ID:        c.peerID,
		Multiaddr: multiaddr,
		CPUCores: cpuCores,
		HasGPU:    hasGPU,
		Price:     price,
		Status:    "online",
		Country:   country,
	}

	resp, err := http.Post(c.serverURL+"/register", "application/json", toJSON(peer))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	go c.heartbeatLoop()
	return nil
}

func (c *RegistryClient) heartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.heartbeat()
		case <-c.stopCh:
			c.unregister()
			return
		}
	}
}

func (c *RegistryClient) heartbeat() {
	req, _ := http.NewRequest(http.MethodPost, c.serverURL+"/heartbeat", toJSON(struct {
		ID string `json:"id"`
	}{ID: c.peerID}))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	client.Do(req)
}

func (c *RegistryClient) unregister() {
	req, _ := http.NewRequest(http.MethodPost, c.serverURL+"/unregister", toJSON(struct {
		ID string `json:"id"`
	}{ID: c.peerID}))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	client.Do(req)
}

func (c *RegistryClient) Stop() {
	close(c.stopCh)
}

func toJSON(v interface{}) *strings.Reader {
	data, _ := json.Marshal(v)
	return strings.NewReader(string(data))
}

type strings struct{}
func (strings) NewReader(s string) *strings.Reader {
	return &strings.Reader{s: s}
}

type Reader struct {
	s   string
	off int
}

func toJSON2(v interface{}) (*Reader, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &Reader{s: string(data)}, nil
}

func (r *Reader) Read(p []byte) (n int, err error) {
	if r.off >= len(r.s) {
		return 0, io.EOF
	}
	n = copy(p, r.s[r.off:])
	r.off += n
	return n, nil
}
