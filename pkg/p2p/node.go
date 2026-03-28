// Package p2p provides libp2p-based P2P networking with NAT traversal.
package p2p

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"bytes"
	"exec"
	"fmt"
	"strconv"
	"strings"
	"time"

	libp2plib "github.com/libp2p/go-libp2p"
	multiaddr "github.com/multiformats/go-multiaddr"
	"github.com/libp2p/go-libp2p-core/host"
	corepeer "github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/libp2p/go-libp2p-relay/v2"
	"github.com/libp2p/go-libp2p-tcp"
)

// ProtocolID is the OpenPool libp2p protocol identifier.
const ProtocolID = "/openpool/1.0"

// ── Node ─────────────────────────────────────────────────────────────────────

// Node is a libp2p-backed P2P node with NAT traversal.
type Node struct {
	ID     string
	Host   host.Host
	Ledger LedgerDB

	tasks     map[string]*Task
	taskChans map[string]chan *TaskResult

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// LedgerDB is the database interface.
type LedgerDB interface {
	AddCredits(id string, amt int) int
	GetCredits(id string) int
}

// NewNode creates a new libp2p P2P node.
func NewNode(ledger LedgerDB) *Node {
	idBytes := make([]byte, 8)
	rand.Read(idBytes)

	ctx, cancel := context.WithCancel(context.Background())

	return &Node{
		ID:        hex.EncodeToString(idBytes),
		Ledger:    ledger,
		tasks:     make(map[string]*Task),
		taskChans: make(map[string]chan *TaskResult),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Listen starts the libp2p host with NAT traversal enabled.
func (n *Node) Listen(port int) error {
	// Enable NAT port mapping (UPnP/NAT-PMP) — NAT traversal step 1
	natOpt, _ := libp2plib.NATPortMap() // best effort

	// Enable NAT service (relay server) — NAT traversal step 2
	swarmOpt, err := swarm.EnableNATService()
	if err != nil {
		return fmt.Errorf("NAT service: %w", err)
	}

	// Enable circuit relay v2 — NAT traversal step 3 (fallback for symmetric NAT)
	relayOpt := relayv2.EnableRelay()

	// Enable TCP transport with no socket reuse (sandboxing)
	tcpOpt := tcp.NewTCP

	h, err := libp2plib.New(
		// Transports
		libp2plib.Transport(tcpOpt),

		// NAT traversal
		natOpt,    // UPnP/NAT-PMP port mapping
		swarmOpt,  // Active NAT service (relay for others)
		relayOpt,  // Circuit relay v2 (connect through relay)

func (n *Node) Listen(port int) error {
	// Circuit relay v2 — enables NAT traversal for all peers
	relayOpt := relayv2.EnableRelay()
	tcpOpt := tcp.NewTCP
		relayOpt,
		libp2plib.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)),
	)
	if err != nil {
		return fmt.Errorf("libp2p new: %w", err)
	}

	n.Host = h
	h.SetStreamHandler(protocol.ID(ProtocolID), n.handleStream)

	}

	return nil
}

	addr, err := multiaddr.NewMultiaddr(multiaddrStr)
	if err != nil {
		return fmt.Errorf("invalid addr: %w", err)
	}

	pidStr := extractPeerID(multiaddrStr)
	if pidStr == "" {
		return fmt.Errorf("no peer ID in multiaddr: %s", multiaddrStr)
	}

	pid, err := corepeer.Decode(pidStr)
	if err != nil {
		return fmt.Errorf("invalid peer ID: %w", err)
	}

	info := corepeer.AddrInfo{ID: pid, Addrs: []multiaddr.Multiaddr{addr}}
	n.Host.Peerstore().AddAddrs(pid, info.Addrs, time.Hour*24)

	ctx, cancel := context.WithTimeout(n.ctx, 15*time.Second)
	defer cancel()

	if err := n.Host.Connect(ctx, info); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	log.Printf("[%s] ✓ connected to %s", n.ID[:6], pid.Pretty()[:16])
	return nil
}

// SubmitTask sends a task to a peer and waits for the result.
func (n *Node) SubmitTask(ctx context.Context, peerID string, task *Task) error {
	pid, err := corepeer.Decode(peerID)
	if err != nil {
		return fmt.Errorf("invalid peer: %w", err)
	}

	resultCh := make(chan *TaskResult, 1)
	n.mu.Lock()
	n.tasks[task.ID] = task
	n.taskChans[task.ID] = resultCh
	n.mu.Unlock()

	s, err := n.Host.NewStream(ctx, pid, protocol.ID(ProtocolID))
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer s.Close()

	enc := json.NewEncoder(s)
	dec := json.NewDecoder(s)

	// Hello handshake
	enc.Encode(Msg{Type: "hello", From: n.ID, Payload: mustMarshal(Hello{
		NodeID: n.ID, Credits: n.Ledger.GetCredits(n.ID),
		CPUCores: runtime.NumCPU(), RAMFreeMB: getFreeRAM(),
		WASMEnabled: true, Country: "IN",
	})})

	var helloResp Msg
	if err := dec.Decode(&helloResp); err != nil {
		return fmt.Errorf("hello: %w", err)
	}

	// Send task
	if err := enc.Encode(Msg{Type: "task_req", From: n.ID,
		Payload: mustMarshal(TaskReq{Task: *task})}); err != nil {
		return fmt.Errorf("send task: %w", err)
	}

	log.Printf("[%s] → task %s → %s", n.ID[:6], task.ID[:8], pid.Pretty()[:12])

	// Wait for result
	select {
	case result := <-resultCh:
		n.mu.Lock()
		task.State = result.State
		task.Result = result.Result
		task.Error = result.Error
		delete(n.taskChans, task.ID)
		delete(n.tasks, task.ID)
		n.mu.Unlock()
		if result.State != "done" {
			return fmt.Errorf("task failed: %s", result.Error)
		}
		return nil

	case <-ctx.Done():
		return ctx.Err()

	case <-time.After(5 * time.Minute):
		return fmt.Errorf("task timeout after 5 minutes")
	}
}

