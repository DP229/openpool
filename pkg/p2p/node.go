// Package p2p provides libp2p-based P2P networking with NAT traversal.
package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	multiaddr "github.com/multiformats/go-multiaddr"
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
	ctx, cancel := context.WithCancel(context.Background())
	return &Node{
		Ledger:    ledger,
		tasks:     make(map[string]*Task),
		taskChans: make(map[string]chan *TaskResult),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// ── Listen ────────────────────────────────────────────────────────────────────

// Listen starts the libp2p host with circuit relay NAT traversal.
func (n *Node) Listen(port int) error {
	h, err := libp2p.New(
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.EnableRelay(),
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)),
	)
	if err != nil {
		return fmt.Errorf("libp2p new: %w", err)
	}

	n.Host = h
	h.SetStreamHandler(protocol.ID(ProtocolID), n.handleStream)

	log.Printf("[%s] libp2p ready", n.ID[:6])
	for _, addr := range h.Addrs() {
		log.Printf("  listen: %s/p2p/%s", addr, h.ID().String())
	}

	return nil
}

// ── Bootstrap ─────────────────────────────────────────────────────────────────

// Bootstrap connects to a list of bootstrap peers.
// After connecting, the peerstore will have their address from the connection.
func (n *Node) Bootstrap(peers []string) {
	for _, addr := range peers {
		ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
		if err := n.Connect(ctx, addr); err != nil {
			log.Printf("[%s] bootstrap %s: %v", n.ID[:6], addr, err)
			cancel()
			continue
		}
		cancel()
		log.Printf("[%s] connected to %s", n.ID[:6], extractPeerID(addr))
	}
}

// Connect connects to a peer by multiaddr string.
func (n *Node) Connect(ctx context.Context, multiaddrStr string) error {
	addr, err := multiaddr.NewMultiaddr(multiaddrStr)
	if err != nil {
		return fmt.Errorf("invalid addr: %w", err)
	}

	pidStr := extractPeerID(multiaddrStr)
	if pidStr == "" {
		return fmt.Errorf("no peer ID in addr")
	}

	pid, err := peer.Decode(pidStr)
	if err != nil {
		return fmt.Errorf("decode peer ID: %w", err)
	}

	info := peer.AddrInfo{ID: pid, Addrs: []multiaddr.Multiaddr{addr}}
	return n.Host.Connect(ctx, info)
}

// exchangeHello opens a short-lived stream to the given peer, sends our hello
// with all listen addresses, and reads their hello to populate our peerstore.
func (n *Node) exchangeHello(ctx context.Context, pid peer.ID) {
	s, err := n.Host.NewStream(ctx, pid, protocol.ID(ProtocolID))
	if err != nil {
		log.Printf("[%s] hello exchange with %s: %v", n.ID[:6], pid.String()[:16], err)
		return
	}
	defer s.Close()

	// Send hello with all our listen addresses
	hello := HelloMsg{
		Type:     "hello",
		ID:       n.ID,
		Credits:  n.Ledger.GetCredits(n.ID),
		Multiaddrs: n.allAddrsForPeer(pid),
	}
	if err := json.NewEncoder(s).Encode(hello); err != nil {
		log.Printf("[%s] hello send: %v", n.ID[:6], err)
		return
	}

	// Read their hello and add their addresses to our peerstore
	var remote HelloMsg
	if err := json.NewDecoder(s).Decode(&remote); err != nil {
		log.Printf("[%s] hello recv: %v", n.ID[:6], err)
		return
	}

	if remote.Multiaddrs != nil {
		remoteInfo := peer.AddrInfo{
			ID:    pid,
			Addrs: remote.Multiaddrs,
		}
		n.Host.Peerstore().AddAddrs(pid, remoteInfo.Addrs, 24*time.Hour)
	}

	log.Printf("[%s] hello exchanged with %s (%d addrs)", n.ID[:6], pid.String()[:16], len(remote.Multiaddrs))
}

// allAddrsForPeer returns our listen addrs with the peer's ID appended.
func (n *Node) allAddrsForPeer(pid peer.ID) []multiaddr.Multiaddr {
	var addrs []multiaddr.Multiaddr
	for _, addr := range n.Host.Addrs() {
		addrs = append(addrs, addr)
	}
	_ = pid // not needed — addr already has our transport addr
	return addrs
}

