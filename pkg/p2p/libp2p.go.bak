// Package p2p provides libp2p-based P2P networking for OpenPool.
package p2p

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	p2praft "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"

	"github.com/dp229/openpool/pkg/ledger"
)

// OpenPool protocol ID
const ProtocolID = "/openpool/1.0"

var _ Protocol = (*LibP2PNode)(nil)

// Protocol defines the P2P networking interface.
type Protocol interface {
	Host() host.Host
	NodeID() string
	Listen(ctx context.Context) error
	Connect(ctx context.Context, addr string) error
	SubmitTask(ctx context.Context, peerID string, task *Task) error
	Close() error
}

// LibP2PNode is a libp2p-backed P2P node.
type LibP2PNode struct {
	id   string
	host host.Host
	dht  *dht.IpfsDHT
	db   ledger.DB

	// Task result handlers
	resultHandlers map[string]chan *TaskResult

	mu    sync.RWMutex
	tasks map[string]*Task

	ctx    context.Context
	cancel context.CancelFunc
}

// ledger.DB interface we need
type LedgerDB interface {
	AddCredits(id string, amt int) int
	GetCredits(id string) int
}

// NewLibP2P creates a new libp2p P2P node.
func NewLibP2P(port int, db LedgerDB) (*LibP2PNode, error) {
	idBytes := make([]byte, 8)
	rand.Read(idBytes)
	nodeID := hex.EncodeToString(idBytes)

	ctx, cancel := context.WithCancel(context.Background())

	n := &LibP2PNode{
		id:             nodeID,
		db:             nil, // assign externally
		resultHandlers:  make(map[string]chan *TaskResult),
		tasks:          make(map[string]*Task),
		ctx:            ctx,
		cancel:          cancel,
	}

	return n, nil
}

// Host returns the libp2p host.
func (n *LibP2PNode) Host() host.Host { return n.host }

// NodeID returns this node's peer ID.
func (n *LibP2PNode) NodeID() string { return n.id }

// Listen starts the libp2p host and sets up the protocol handler.
func (n *LibP2PNode) Listen(ctx context.Context) error {
	// Determine listen addresses
	var listenOpts []libp2p.Option

	// TCP listener
	listenOpts = append(listenOpts, libp2p.ListenAddrStrings(
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", 0), // port 0 = OS-assigned
	))

	// Connection manager for rate limiting
	cm, err := connmgr.NewConnManager(10, 100)
	if err == nil {
		listenOpts = append(listenOpts, libp2p.ConnectionManager(cm))
	}

	// NAT traversal (hole punching, relay)
	listenOpts = append(listenOpts,
		libp2p.EnableNATService(),
		libp2p.EnableRelay(), // circuit relay for NAT-ed peers
	)

	// Auto NAT port mapping
	listenOpts = append(listenOpts,
		libp2p.NATPortMap(), // use UPnP/NAT-PMP if available
	)

	h, err := libp2p.New(listenOpts...)
	if err != nil {
		return fmt.Errorf("libp2p listen: %w", err)
	}

	n.host = h

	// Register OpenPool protocol handler
	h.SetStreamHandler(protocol.ID(ProtocolID), n.handleStream)

	log.Printf("[%s] libp2p listening on:", n.id[:6])
	for _, addr := range h.Addrs() {
		log.Printf("  %s/p2p/%s", addr, h.ID().Pretty())
	}

	return nil
}

// Bootstrap connects to a bootstrap node and optionally starts a DHT.
func (n *LibP2PNode) Bootstrap(ctx context.Context, bootstrapAddrs []string) error {
	if len(bootstrapAddrs) == 0 {
		return nil
	}

	// Create a DHT for peer discovery (optional, Phase 1)
	// For Phase 0, we just connect directly
	for _, addrStr := range bootstrapAddrs {
		if err := n.connectToAddr(ctx, addrStr); err != nil {
			log.Printf("[%s] bootstrap connect failed: %v", n.id[:6], err)
		} else {
			log.Printf("[%s] ✓ connected to bootstrap: %s", n.id[:6], addrStr)
		}
	}

	return nil
}

