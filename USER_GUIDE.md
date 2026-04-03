# OpenPool User Guide

> **Democratizing compute -- one node at a time.**

OpenPool is a peer-to-peer distributed computing network that lets you contribute or consume processing power across a global mesh of volunteer hardware. No centralized cloud providers, no vendor lock-in -- just open, fair, and secure computation.

---

## Table of Contents

1. [Quick Start](#1-quick-start)
2. [Installation](#2-installation)
3. [Command-Line Reference](#3-command-line-reference)
4. [Running Your First Node](#4-running-your-first-node)
5. [HTTP API Reference](#5-http-api-reference)
6. [Web Dashboard](#6-web-dashboard)
7. [Task Execution](#7-task-execution)
8. [P2P Networking](#8-p2p-networking)
9. [Marketplace & Bidding](#9-marketplace--bidding)
10. [GPU Computing](#10-gpu-computing)
11. [Task Verification & Slashing](#11-task-verification--slashing)
12. [Monitoring & Metrics](#12-monitoring--metrics)
13. [Deployment to VPS](#13-deployment-to-vps)
14. [Testing](#14-testing)
15. [Architecture Overview](#15-architecture-overview)
16. [Troubleshooting](#16-troubleshooting)

---

## 1. Quick Start

```bash
# Build
go build -o openpool ./cmd/node2

# Start a node with HTTP API and DHT discovery
./openpool -http 8080 -port 9000 -dht

# Check status
curl http://localhost:8080/status
```

That's it. Your node is now running on port 9000 (P2P) with a REST API on port 8080. Open `http://localhost:8080/` in your browser for the dashboard.

---

## 2. Installation

### Prerequisites

| Requirement | Version |
|---|---|
| Go | 1.21+ |
| OS | Linux, macOS, Windows |
| RAM | 512 MB minimum |
| Disk | 100 MB |

### Build from Source

```bash
git clone https://github.com/DP229/openpool.git
cd openpool
go build -o openpool ./cmd/node2
```

### Using Make

```bash
make build       # Build the binary
make test        # Run all unit tests
make check       # Format, lint, and test
make run         # Start a local node
```

---

## 3. Command-Line Reference

All flags are optional. A node can run with zero flags (defaults to P2P-only mode).

### Core Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `-port` | int | `9000` | TCP port for P2P connections |
| `-http` | int | `0` | HTTP API port (`0` = disabled) |
| `-ledger` | string | `"openpool.db"` | SQLite file for credit tracking |
| `-credits` | int | `100` | Starting credits for new nodes |
| `-node-id` | string | auto-generated | Fixed node ID |
| `-node-id-file` | string | `<ledger>.nodeid` | Path to persist node ID across restarts |

### P2P Networking

| Flag | Type | Default | Description |
|---|---|---|---|
| `-bootstrap` | string | `""` | Bootstrap peer multiaddr (comma-separated) |
| `-connect` | string | `""` | Connect directly to a peer multiaddr |
| `-dht` | bool | `false` | Enable DHT peer discovery |
| `-discover` | bool | `false` | Discover peers via DHT (implies `-dht`) |
| `-max-peers` | int | `5` | Max peers to discover via DHT |
| `-peerstore` | string | `""` | JSON file to persist known peers |

### Execution

| Flag | Type | Default | Description |
|---|---|---|---|
| `-wasm` | string | `""` | WASM module path for local execution |
| `-gpu` | bool | `false` | Enable GPU execution |
| `-test` | bool | `false` | Run built-in test task and exit |
| `-chunked` | int | `0` | Split task into N chunks (MapReduce) |
| `-task` | string | `""` | Task JSON file for submission |
| `-send` | string | `""` | Send task to a specific peer ID |

### Marketplace

| Flag | Type | Default | Description |
|---|---|---|---|
| `-market` | bool | `false` | Enable task marketplace |
| `-price` | int | `10` | Price per task in credits |

### Verification

| Flag | Type | Default | Description |
|---|---|---|---|
| `-verify` | bool | `true` | Enable task verification |

### Utility

| Flag | Type | Default | Description |
|---|---|---|---|
| `-info` | bool | `false` | Print node info and exit |

---

## 4. Running Your First Node

### Basic Node (P2P Only)

```bash
./openpool
```

Output:
```
[abc123] ✓ Ledger: abc123 | 100 credits (initialized)

🔗 Share this to connect:
  /ip4/127.0.0.1/tcp/9000/p2p/QmYourPeerID...
```

### Full Node (HTTP + WASM + DHT + Marketplace)

```bash
./openpool \
  --http 8080 \
  --port 9000 \
  --wasm wasm/sandbox.wasm \
  --dht \
  --market \
  --price 10 \
  --ledger /opt/openpool/data/openpool.db \
  --peerstore /opt/openpool/data/peerstore.json
```

Output:
```
[abc123] ✓ Ledger: abc123 | 100 credits (initialized)
[abc123] ✓ Task verifier ready
[abc123] ✓ WASM executor ready (native mode)
[abc123] ✓ Marketplace enabled (price: 10 credits/task)
[abc123]   Hardware: 8 cores, 16GB RAM, 500GB storage
[abc123] ✓ Task queue ready (8 workers, 100 task depth)
```

### Quick Test Mode

```bash
./openpool --test
```

Runs a built-in Fibonacci task locally and exits.

### Info Mode

```bash
./openpool --info
```

Prints node ID, multiaddrs, credits, CPU, RAM, DHT status, and connected peers, then exits.

---

## 5. HTTP API Reference

Enable with `--http <port>`. All endpoints return JSON unless noted.

### Core Endpoints

#### `GET /status` -- Node Status

```bash
curl http://localhost:8080/status
```

```json
{
  "node_id": "abc123def456",
  "peer_info": "/ip4/127.0.0.1/tcp/9000/p2p/Qm...",
  "multiaddrs": ["/ip4/127.0.0.1/tcp/9000/p2p/Qm..."],
  "credits": 100,
  "cpu_cores": 8,
  "ram_mb": 4096,
  "connected_peers": 3
}
```

#### `GET /ledger` -- Credit Ledger

```bash
curl http://localhost:8080/ledger
```

```json
[
  {
    "node_id": "abc123",
    "credits": 100,
    "tasks_completed": 5,
    "tasks_failed": 0,
    "updated_at": 1712000000
  }
]
```

#### `POST /connect` -- Connect to Peer

```bash
curl -X POST http://localhost:8080/connect \
  -H "Content-Type: application/json" \
  -d '{"address": "/ip4/195.35.22.4/tcp/9000/p2p/QmBootstrap..."}'
```

```json
{"status": "connected"}
```

#### `GET /discover` -- Discover Peers

```bash
curl http://localhost:8080/discover
```

```json
{
  "peer_count": 3,
  "peers": ["QmPeer1...", "QmPeer2...", "QmPeer3..."]
}
```

### Task Execution

#### `POST /submit` -- Submit Task

```bash
curl -X POST http://localhost:8080/submit \
  -H "Content-Type: application/json" \
  -d '{"op": "fib", "arg": 30, "credits": 10, "timeout_sec": 30}'
```

```json
{
  "status": "ok",
  "result": "{\"result\":832040}",
  "credits_deducted": 10,
  "duration_ms": 12,
  "verified": true
}
```

#### `POST /run` -- Run Task Locally

```bash
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{"op": "fib", "arg": 20}'
```

```json
{"result": 6765}
```

#### `POST /run/large` -- Large Payload (up to 100MB)

```bash
curl -X POST http://localhost:8080/run/large \
  -H "Content-Type: application/json" \
  -d '{"op": "matrixTrace", "input": {"matrix": [[1,2],[3,4]]}, "credits": 5, "timeout_sec": 60}'
```

```json
{
  "status": "ok",
  "result_size": 24,
  "credits_deducted": 5
}
```

### Verification

#### `GET /verify` -- Verification History

All node stats:
```bash
curl http://localhost:8080/verify
```

Specific task:
```bash
curl "http://localhost:8080/verify?task_id=http-123456"
```

#### `GET /slashing` -- Slashing History

```bash
curl "http://localhost:8080/slashing?node_id=abc123"
```

### Stats & Monitoring

#### `GET /stats` -- Marketplace Stats

```bash
curl http://localhost:8080/stats
```

#### `GET /hardware` -- Hardware Detection

```bash
curl http://localhost:8080/hardware
```

Returns full hardware info: CPU, memory, GPU, storage, network (country/city), benchmark.

#### `GET /metrics` -- Prometheus Metrics

```bash
curl http://localhost:8080/metrics
```

```text
# HELP openpool_uptime_seconds Time since node started
# TYPE openpool_uptime_seconds gauge
openpool_uptime_seconds 3600.123

# HELP openpool_tasks_completed_total Total completed tasks
# TYPE openpool_tasks_completed_total counter
openpool_tasks_completed_total{op="fib"} 15

# HELP openpool_free_ram_mb Available RAM in MB
# TYPE openpool_free_ram_mb gauge
openpool_free_ram_mb 4096.000
```

#### `GET /queue` -- Task Queue Status

```bash
curl http://localhost:8080/queue
```

### GPU

#### `GET /gpu` -- GPU Device Info

```bash
curl http://localhost:8080/gpu
```

#### `POST /gpu/run` -- Execute GPU Operation

```bash
curl -X POST http://localhost:8080/gpu/run \
  -H "Content-Type: application/json" \
  -d '{"op": "matrixMul", "input": {"real": [1,2,3,4], "imag": [0,0,0,0]}}'
```

Supported GPU operations: `matrixMul`, `conv2d`, `gemm`, `fft`

### Marketplace

#### `GET /nodes` -- List Available Nodes

```bash
curl http://localhost:8080/nodes
```

#### `POST /tasks` -- Publish a Task

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{"task_id": "task-001", "op": "fib", "input": "{\"arg\":30}", "credits": 50, "timeout_sec": 60, "status": "pending"}'
```

#### `GET /tasks` -- List Tasks

```bash
curl "http://localhost:8080/tasks?status=pending"
```

#### `POST /bids` -- Place a Bid

```bash
curl -X POST http://localhost:8080/bids \
  -H "Content-Type: application/json" \
  -d '{"task_id": "task-001", "node_id": "abc123", "node_addr": "/ip4/...", "credits": 40, "eta_sec": 30}'
```

#### `GET /match` -- Auto-Match Best Bid

```bash
curl "http://localhost:8080/match?task_id=task-001"
```

---

## 6. Web Dashboard

Open `http://localhost:8080/` in your browser.

### Status Bar

- **Node ID** -- Unique identifier (truncated)
- **Credits** -- Current balance (green)
- **Connected Peers** -- Active P2P connections
- **CPU Cores** -- Available cores

### Action Buttons

| Button | Action |
|---|---|
| **Refresh** | Reloads all status data |
| **Discover Peers** | Triggers DHT peer discovery |
| **View Ledger** | Shows full credit ledger |

### Tabs

| Tab | Description |
|---|---|
| **Run Task** | Execute WASM tasks: `fib`, `sumFib`, `sumSquares`, `matrixTrace` |
| **Peers** | View and refresh discovered peers |
| **Connect** | Connect to a peer by multiaddr |
| **Market** | Browse nodes, publish tasks, view status |
| **GPU** | Run GPU tasks: `matrixMul`, `conv2d`, `gemm`, `fft` |

---

## 7. Task Execution

### Supported Operations

| Operation | Description | Input | Output |
|---|---|---|---|
| `fib` | Nth Fibonacci number | `{"arg": 30}` | `{"result": 832040}` |
| `sumFib` | Sum of first N Fibonacci | `{"arg": 10}` | `{"result": 143}` |
| `sumSquares` | Sum of squares 1..N | `{"arg": 5}` | `{"result": 55}` |
| `matrixTrace` | Trace of NxN matrix | `{"matrix": [[1,2],[3,4]]}` | `{"result": 5}` |

### MapReduce Chunked Execution

```bash
./openpool --chunked 4 --task task.json --bootstrap "/ip4/195.35.22.4/tcp/9000/p2p/Qm..."
```

### Send Task to Specific Peer

```bash
./openpool --send "QmPeerID..." --task task.json --connect "/ip4/195.35.22.4/tcp/9000/p2p/QmPeerID..."
```

---

## 8. P2P Networking

### Bootstrap from Known Peer

```bash
./openpool --port 9001 --bootstrap "/ip4/195.35.22.4/tcp/9000/p2p/QmBootstrap..."
```

### Multiple Bootstrap Peers

```bash
./openpool --bootstrap "/ip4/1.2.3.4/tcp/9000/p2p/QmPeer1,/ip4/5.6.7.8/tcp/9000/p2p/QmPeer2"
```

### DHT Discovery

```bash
./openpool --dht --discover --max-peers 10
```

### Peer Persistence

```bash
./openpool --peerstore /opt/openpool/data/peerstore.json
```

---

## 9. Marketplace & Bidding

### Enable Marketplace

```bash
./openpool --market --price 10 --http 8080
```

### Bidding Flow

1. **Publisher** creates a task listing with credit reward
2. **Workers** place bids with their price and ETA
3. **Auto-match** selects the lowest-price bid
4. **Worker** executes the task and submits result
5. **Credits** transfer from publisher to worker

---

## 10. GPU Computing

### Enable GPU

```bash
./openpool --gpu --http 8080
```

### Check GPU Status

```bash
curl http://localhost:8080/gpu
```

### Run GPU Task

```bash
curl -X POST http://localhost:8080/gpu/run \
  -H "Content-Type: application/json" \
  -d '{"op": "fft", "input": {"real": [1, 2, 3, 4], "imag": [0, 0, 0, 0], "inverse": false}}'
```

---

## 11. Task Verification & Slashing

### How Verification Works

Verification is **enabled by default** (`--verify true`). When a task completes:

1. The executor records an audit entry with input/output hashes
2. For high-value tasks, redundant verification is triggered
3. Multiple nodes execute the same task independently
4. Results are compared -- matching results are accepted
5. Mismatching results trigger slashing

### Slashing

Nodes that produce incorrect results lose credits:

```bash
curl "http://localhost:8080/slashing?node_id=abc123"
```

### Node Reliability Score

Scores range from 0.0 (always wrong) to 1.0 (always correct). New nodes start at 0.5.

---

## 12. Monitoring & Metrics

### Prometheus Metrics

```bash
curl http://localhost:8080/metrics
```

Available metrics:

| Metric | Type | Description |
|---|---|---|
| `openpool_uptime_seconds` | gauge | Time since node started |
| `openpool_tasks_completed_total` | counter | Total completed tasks |
| `openpool_tasks_failed_total` | counter | Total failed tasks |
| `openpool_task_duration_ms` | histogram | Task execution duration |
| `openpool_free_ram_mb` | gauge | Available RAM |
| `openpool_connected_peers` | gauge | Active P2P connections |
| `openpool_credits` | gauge | Current credit balance |
| `openpool_queue_size` | gauge | Pending tasks in queue |
| `openpool_workers` | gauge | Active worker count |
| `openpool_queue_running` | gauge | Whether queue is running |

### Prometheus Integration

```yaml
scrape_configs:
  - job_name: 'openpool'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
```

---

## 13. Deployment to VPS

### Quick Deploy

```bash
cd /opt/openpool
git pull
rm -f openpool
go build -o openpool ./cmd/node2
systemctl restart openpool
```

### Automated Deployment

```bash
bash deploy/deploy.sh
```

Handles: system updates, Go installation, directory setup, repo clone, build, systemd service, Nginx, SSL, startup, health check.

### Required Ports

| Port | Purpose |
|---|---|
| 22 | SSH |
| 80 | HTTP (SSL) |
| 443 | HTTPS |
| 8080 | HTTP API |
| 9000 | P2P |

### Resource Usage

| State | CPU | RAM |
|---|---|---|
| Idle | ~1% | ~50 MB |
| Task execution | 10-50% | ~100 MB |

---

## 14. Testing

```bash
make test           # Run all tests
make test-verbose   # With race detection
make test-coverage  # HTML coverage report
make check          # Format + lint + test
```

All 11 packages pass with 100+ tests total.

---

## 15. Architecture Overview

### Package Structure

```
cmd/node2/          # Main entry point
pkg/
  capabilities/     # Hardware detection
  executor/         # Task execution + verification
  gpu/              # GPU compute pool
  ledger/           # SQLite credit tracking
  marketplace/      # Task marketplace + bidding
  metrics/          # Prometheus metrics
  p2p/              # libp2p networking + DHT
  queue/            # Priority task queue + worker pool
  scheduler/        # MapReduce chunked distribution
  verification/     # Result verification + slashing
  wasm/             # WASM sandbox executor
ui/                 # Web dashboard
deploy/             # Deployment scripts
```

### Execution Flow

```
Client Request → HTTP API → Task Queue → Worker Pool → Executor → Result
                                                        ├── WASM Runtime
                                                        ├── GPU Pool
                                                        └── Verifier
```

### Credit System

- Each node starts with configurable credits (default: 100)
- Node ID persisted to disk so credits survive restarts
- Submitting deducts credits; completing awards credits
- Slashing deducts credits for incorrect results

---

## 16. Troubleshooting

### Node Won't Start
```bash
lsof -i :9000
pkill openpool
```

### Credits Showing as 0
Use `--node-id` to set a fixed ID, or check that `<ledger>.nodeid` exists.

### Git Pull Fails on VPS
```bash
cd /opt/openpool && rm openpool && git pull && go build -o openpool ./cmd/node2
```

### No Peers Discovered
Specify a bootstrap peer: `--bootstrap "/ip4/195.35.22.4/tcp/9000/p2p/Qm..."`

### GPU Not Detected
Requires CUDA/OpenCL drivers. Falls back to CPU automatically.

### WebUI Shows Errors
Clear browser cache. The dashboard is served statically.

### High Memory Usage
Check queue status: `curl http://localhost:8080/queue`

---

*For more information, visit [github.com/DP229/openpool](https://github.com/DP229/openpool).*
