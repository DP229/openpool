package connection

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrConnectionLimit   = errors.New("connection limit reached")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
	ErrConnNotFound      = errors.New("connection not found")
)

type ConnectionState int

const (
	StateConnecting ConnectionState = iota
	StateActive
	StateClosing
	StateClosed
)

type Connection struct {
	ID          string
	PeerID      string
	RemoteAddr  string
	ConnectedAt time.Time
	LastActive  time.Time
	BytesIn     int64
	BytesOut    int64
	State       ConnectionState
	Metadata    map[string]interface{}
}

type Limits struct {
	MaxConnections      int
	MaxConnectionsPerIP int
	MaxStreamsPerConn   int
	MaxBandwidth        int64 // bytes per second
	ConnectionRate      int   // new connections per minute
	StreamRate          int   // new streams per minute
	IdleTimeout         time.Duration
	TotalTimeout        time.Duration
}

type DefaultLimits struct{}

func (d DefaultLimits) Limits() Limits {
	return Limits{
		MaxConnections:      1000,
		MaxConnectionsPerIP: 50,
		MaxStreamsPerConn:   100,
		MaxBandwidth:        50 * 1024 * 1024, // 50 MB/s
		ConnectionRate:      100,
		StreamRate:          1000,
		IdleTimeout:         5 * time.Minute,
		TotalTimeout:        1 * time.Hour,
	}
}

type Manager struct {
	limits Limits

	connections map[string]*Connection
	byIP        map[string]int
	byPeer      map[string][]string

	mu sync.RWMutex

	// Tracking
	connectionHistory []time.Time
	streamHistory     []time.Time

	// Callbacks
	onConnect    func(*Connection)
	onDisconnect func(*Connection)
}

func NewManager(limits Limits) *Manager {
	if limits.MaxConnections <= 0 {
		limits.MaxConnections = 1000
	}
	if limits.MaxConnectionsPerIP <= 0 {
		limits.MaxConnectionsPerIP = 50
	}
	if limits.MaxStreamsPerConn <= 0 {
		limits.MaxStreamsPerConn = 100
	}

	return &Manager{
		limits:      limits,
		connections: make(map[string]*Connection),
		byIP:        make(map[string]int),
		byPeer:      make(map[string][]string),
	}
}

func (m *Manager) CanConnect(remoteAddr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check total connections
	if len(m.connections) >= m.limits.MaxConnections {
		return ErrConnectionLimit
	}

	// Check connections per IP
	if m.byIP[remoteAddr] >= m.limits.MaxConnectionsPerIP {
		return ErrConnectionLimit
	}

	// Check connection rate
	if !m.checkConnectionRate() {
		return ErrRateLimitExceeded
	}

	return nil
}

func (m *Manager) OnConnect(conn *Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conn.ID == "" {
		return errors.New("connection ID required")
	}

	if _, exists := m.connections[conn.ID]; exists {
		return errors.New("connection already exists")
	}

	conn.ConnectedAt = time.Now()
	conn.LastActive = time.Now()
	conn.State = StateActive
	if conn.Metadata == nil {
		conn.Metadata = make(map[string]interface{})
	}

	m.connections[conn.ID] = conn
	m.byIP[conn.RemoteAddr]++

	if conn.PeerID != "" {
		m.byPeer[conn.PeerID] = append(m.byPeer[conn.PeerID], conn.ID)
	}

	m.connectionHistory = append(m.connectionHistory, time.Now())

	if m.onConnect != nil {
		go m.onConnect(conn)
	}

	return nil
}

func (m *Manager) OnDisconnect(connID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, exists := m.connections[connID]
	if !exists {
		return ErrConnNotFound
	}

	conn.State = StateClosed
	delete(m.connections, connID)

	m.byIP[conn.RemoteAddr]--
	if m.byIP[conn.RemoteAddr] <= 0 {
		delete(m.byIP, conn.RemoteAddr)
	}

	if conn.PeerID != "" {
		peerConns := m.byPeer[conn.PeerID]
		for i, id := range peerConns {
			if id == connID {
				m.byPeer[conn.PeerID] = append(peerConns[:i], peerConns[i+1:]...)
				break
			}
		}
		if len(m.byPeer[conn.PeerID]) == 0 {
			delete(m.byPeer, conn.PeerID)
		}
	}

	if m.onDisconnect != nil {
		go m.onDisconnect(conn)
	}

	return nil
}