// Connect connects to a peer by multiaddr string.
func (n *LibP2PNode) Connect(ctx context.Context, addr string) error {
	return n.connectToAddr(ctx, addr)
}

func (n *LibP2PNode) connectToAddr(ctx context.Context, addrStr string) error {
	addr, err := multiaddr.NewMultiaddr(addrStr)
	if err != nil {
		return fmt.Errorf("invalid addr %s: %w", addrStr, err)
	}

	pidStr := extractPeerID(addrStr)
	if pidStr == "" {
		return fmt.Errorf("no peer ID in addr: %s", addrStr)
	}

	pid, err := peer.Decode(pidStr)
	if err != nil {
		return fmt.Errorf("invalid peer ID %s: %w", pidStr, err)
	}

	info := peer.AddrInfo{ID: pid, Addrs: []multiaddr.Multiaddr{addr}}
	n.host.Peerstore().AddAddrs(pid, info.Addrs, time.Hour*24)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := n.host.Connect(ctx, info); err != nil {
		return fmt.Errorf("connect to %s: %w", pidStr[:12], err)
	}

	log.Printf("[%s] ✓ connected to %s", n.id[:6], pid.Pretty()[:12])
	return nil
}

// SubmitTask sends a task to a peer by peer ID.
func (n *LibP2PNode) SubmitTask(ctx context.Context, peerIDStr string, task *Task) error {
	pid, err := peer.Decode(peerIDStr)
	if err != nil {
		return fmt.Errorf("invalid peer ID: %w", err)
	}

	// Create result channel
	resultCh := make(chan *TaskResult, 1)
	n.mu.Lock()
	n.resultHandlers[task.ID] = resultCh
	n.tasks[task.ID] = task
	n.mu.Unlock()

	// Open stream
	s, err := n.host.NewStream(ctx, pid, protocol.ID(ProtocolID))
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer s.Close()

	// Send hello + task
	enc := json.NewEncoder(s)
	dec := json.NewDecoder(s)

	hello := Msg{
		Type: "hello",
		From: n.id,
		Payload: mustMarshal(Hello{
			NodeID:      n.id,
			Credits:     100,
			CPUCores:    8,
			RAMFreeMB:   16000,
			WASMEnabled:  true,
			Country:     "IN",
		}),
	}
	if err := enc.Encode(hello); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	// Wait for hello response
	var helloResp Msg
	if err := dec.Decode(&helloResp); err != nil {
		return fmt.Errorf("hello response: %w", err)
	}

	// Send task
	taskMsg := Msg{
		Type:    "task_req",
		From:    n.id,
		Payload: mustMarshal(TaskReq{Task: *task}),
	}
	if err := enc.Encode(taskMsg); err != nil {
		return fmt.Errorf("send task: %w", err)
	}

	log.Printf("[%s] → task %s to %s", n.id[:6], task.ID[:8], pid.Pretty()[:12])

	// Wait for result
	select {
	case result := <-resultCh:
		task.State = result.State
		task.Result = result.Result
		task.Error = result.Error
		if result.State == "done" {
			return nil
		}
		return fmt.Errorf("task failed: %s", result.Error)
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(60 * time.Second):
		return fmt.Errorf("task timeout after 60s")
	}
}

