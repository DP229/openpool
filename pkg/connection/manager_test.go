package connection

import (
	"sync"
	"testing"
	"time"
)

func TestManager_CanConnect(t *testing.T) {
	limits := Limits{
		MaxConnections:      10,
		MaxConnectionsPerIP: 2,
		ConnectionRate:      100,
	}

	mgr := NewManager(limits)

	for i := 0; i < 2; i++ {
		err := mgr.CanConnect("192.168.1.1")
		if err != nil {
			t.Errorf("Expected connection %d to be allowed, got: %v", i, err)
		}

		conn := &Connection{
			ID:         string(rune('A' + i)),
			RemoteAddr: "192.168.1.1",
		}
		mgr.OnConnect(conn)
	}

	err := mgr.CanConnect("192.168.1.1")
	if err == nil {
		t.Error("Expected connection limit to be reached")
	}

	err = mgr.CanConnect("192.168.1.2")
	if err != nil {
		t.Errorf("Expected new IP to be allowed, got: %v", err)
	}
}

func TestManager_OnConnect(t *testing.T) {
	limits := Limits{
		MaxConnections: 10,
	}

	mgr := NewManager(limits)

	conn := &Connection{
		ID:         "test-1",
		PeerID:     "peer-1",
		RemoteAddr: "192.168.1.1",
	}

	err := mgr.OnConnect(conn)
	if err != nil {
		t.Fatalf("Expected connection to be added, got: %v", err)
	}

	if len(mgr.connections) != 1 {
		t.Errorf("Expected 1 connection, got %d", len(mgr.connections))
	}

	if mgr.byIP["192.168.1.1"] != 1 {
		t.Errorf("Expected 1 connection per IP, got %d", mgr.byIP["192.168.1.1"])
	}

	if len(mgr.byPeer["peer-1"]) != 1 {
		t.Errorf("Expected 1 connection per peer, got %d", len(mgr.byPeer["peer-1"]))
	}
}

func TestManager_OnConnect_Duplicate(t *testing.T) {
	mgr := NewManager(Limits{MaxConnections: 10})

	conn := &Connection{ID: "test-1", RemoteAddr: "192.168.1.1"}
	mgr.OnConnect(conn)

	err := mgr.OnConnect(conn)
	if err == nil {
		t.Error("Expected error for duplicate connection")
	}
}

func TestManager_OnDisconnect(t *testing.T) {
	mgr := NewManager(Limits{MaxConnections: 10})

	conn := &Connection{
		ID:         "test-1",
		PeerID:     "peer-1",
		RemoteAddr: "192.168.1.1",
	}
	mgr.OnConnect(conn)

	err := mgr.OnDisconnect("test-1")
	if err != nil {
		t.Fatalf("Expected disconnect to succeed, got: %v", err)
	}

	if len(mgr.connections) != 0 {
		t.Errorf("Expected 0 connections, got %d", len(mgr.connections))
	}

	if mgr.byIP["192.168.1.1"] != 0 {
		t.Errorf("Expected 0 connections per IP, got %d", mgr.byIP["192.168.1.1"])
	}

	if len(mgr.byPeer["peer-1"]) != 0 {
		t.Errorf("Expected 0 connections per peer, got %d", len(mgr.byPeer["peer-1"]))
	}
}

func TestManager_RecordActivity(t *testing.T) {
	mgr := NewManager(Limits{MaxConnections: 10})

	conn := &Connection{ID: "test-1", RemoteAddr: "192.168.1.1"}
	mgr.OnConnect(conn)

	err := mgr.RecordActivity("test-1", 1024, 2048)
	if err != nil {
		t.Fatalf("Expected activity to be recorded, got: %v", err)
	}

	c, _ := mgr.GetConnection("test-1")
	if c.BytesIn != 1024 {
		t.Errorf("Expected 1024 bytes in, got %d", c.BytesIn)
	}
	if c.BytesOut != 2048 {
		t.Errorf("Expected 2048 bytes out, got %d", c.BytesOut)
	}
}

func TestManager_ConnectionRate(t *testing.T) {
	limits := Limits{
		MaxConnections:      100,
		ConnectionRate:      2,
		MaxConnectionsPerIP: 100,
	}

	mgr := NewManager(limits)

	for i := 0; i < 2; i++ {
		err := mgr.CanConnect("192.168.1.1")
		if err != nil {
			t.Errorf("Expected connection %d to be allowed, got: %v", i, err)
		}
		conn := &Connection{ID: string(rune('A' + i)), RemoteAddr: "192.168.1.1"}
		mgr.OnConnect(conn)
	}

	err := mgr.CanConnect("192.168.1.1")
	if err == nil {
		t.Error("Expected connection rate limit to be exceeded")
	}

	mgr.Cleanup()
}

func TestManager_MaxConnections(t *testing.T) {
	limits := Limits{
		MaxConnections:      5,
		MaxConnectionsPerIP: 10,
		ConnectionRate:      100,
	}

	mgr := NewManager(limits)

	for i := 0; i < 5; i++ {
		err := mgr.CanConnect("192.168.1.1")
		if err != nil {
			t.Errorf("Expected connection %d to be allowed, got: %v", i, err)
		}
		conn := &Connection{
			ID:         string(rune('A' + i)),
			RemoteAddr: "192.168.1." + string(rune('0'+i)),
		}
		mgr.OnConnect(conn)
	}

	err := mgr.CanConnect("192.168.1.6")
	if err == nil {
		t.Error("Expected max connections limit to be reached")
	}
}