func (m *Manager) RecordActivity(connID string, bytesIn, bytesOut int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, exists := m.connections[connID]
	if !exists {
		return ErrConnNotFound
	}

	conn.LastActive = time.Now()
	conn.BytesIn += bytesIn
	conn.BytesOut += bytesOut

	return nil
}

func (m *Manager) CanOpenStream(connID string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.checkStreamRate() {
		return ErrRateLimitExceeded
	}

	return nil
}

func (m *Manager) checkConnectionRate() bool {
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	valid := make([]time.Time, 0, len(m.connectionHistory))
	for _, t := range m.connectionHistory {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	m.connectionHistory = valid

	if len(valid) >= m.limits.ConnectionRate {
		return false
	}

	return true
}

func (m *Manager) checkStreamRate() bool {
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	valid := make([]time.Time, 0, len(m.streamHistory))
	for _, t := range m.streamHistory {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	m.streamHistory = valid

	if len(valid) >= m.limits.StreamRate {
		return false
	}

	m.streamHistory = append(m.streamHistory, now)
	return true
}

func (m *Manager) GetConnection(connID string) (*Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.connections[connID]
	if !exists {
		return nil, ErrConnNotFound
	}

	return conn, nil
}

func (m *Manager) GetActiveConnections() []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	connections := make([]*Connection, 0, len(m.connections))
	for _, conn := range m.connections {
		connections = append(connections, conn)
	}

	return connections
}

func (m *Manager) GetConnectionsByPeer(peerID string) []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	connIDs, exists := m.byPeer[peerID]
	if !exists {
		return nil
	}

	connections := make([]*Connection, 0, len(connIDs))
	for _, id := range connIDs {
		if conn, ok := m.connections[id]; ok {
			connections = append(connections, conn)
		}
	}

	return connections
}

func (m *Manager) GetConnectionsByIP(ip string) []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	connections := make([]*Connection, 0)
	for _, conn := range m.connections {
		if conn.RemoteAddr == ip {
			connections = append(connections, conn)
		}
	}

	return connections
}

func (m *Manager) Stats() *ConnectionStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	active := 0
	totalBytesIn := int64(0)
	totalBytesOut := int64(0)

	for _, conn := range m.connections {
		if conn.State == StateActive {
			active++
		}
		totalBytesIn += conn.BytesIn
		totalBytesOut += conn.BytesOut
	}

	return &ConnectionStats{
		TotalConnections:  len(m.connections),
		ActiveConnections: active,
		ConnectionsByIP:   len(m.byIP),
		ConnectionsByPeer: len(m.byPeer),
		TotalBytesIn:      totalBytesIn,
		TotalBytesOut:     totalBytesOut,
	}
}

type ConnectionStats struct {
	TotalConnections  int
	ActiveConnections int
	ConnectionsByIP   int
	ConnectionsByPeer int
	TotalBytesIn      int64
	TotalBytesOut     int64
}

func (m *Manager) Cleanup() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-m.limits.IdleTimeout)
	closed := 0

	for connID, conn := range m.connections {
		if conn.LastActive.Before(cutoff) || conn.State == StateClosing {
			delete(m.connections, connID)
			m.byIP[conn.RemoteAddr]--
			if m.byIP[conn.RemoteAddr] <= 0 {
				delete(m.byIP, conn.RemoteAddr)
			}
			closed++
		}
	}

	return closed
}

func (m *Manager) OnConnectCallback(fn func(*Connection)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onConnect = fn
}

func (m *Manager) OnDisconnectCallback(fn func(*Connection)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onDisconnect = fn
}
