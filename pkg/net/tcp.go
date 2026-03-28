// Package net provides a simple TCP-based P2P node for OpenPool Phase 0.
package net

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Public Types ─────────────────────────────────────────────────────────────

type Node struct {
	ID   string
	Port int

	DB interface {
		AddCredits(id string, amt int) int
		GetCredits(id string) int
	}

	mu     sync.RWMutex
	peers  map[string]*PeerConn
	tasks  map[string]*Task
	ctx    context.Context
	cancel context.CancelFunc
	Ctx    context.Context
	Cancel context.CancelFunc
	ln     net.Listener
}

type PeerConn struct {
	ID   string
	Conn net.Conn
	Enc  *json.Encoder
	Dec  *json.Decoder
}

type Msg struct {
	Type    string          `json:"type"` // hello | task_req | task_resp | ping | pong
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
	CreatedAt  time.Time       `json:"created_at"`
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

// ── Constructor ───────────────────────────────────────────────────────────────

func New(port int) *Node {
	id := make([]byte, 8)
	rand.Read(id)
	return &Node{
		ID:    hex.EncodeToString(id),
		Port:  port,
		peers: make(map[string]*PeerConn),
		tasks: make(map[string]*Task),
	}
}

// ── Listen ──────────────────────────────────────────────────────────────────

func (n *Node) Tasks() map[string]*Task {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.tasks
}

func (n *Node) PeerCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.peers)
}

func (n *Node) PeerIDs() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	ids := make([]string, 0, len(n.peers))
	for id := range n.peers {
		ids = append(ids, id)
	}
	return ids
}

func (n *Node) RunTask(ctx context.Context, task *Task) (json.RawMessage, error) {
	runCtx := n.Ctx
	if runCtx == nil {
		runCtx = ctx
	}
	return n.runTask(runCtx, task)
}

func (n *Node) Listen(ctx context.Context) error {
	n.Ctx, n.Cancel = context.WithCancel(ctx)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", n.Port))
	if err != nil {
		return fmt.Errorf("listen :%d: %w", n.Port, err)
	}
	n.ln = ln
	go n.acceptLoop()
	log.Printf("[%s] TCP listening on :%d", n.ID[:6], n.Port)
	return nil
}

func (n *Node) acceptLoop() {
	for {
		conn, err := n.ln.Accept()
		if err != nil {
			if n.Ctx.Err() != nil {
				return
			}
			continue
		}
		go n.handleConn(conn)
	}
}

func (n *Node) handleConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var msg Msg
		if err := dec.Decode(&msg); err != nil {
			if err != io.EOF {
				log.Printf("[%s] read error: %v", n.ID[:6], err)
			}
			return
		}
		n.processMsg(conn, enc, &msg)
	}
}

func (n *Node) processMsg(conn net.Conn, enc *json.Encoder, msg *Msg) {
	switch msg.Type {

	case "hello":
		var h Hello
		json.Unmarshal(msg.Payload, &h)
		log.Printf("[%s] ← hello from %s | %s | credits=%d cores=%d",
			n.ID[:6], h.NodeID[:6], h.Country, h.Credits, h.CPUCores)

		n.mu.Lock()
		if _, ok := n.peers[h.NodeID]; !ok {
			n.peers[h.NodeID] = &PeerConn{ID: h.NodeID, Conn: conn, Enc: enc, Dec: json.NewDecoder(conn)}
		}
		n.mu.Unlock()

		enc.Encode(Msg{Type: "hello", From: n.ID, Payload: mustMarshal(Hello{
			NodeID:      n.ID,
			Credits:     n.DB.GetCredits(n.ID),
			CPUCores:    runtime.NumCPU(),
			RAMFreeMB:   getFreeRAM(),
			WASMEnabled: true,
			Country:     "IN",
		})})

	case "task_req":
		var req TaskReq
		json.Unmarshal(msg.Payload, &req)
		log.Printf("[%s] ← task_req: %s (timeout=%ds credits=%d)",
			n.ID[:6], req.Task.ID[:8], req.Task.TimeoutSec, req.Task.Credits)

		result, err := n.runTask(n.Ctx, &req.Task)
		state := "done"
		errStr := ""
		if err != nil {
			state = "failed"
			errStr = err.Error()
		}
		n.DB.AddCredits(n.ID, req.Task.Credits)
		log.Printf("[%s]   💰 +%d credits (balance: %d) [%s]",
			n.ID[:6], req.Task.Credits, n.DB.GetCredits(n.ID), state)

		enc.Encode(Msg{Type: "task_resp", From: n.ID, Payload: mustMarshal(TaskResp{
			TaskID:  req.Task.ID,
			State:   state,
			Result:  result,
			Error:   errStr,
			Credits: req.Task.Credits,
		})})

	case "task_resp":
		var resp TaskResp
		json.Unmarshal(msg.Payload, &resp)
		n.mu.Lock()
		if t, ok := n.tasks[resp.TaskID]; ok {
			t.State = resp.State
			t.Result = resp.Result
			t.Error = resp.Error
			if resp.State == "done" {
				n.DB.AddCredits(n.ID, -resp.Credits)
				log.Printf("[%s] ✓ Task %s done — spent %d credits | balance: %d",
					n.ID[:6], resp.TaskID[:8], resp.Credits, n.DB.GetCredits(n.ID))
			} else {
				log.Printf("[%s] ✗ Task %s failed: %s", n.ID[:6], resp.TaskID[:8], resp.Error)
			}
		}
		n.mu.Unlock()
	}
}