// handleStream processes incoming streams.
func (n *LibP2PNode) handleStream(s network.Stream) {
	defer s.Close()

	enc := json.NewEncoder(s)
	dec := json.NewDecoder(s)
	peerID := s.Conn().RemotePeer().Pretty()[:12]

	for {
		var msg Msg
		if err := dec.Decode(&msg); err != nil {
			return
		}

		switch msg.Type {
		case "hello":
			var h Hello
			json.Unmarshal(msg.Payload, &h)
			log.Printf("[%s] ← hello from %s (%s)", n.id[:6], h.NodeID[:6], h.Country)

			// Respond
			enc.Encode(Msg{
				Type: "hello",
				From: n.id,
				Payload: mustMarshal(Hello{
					NodeID:      n.id,
					Credits:     100,
					CPUCores:    8,
					RAMFreeMB:   16000,
					WASMEnabled:  true,
					Country:     "IN",
				}),
			})

		case "task_req":
			var req TaskReq
			json.Unmarshal(msg.Payload, &req)
			log.Printf("[%s] ← task %s from %s", n.id[:6], req.Task.ID[:8], peerID)

			// Execute task
			result, err := n.runTask(&req.Task)
			state := "done"
			errStr := ""
			if err != nil {
				state = "failed"
				errStr = err.Error()
			}

			enc.Encode(Msg{
				Type:    "task_resp",
				From:    n.id,
				Payload: mustMarshal(TaskResp{
					TaskID:  req.Task.ID,
					State:   state,
					Result:  result,
					Error:   errStr,
					Credits: req.Task.Credits,
				}),
			})

		case "task_resp":
			var resp TaskResp
			json.Unmarshal(msg.Payload, &resp)

			n.mu.Lock()
			if ch, ok := n.resultHandlers[resp.TaskID]; ok {
				ch <- &TaskResult{
					State:  resp.State,
					Result: resp.Result,
					Error:  resp.Error,
				}
				delete(n.resultHandlers, resp.TaskID)
			}
			n.mu.Unlock()

			log.Printf("[%s] ✓ task %s result: %s", n.id[:6], resp.TaskID[:8], resp.State)
		}
	}
}

// runTask executes a task and returns the result.
func (n *LibP2PNode) runTask(task *Task) (json.RawMessage, error) {
	// Phase 0: use Python fallback
	return runPythonTest(n.ctx)
}

// Close shuts down the node.
func (n *LibP2PNode) Close() error {
	n.cancel()
	if n.host != nil {
		return n.host.Close()
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────

func extractPeerID(addr string) string {
	if strings.Contains(addr, "/p2p/") {
		parts := strings.Split(addr, "/p2p/")
		return strings.Split(parts[len(parts)-1], "/")[0]
	}
	return ""
}

// ── Re-export types from pkg/net ─────────────────────────────────────────

type Task = Task_
type TaskResult = TaskResult_
type Msg = Msg_

type Task_ struct {
	ID         string          `json:"id"`
	WASMPath   string          `json:"wasm_path"`
	Input      json.RawMessage `json:"input"`
	TimeoutSec int             `json:"timeout_sec"`
	State      string          `json:"state"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	Credits    int             `json:"credits"`
}

type TaskResult_ struct {
	State  string          `json:"state"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type Msg_ struct {
	Type    string          `json:"type"`
	From    string          `json:"from"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Hello struct {
	NodeID      string `json:"node_id"`
	Credits     int    `json:"credits"`
	CPUCores    int    `json:"cpu_cores"`
	RAMFreeMB   int    `json:"ram_free_mb"`
	WASMEnabled bool   `json:"wasm_enabled"`
	Country     string `json:"country"`
}

type TaskReq struct {
	Task Task `json:"task"`
}

type TaskResp struct {
	TaskID  string          `json:"task_id"`
	State   string          `json:"state"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string         `json:"error,omitempty"`
	Credits int            `json:"credits"`
}

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// ── Import stubs (to satisfy compiler until we use these) ─────────────────
var (
	_ multiaddr.Multiaddr
	_ dht.IpfsDHT
	_ p2praft.DHTModeOpt
)

import (
	"encoding/json"
	"os/exec"
	"context"
	"bytes"
	"fmt"
)

func runPythonTest(ctx context.Context) (json.RawMessage, error) {
	script := `
import json
def fib(n):
    a,b=0,1
    for _ in range(n): a,b=b,a+b
    return a
def trace(s):
    return sum(i*s+i for i in range(s))
result={"fib_20":fib(20),"fib_35":fib(35),"trace_10":trace(10),"status":"ok","node":"openpool-libp2p"}
print(json.dumps(result))
`
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("python: %v | %s", err, string(out))
	}
	return json.RawMessage(bytes.TrimSpace(out)), nil
}

import "github.com/multiformats/go-multiaddr"