// ── Protocol Messages ────────────────────────────────────────────────────────

type HelloMsg struct {
	Type      string              `json:"type"`
	ID        string              `json:"id"`
	Credits   int                 `json:"credits"`
	Multiaddrs []multiaddr.Multiaddr `json:"multiaddrs,omitempty"`
}

// Task represents a compute task.
type Task struct {
	ID          string          `json:"id"`
	Code        string          `json:"code,omitempty"`
	Lang        string          `json:"lang,omitempty"`
	TimeoutSec  int             `json:"timeout_sec"`
	Credits     int             `json:"credits"`
	State       string          `json:"state"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// TaskResult is the result of a completed task.
type TaskResult struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// ── Stream Handler ───────────────────────────────────────────────────────────

func (n *Node) handleStream(s network.Stream) {
	defer s.Close()

	// Decode into a generic map first to determine message type
	var raw map[string]interface{}
	if err := json.NewDecoder(s).Decode(&raw); err != nil {
		log.Printf("[%s] stream decode: %v", n.ID[:6], err)
		return
	}

	msgType, _ := raw["type"].(string)
	switch msgType {
	case "hello":
		b, _ := json.Marshal(raw)
		var h HelloMsg
		json.Unmarshal(b, &h)
		n.onHello(s, &h)

	case "task_req":
		if taskData, ok := raw["task"].(map[string]interface{}); ok {
			b, _ := json.Marshal(taskData)
			var t Task
			json.Unmarshal(b, &t)
			n.handleTaskRequest(s, &t)
		}

	default:
		log.Printf("[%s] unknown msg type: %s", n.ID[:6], msgType)
	}
}

func (n *Node) onHello(s network.Stream, hello *HelloMsg) {
	// Add their addresses to our peerstore
	if hello.Multiaddrs != nil && len(hello.Multiaddrs) > 0 {
		pid := s.Conn().RemotePeer()
		n.Host.Peerstore().AddAddrs(pid, hello.Multiaddrs, 24*time.Hour)
	}

	// Respond with our hello
	resp := HelloMsg{
		Type:       "hello",
		ID:         n.ID,
		Credits:    n.Ledger.GetCredits(n.ID),
		Multiaddrs: n.Host.Addrs(),
	}
	json.NewEncoder(s).Encode(resp)
}

func decodeTaskFromStream(s network.Stream) (Task, error) {
	var t Task
	decoder := json.NewDecoder(s)
	err := decoder.Decode(&t)
	return t, err
}

func (n *Node) handleTaskRequest(s network.Stream, task *Task) {
	if n.Ledger.GetCredits(n.ID) < task.Credits {
		json.NewEncoder(s).Encode(map[string]string{
			"type": "task_resp", "id": task.ID, "error": "insufficient credits",
		})
		return
	}

	// Execute task
	result, err := n.executeTask(task)

	// Reward executor (submitter already deducted from itself)
	n.Ledger.AddCredits(n.ID, task.Credits)

	if err != nil {
		resp := &TaskResult{ID: task.ID, Success: false, Error: err.Error()}
		json.NewEncoder(s).Encode(map[string]interface{}{"type": "task_resp", "result": resp})
	} else {
		resp := &TaskResult{ID: task.ID, Success: true, Result: result}
		json.NewEncoder(s).Encode(map[string]interface{}{"type": "task_resp", "result": resp})
	}
	// Close write side so submitter's goroutine gets EOF after reading result
	s.CloseWrite()
}

func (n *Node) handleTaskResult(result *TaskResult) {
	n.mu.Lock()
	ch, ok := n.taskChans[result.ID]
	if ok {
		ch <- result
		delete(n.taskChans, result.ID)
	}
	n.mu.Unlock()
}

// ── Task Submission ───────────────────────────────────────────────────────────

// SubmitTask submits a task to a peer and waits for the result.
// Opens a stream, sends the task, reads response concurrently (goroutine),
// so write and read can happen simultaneously without deadlock.
func (n *Node) SubmitTask(ctx context.Context, peerID string, task *Task) error {
	pid, err := peer.Decode(peerID)
	if err != nil {
		return fmt.Errorf("decode peer: %w", err)
	}

	s, err := n.Host.NewStream(ctx, pid, protocol.ID(ProtocolID))
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer s.Close()

	// Deduct credits locally first (submitter pays)
	n.Ledger.AddCredits(n.ID, -task.Credits)

	// Send task request
	if err := json.NewEncoder(s).Encode(map[string]interface{}{
		"type": "task_req",
		"task": task,
	}); err != nil {
		// Refund on send failure
		n.Ledger.AddCredits(n.ID, task.Credits)
		return fmt.Errorf("send task: %w", err)
	}
	s.CloseWrite() // executor now sees EOF on its read side

	// Read result in a goroutine so write/read happen simultaneously
	type respMsg struct {
		Type   string       `json:"type"`
		Result *TaskResult `json:"result,omitempty"`
	}
	resultCh := make(chan respMsg, 1)
	go func() {
		var resp respMsg
		if err := json.NewDecoder(s).Decode(&resp); err != nil {
			resultCh <- respMsg{Type: "error"}
			return
		}
		resultCh <- resp
	}()

	// Wait for result or timeout
	select {
	case resp := <-resultCh:
		if resp.Type == "error" {
			return fmt.Errorf("read result: decoder error")
		}
		if resp.Result != nil {
			task.Result = resp.Result.Result
			task.Error = resp.Result.Error
			if resp.Result.Success {
				task.State = "completed"
			} else {
				task.State = "failed"
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ── Task Execution ───────────────────────────────────────────────────────────

func (n *Node) executeTask(task *Task) (json.RawMessage, error) {
	if task.TimeoutSec == 0 {
		task.TimeoutSec = 60
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(task.TimeoutSec)*time.Second)
	defer cancel()

	switch task.Lang {
	case "python", "py", "":
		return n.runPythonTask(ctx, task)
	default:
		return nil, fmt.Errorf("unsupported lang: %s", task.Lang)
	}
}

func (n *Node) runPythonTask(ctx context.Context, task *Task) (json.RawMessage, error) {
	switch task.ID {
	case "builtin-fib":
		return n.runFibTask(ctx)
	case "builtin-matrix":
		return n.runMatrixTask(ctx)
	default:
		if task.Code != "" {
			return n.runGenericPython(ctx, task.Code)
		}
		return n.runFibTask(ctx)
	}
}

func (n *Node) runFibTask(ctx context.Context) (json.RawMessage, error) {
	script := `
import json, time
def fib(n):
    a, b = 0, 1
    for _ in range(n):
        a, b = b, a + b
    return a
t0 = time.time()
r20 = fib(20)
r35 = fib(35)
elapsed = (time.time() - t0) * 1000
print(json.dumps({
    "fib_20": r20,
    "fib_35": r35,
    "status": "ok",
    "runtime": "python3",
    "elapsed_ms": round(elapsed, 2)
}))`
	return runScript(ctx, script)
}

func (n *Node) runMatrixTask(ctx context.Context) (json.RawMessage, error) {
	script := `
import json, time
def matrix_trace(n):
    return sum(row[i] for i, row in enumerate([[j for j in range(i,i+n)] for i in range(n)]))
t0 = time.time()
r = matrix_trace(100)
elapsed = (time.time() - t0) * 1000
print(json.dumps({
    "trace": r,
    "status": "ok",
    "runtime": "python3",
    "elapsed_ms": round(elapsed, 2)
}))`
	return runScript(ctx, script)
}

func (n *Node) runGenericPython(ctx context.Context, code string) (json.RawMessage, error) {
	script := fmt.Sprintf(`
import json
%s
print(json.dumps({"status": "ok", "output": str(result)}))`, code)
	return runScript(ctx, script)
}

func runScript(ctx context.Context, script string) (json.RawMessage, error) {
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%v | %s", err, string(out))
	}
	return json.RawMessage(out), nil
}

// ── Utilities ─────────────────────────────────────────────────────────────────

// Multiaddrs returns all listen multiaddrs for this node.
func (n *Node) Multiaddrs() []string {
	if n.Host == nil {
		return nil
	}
	var addrs []string
	for _, addr := range n.Host.Addrs() {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", addr, n.Host.ID().String()))
	}
	return addrs
}

// PeerInfo returns the primary multiaddr for this node.
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

// extractPeerID pulls the peer ID component from a multiaddr string.
func extractPeerID(addr string) string {
	if i := strings.Index(addr, "/p2p/"); i >= 0 {
		rest := addr[i+len("/p2p/"):]
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return addr
}