// ── Connect ─────────────────────────────────────────────────────────────────

func (n *Node) Connect(ctx context.Context, addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	// Send hello
	enc.Encode(Msg{Type: "hello", From: n.ID, Payload: mustMarshal(Hello{
		NodeID:      n.ID,
		Credits:     n.DB.GetCredits(n.ID),
		CPUCores:    runtime.NumCPU(),
		RAMFreeMB:   getFreeRAM(),
		WASMEnabled: true,
		Country:     "IN",
	})})

	// Read their hello
	var resp Msg
	if err := dec.Decode(&resp); err != nil {
		return fmt.Errorf("hello from %s: %w", addr, err)
	}

	var theirHello Hello
	json.Unmarshal(resp.Payload, &theirHello)
	log.Printf("[%s] ✓ Connected to %s (%s) | credits=%d",
		n.ID[:6], theirHello.NodeID[:6], theirHello.Country, theirHello.Credits)

	n.mu.Lock()
	n.peers[theirHello.NodeID] = &PeerConn{ID: theirHello.NodeID, Conn: conn, Enc: enc, Dec: dec}
	n.mu.Unlock()

	go n.handleConn(conn)
	return nil
}

// ── Submit Task ─────────────────────────────────────────────────────────────

func (n *Node) SubmitTask(ctx context.Context, peerID string, task *Task) error {
	task.State = "pending"
	task.CreatedAt = time.Now()

	n.mu.Lock()
	n.tasks[task.ID] = task
	n.mu.Unlock()

	peer, ok := n.peers[peerID]
	if !ok {
		return fmt.Errorf("peer %s not connected", peerID[:6])
	}

	// Re-send hello for identification
	peer.Enc.Encode(Msg{Type: "hello", From: n.ID, Payload: mustMarshal(Hello{
		NodeID: n.ID, Credits: n.DB.GetCredits(n.ID),
		CPUCores: runtime.NumCPU(), RAMFreeMB: getFreeRAM(),
		WASMEnabled: true, Country: "IN",
	})})

	var helloResp Msg
	if err := peer.Dec.Decode(&helloResp); err != nil {
		return fmt.Errorf("hello exchange: %w", err)
	}

	if err := peer.Enc.Encode(Msg{Type: "task_req", From: n.ID, Payload: mustMarshal(TaskReq{Task: *task})}); err != nil {
		return fmt.Errorf("send task: %w", err)
	}

	log.Printf("[%s] → Task %s submitted to %s", n.ID[:6], task.ID[:8], peerID[:6])
	return nil
}

// ── Task Execution ─────────────────────────────────────────────────────────

func (n *Node) runTask(taskCtx context.Context, task *Task) (json.RawMessage, error) {
	timeout := 30 * time.Second
	if task.TimeoutSec > 0 {
		timeout = time.Duration(task.TimeoutSec) * time.Second
	}
	if timeout > 4*time.Hour {
		timeout = 4 * time.Hour
	}

	ctx, cancel := context.WithTimeout(taskCtx, timeout)
	defer cancel()

	if task.WASMPath == "" {
		return runPythonTest(ctx)
	}

	// Try wasmtime first
	for _, wp := range []string{"/home/durga/bin/wasmtime", "/usr/local/bin/wasmtime", "wasmtime"} {
		cmd := exec.CommandContext(ctx, wp, "--dir", ".", task.WASMPath)
		if task.Input != nil {
			cmd.Stdin = bytes.NewReader(task.Input)
		}
		out, err := cmd.CombinedOutput()
		if ctx.Err() == nil {
			return json.RawMessage(bytes.TrimSpace(out)), nil
		}
		if err != nil && ctx.Err() == nil {
			return json.RawMessage(bytes.TrimSpace(out)), fmt.Errorf("wasmtime: %v", err)
		}
	}

	// Fallback: Python
	return runPython(ctx, task.WASMPath, task.Input)
}

// ── Close ──────────────────────────────────────────────────────────────────

func (n *Node) Close() error {
	n.Cancel()
	if n.ln != nil {
		n.ln.Close()
	}
	return nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func runPythonTest(ctx context.Context) (json.RawMessage, error) {
	script := `
import json
def fib(n):
    a, b = 0, 1
    for _ in range(n):
        a, b = b, a + b
    return a
def trace(size=10):
    return sum(i*size+i for i in range(size))
result = {"fib_20": fib(20), "fib_35": fib(35), "trace_10": trace(), "status": "computed", "runtime": "python3", "node": "openpool"}
print(json.dumps(result))
`
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("test failed: %v | %s", err, string(out))
	}
	return json.RawMessage(bytes.TrimSpace(out)), nil
}

func runPython(ctx context.Context, path string, input json.RawMessage) (json.RawMessage, error) {
	if input == nil {
		input = []byte("{}")
	}
	cmd := exec.CommandContext(ctx, "python3", path)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("python: %v", err)
	}
	return json.RawMessage(bytes.TrimSpace(out)), nil
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