func TestManager_GetConnections(t *testing.T) {
	mgr := NewManager(Limits{MaxConnections: 10})

	mgr.OnConnect(&Connection{ID: "conn-1", PeerID: "peer-1", RemoteAddr: "192.168.1.1"})
	mgr.OnConnect(&Connection{ID: "conn-2", PeerID: "peer-1", RemoteAddr: "192.168.1.1"})
	mgr.OnConnect(&Connection{ID: "conn-3", PeerID: "peer-2", RemoteAddr: "192.168.1.2"})

	all := mgr.GetActiveConnections()
	if len(all) != 3 {
		t.Errorf("Expected 3 connections, got %d", len(all))
	}

	peer1 := mgr.GetConnectionsByPeer("peer-1")
	if len(peer1) != 2 {
		t.Errorf("Expected 2 connections for peer-1, got %d", len(peer1))
	}

	peer2 := mgr.GetConnectionsByPeer("peer-2")
	if len(peer2) != 1 {
		t.Errorf("Expected 1 connection for peer-2, got %d", len(peer2))
	}

	peer3 := mgr.GetConnectionsByPeer("peer-3")
	if len(peer3) != 0 {
		t.Errorf("Expected 0 connections for peer-3, got %d", len(peer3))
	}
}

func TestManager_Stats(t *testing.T) {
	mgr := NewManager(Limits{MaxConnections: 10})

	mgr.OnConnect(&Connection{ID: "conn-1", PeerID: "peer-1", RemoteAddr: "192.168.1.1"})
	mgr.OnConnect(&Connection{ID: "conn-2", PeerID: "peer-2", RemoteAddr: "192.168.1.2"})
	mgr.RecordActivity("conn-1", 1024, 2048)
	mgr.RecordActivity("conn-2", 512, 1024)

	stats := mgr.Stats()

	if stats.TotalConnections != 2 {
		t.Errorf("Expected 2 total connections, got %d", stats.TotalConnections)
	}

	if stats.ActiveConnections != 2 {
		t.Errorf("Expected 2 active connections, got %d", stats.ActiveConnections)
	}

	if stats.TotalBytesIn != 1536 {
		t.Errorf("Expected 1536 bytes in, got %d", stats.TotalBytesIn)
	}

	if stats.TotalBytesOut != 3072 {
		t.Errorf("Expected 3072 bytes out, got %d", stats.TotalBytesOut)
	}
}

func TestManager_Cleanup(t *testing.T) {
	limits := Limits{
		MaxConnections: 10,
		IdleTimeout:    100 * time.Millisecond,
	}

	mgr := NewManager(limits)

	conn1 := &Connection{
		ID:         "conn-1",
		RemoteAddr: "192.168.1.1",
	}
	mgr.OnConnect(conn1)

	conn2 := &Connection{
		ID:         "conn-2",
		RemoteAddr: "192.168.1.2",
	}
	mgr.OnConnect(conn2)

	time.Sleep(150 * time.Millisecond)

	closed := mgr.Cleanup()

	if closed != 2 {
		t.Errorf("Expected 2 connections closed, got %d", closed)
	}

	if len(mgr.connections) != 0 {
		t.Errorf("Expected 0 connections after cleanup, got %d", len(mgr.connections))
	}
}

func TestManager_Callbacks(t *testing.T) {
	mgr := NewManager(Limits{MaxConnections: 10})

	connectCalled := false
	disconnectCalled := false

	mgr.OnConnectCallback(func(c *Connection) {
		connectCalled = true
		if c.ID != "test-1" {
			t.Errorf("Expected connection ID test-1, got %s", c.ID)
		}
	})

	mgr.OnDisconnectCallback(func(c *Connection) {
		disconnectCalled = true
	})

	conn := &Connection{ID: "test-1", RemoteAddr: "192.168.1.1"}
	mgr.OnConnect(conn)

	time.Sleep(10 * time.Millisecond)

	if !connectCalled {
		t.Error("Expected onConnect callback to be called")
	}

	mgr.OnDisconnect("test-1")

	time.Sleep(10 * time.Millisecond)

	if !disconnectCalled {
		t.Error("Expected onDisconnect callback to be called")
	}
}

func TestDefaultLimits(t *testing.T) {
	defaults := DefaultLimits{}
	limits := defaults.Limits()

	if limits.MaxConnections != 1000 {
		t.Errorf("Expected default MaxConnections to be 1000, got %d", limits.MaxConnections)
	}

	if limits.MaxConnectionsPerIP != 50 {
		t.Errorf("Expected default MaxConnectionsPerIP to be 50, got %d", limits.MaxConnectionsPerIP)
	}

	if limits.MaxStreamsPerConn != 100 {
		t.Errorf("Expected default MaxStreamsPerConn to be 100, got %d", limits.MaxStreamsPerConn)
	}
}

func TestManager_Concurrent(t *testing.T) {
	mgr := NewManager(Limits{
		MaxConnections:      100,
		MaxConnectionsPerIP: 50,
		ConnectionRate:      200,
	})

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn := &Connection{
				ID:         string(rune('A' + id)),
				RemoteAddr: "192.168.1." + string(rune('0'+id%4)),
			}
			mgr.OnConnect(conn)
		}(i)
	}

	wg.Wait()

	stats := mgr.Stats()
	if stats.TotalConnections != 50 {
		t.Errorf("Expected 50 connections, got %d", stats.TotalConnections)
	}
}