// Multiaddrs returns the node's listen addresses as shareable multiaddr strings.
func (n *Node) Multiaddrs() []string {
	var addrs []string
	for _, addr := range n.Host.Addrs() {
		addrs = append(addrs,
			fmt.Sprintf("%s/p2p/%s", addr, n.Host.ID().Pretty()))
	}
	return addrs
}

// PeerInfo returns the first listen multiaddr (for sharing).
func (n *Node) PeerInfo() string {
	addrs := n.Multiaddrs()
	if len(addrs) > 0 {
		return addrs[0]
	}
	return ""
}

// Close shuts down the node.
func (n *Node) Close() error {
	n.cancel()
	if n.Host != nil {
		return n.Host.Close()
	}
	return nil
}

// ── Stream handler ───────────────────────────────────────────────────────────

func (n *Node) handleStream(s network.Stream) {
	defer s.Close()

	dec := json.NewDecoder(s)
	enc := json.NewEncoder(s)
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
			log.Printf("[%s] ← hello from %s (%s, cores=%d)",
				n.ID[:6], h.NodeID[:6], h.Country, h.CPUCores)

			enc.Encode(Msg{Type: "hello", From: n.ID, Payload: mustMarshal(Hello{
				NodeID: n.ID, Credits: n.Ledger.GetCredits(n.ID),
				CPUCores: runtime.NumCPU(), RAMFreeMB: getFreeRAM(),
				WASMEnabled: true, Country: "IN",
			})})

		case "task_req":
			var req TaskReq
			json.Unmarshal(msg.Payload, &req)
			task := &req.Task
			log.Printf("[%s] ← task %s from %s (credits=%d)",
				n.ID[:6], task.ID[:8], peerID, task.Credits)

			result, err := runPythonTest(n.ctx)
			state := "done"
			errStr := ""
			if err != nil {
				state = "failed"
				errStr = err.Error()
			}

			n.Ledger.AddCredits(n.ID, task.Credits)
			log.Printf("[%s]   💰 +%d credits (balance: %d) [%s]",
				n.ID[:6], task.Credits, n.Ledger.GetCredits(n.ID), state)

			enc.Encode(Msg{Type: "task_resp", From: n.ID, Payload: mustMarshal(TaskResp{
				TaskID: task.ID, State: state,
				Result: result, Error: errStr, Credits: task.Credits,
			})})

		case "task_resp":
			var resp TaskResp
			json.Unmarshal(msg.Payload, &resp)

			n.mu.Lock()
			task, ok := n.tasks[resp.TaskID]
			ch, hasCh := n.taskChans[resp.TaskID]
			delete(n.taskChans, resp.TaskID)
			delete(n.tasks, resp.TaskID)
			n.mu.Unlock()

			if !ok {
				return
			}

			task.State = resp.State
			task.Result = resp.Result
			task.Error = resp.Error

			if resp.State == "done" {
				n.Ledger.AddCredits(n.ID, -resp.Credits)
				log.Printf("[%s] ✓ task %s done — balance: %d",
					n.ID[:6], resp.TaskID[:8], n.Ledger.GetCredits(n.ID))
			} else {
				log.Printf("[%s] ✗ task %s failed: %s",
					n.ID[:6], resp.TaskID[:8], resp.Error)
			}

			if hasCh {
				ch <- &TaskResult{State: resp.State, Result: resp.Result, Error: resp.Error}
			}
		}
	}
}

// ── Types ─────────────────────────────────────────────────────────────────────

type Msg struct {
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

type Task struct {
	ID         string          `json:"id"`
	WASMPath   string          `json:"wasm_path"`
	Input      json.RawMessage `json:"input"`
	TimeoutSec int             `json:"timeout_sec"`
	State      string          `json:"state"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	Credits    int             `json:"credits"`
}

type TaskReq  struct{ Task Task }
type TaskResp struct {
	TaskID  string          `json:"task_id"`
	State   string          `json:"state"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string         `json:"error,omitempty"`
	Credits int            `json:"credits"`
}

type TaskResult struct {
	State  string          `json:"state"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func extractPeerID(addr string) string {
	if i := strings.Index(addr, "/p2p/"); i >= 0 {
		rest := addr[i+len("/p2p/"):]
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return ""
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

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func runPythonTest(ctx context.Context) (json.RawMessage, error) {
	script := `import json
def fib(n):
    a,b=0,1
    for _ in range(n): a,b=b,a+b
    return a
result={"fib_20":fib(20),"fib_35":fib(35),"status":"ok","runtime":"python3","node":"openpool-libp2p"}
print(json.dumps(result))`

	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("python: %v | %s", err, string(out))
	}
	return json.RawMessage(out), nil
}

// ── Unused ──────────────────────────────────────────────────────────────────
var _ = sync.RWMutex{}
