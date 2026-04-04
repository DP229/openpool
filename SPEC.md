# OpenPool: Client Requirements & Benchmarks

## Minimum Client Requirements

OpenPool is designed for maximum reach — from a Raspberry Pi to a data center. The minimum bar should be **zero-cost entry**: if you have a computer and internet, you can contribute.

---

### Node Tiers

#### 🌱 Light Client (Minimum Viable)
| Requirement | Minimum | Notes |
|---|---|---|
| **CPU** | 1 core | ARM (Raspberry Pi) or x86 |
| **RAM** | 512 MB available | Above OS baseline |
| **Storage** | 500 MB free | For task data + WASM runtime |
| **Network** | 1 Mbps up/down | 128 KB/s sustained |
| **Uptime** | Intermittent OK | Contribute when idle |
| **OS** | Linux, macOS, Windows, Android | Browser (WASM) also works |
| **Run as** | CLI binary, background daemon | No root/admin required |

**What they can run:**
- WASM-compiled workloads
- Small Python scripts (< 5 min runtime)
- Lightweight data processing
- Networked tasks (small bandwidth)

**What they CANNOT run:**
- GPU tasks
- Large Docker containers
- Long-running jobs (> 10 min unless always-on)

---

#### 🖥️ Standard Client
| Requirement | Recommended | Notes |
|---|---|---|
| **CPU** | 4+ cores | Can run multiple subtasks in parallel |
| **RAM** | 4 GB available | Above OS + other apps |
| **Storage** | 5 GB free | Task data + Docker images |
| **Network** | 5 Mbps up/down | For large data transfer |
| **Uptime** | 4+ hrs/day avg | More uptime = more credits |
| **OS** | Linux (primary), macOS, Windows | Linux recommended |
| **Run as** | CLI daemon or Docker container | Docker optional but recommended |

**What they can run:**
- All light client tasks
- Docker containerized workloads
- Medium-length Python/Rust/C++ tasks
- Multi-chunk MapReduce jobs
- GPU tasks (if GPU available)

**What they CANNOT run:**
- GPU tasks (without GPU)
- Very large tasks (> 1GB input per subtask)

---

#### 🚀 Heavy / GPU Client
| Requirement | Minimum | Recommended |
|---|---|---|
| **CPU** | 8+ cores | For orchestration |
| **RAM** | 16 GB | For GPU + workload |
| **GPU** | 4 GB VRAM | GTX 1050 Ti minimum |
| **Storage** | 50 GB free | For large workloads/data |
| **Network** | 25 Mbps up/down | For data transfer |
| **Uptime** | Always-on preferred | Maximum credit earning |
| **Run as** | Docker + systemd service | Production-grade |

**What they can run:**
- All standard client tasks
- GPU rendering
- Large CFD meshes
- Batch processing jobs

---

## Hardware Capability Discovery

On join, each node advertises capabilities:

```json
{
  "node_id": "QmX...abc",
  "hardware": {
    "cpu_cores": 8,
    "cpu_arch": "x86_64",
    "ram_total_gb": 32,
    "ram_available_gb": 16,
    "gpu": {
      "present": true,
      "model": "NVIDIA RTX 4090",
      "vram_gb": 24,
      "cuda_version": "12.1"
    },
    "storage_free_gb": 100
  },
  "network": {
    "public_ip": "x.x.x.x",   // NAT-punched or relayed
    "bandwidth_up_mbps": 50,
    "bandwidth_down_mbps": 200,
    "country": "IN",
    "city": "Bangalore"
  },
  "software": {
    "docker_available": true,
    "cuda_available": true,
    "wasmtime_version": "20.0"
  },
  "availability": {
    "uptime_score": 0.95,   // 30-day rolling average
    "reliability_score": 0.98,
    "verified_tasks": 142,
    "failed_tasks": 3
  }
}
```

---

## Benchmarks & Performance Targets

### Node Performance Benchmarks

#### CPU Benchmark (Whetstone / Dhrystone equivalent)
```
Task: Pi to 1000 decimals
Target: < 10 seconds on standard client
Target: < 3 seconds on heavy client
Fails if: > 30 seconds (unreliable or too slow)
```

#### Memory Bandwidth Benchmark
```
Task: Matrix multiplication 4096x4096 float64
Target: > 20 GB/s on standard client
Fails if: < 5 GB/s (resource contention)
```

#### Network Benchmark
```
Task: Download + upload 100MB test file from peer
Target: Achieved bandwidth matches advertised
Fails if: < 50% of advertised bandwidth sustained
```

#### GPU Benchmark (if present)
```
Task: Run TinyLlama-1.1B inference (384 tokens)
Target: < 30 seconds on RTX 3060 or equivalent
Target: < 60 seconds on integrated GPU
Fails if: > 120 seconds (hardware issue or driver problem)
```

---

### Network-Level Benchmarks

| Metric | Target | Measurement |
|---|---|---|
| **Task distribution latency** | < 500ms (same continent) | P50 of task dispatch to node receipt |
| **Result retrieval latency** | < 1s after completion | P50 from result ready to requester received |
| **Network P2P round-trip** | < 100ms (local), < 300ms (global) | libp2p ping |
| **Peer discovery time** | < 5 seconds | New node joins → gets peer list |
| **Task assignment success rate** | > 95% | Tasks successfully dispatched |
| **Result verification rate** | > 98% | Results passing verification |

---

### Task-Level Limits

These are **hard limits** enforced by the protocol:

| Parameter | Limit | Reason |
|---|---|---|
| **Max input size per subtask** | 100 MB | Prevent memory exhaustion |
| **Max output size per subtask** | 500 MB | Prevent storage griefing |
| **Max execution time per subtask** | 30 minutes | Standard client |
| **Max execution time (heavy)** | 4 hours | GPU/always-on nodes |
| **Max concurrent subtasks** | `available_ram_gb / 2` | Prevent resource exhaustion |
| **Max WASM memory** | 1 GB | WASM linear memory limit |
| **Max Docker image size** | 2 GB | Storage and pull time |
| **Max network bandwidth per task** | 100 MB/s | Fair use |
| **Min peer reputation to receive tasks** | 0.3 / 1.0 | Prevent sybil/new node flooding |
| **Task timeout (no response)** | 2x expected runtime | Grace period for slow nodes |

---

### Reliability Targets

| Metric | Target | How Measured |
|---|---|---|
| **Task completion rate** | > 98% | Completed / dispatched |
| **False result rate** | < 1% | Caught by verification |
| **Node churn tolerance** | 50% simultaneous offline | Network stays alive |
| **Credit accuracy** | 100% | No double-spending |
| **Uptime SLA (relay nodes)** | 99.9% | Always-on bootstrap |

---

## Bandwidth & Storage Budget (Per Node)

### Per Task
| Resource | Light | Standard | Heavy |
|---|---|---|---|
| Download (input) | 10 MB | 100 MB | 1 GB |
| Upload (output) | 50 MB | 500 MB | 5 GB |
| Storage used | 100 MB | 1 GB | 10 GB |
| RAM peak | 256 MB | 4 GB | 32 GB |
| CPU time | 5 min | 30 min | 4 hrs |

### Always-On Node Monthly Budget
| Resource | Estimated Monthly |
|---|---|
| Bandwidth upload | ~50 GB |
| Bandwidth download | ~20 GB |
| Storage wear | ~10 GB writes |
| Electricity cost | ~$2-5 / month |

*At idle rates (0.5 kW, $0.10/kWh)*

---

## Benchmark Test Suite

Each node runs a lightweight benchmark on first boot and weekly:

```bash
openpool benchmark
```

This runs:
1. **CPU test** — 5-second compute benchmark → TFLOPS estimate
2. **Memory test** — 1 GB read/write → GB/s bandwidth
3. **Disk test** — Sequential read/write → MB/s
4. **Network test** — Ping to 5 bootstrap nodes → latency + bandwidth estimate
5. **GPU test** — (if present) CUDA device query + inference benchmark

Results are published to the DHT and used for task matching.

---

## Task Size Classification

| Class | Input Size | Runtime | Nodes Needed |
|---|---|---|---|
| **Micro** | < 1 KB | < 10 sec | 1 |
| **Small** | 1 KB - 10 MB | 10 sec - 5 min | 1-4 |
| **Medium** | 10 MB - 100 MB | 5 min - 30 min | 4-16 |
| **Large** | 100 MB - 1 GB | 30 min - 4 hrs | 16-64 |
| **X-Large** | > 1 GB | > 4 hrs | 64+ (requires heavy nodes) |

---

## Summary: Minimum to Run

**The absolute minimum to join OpenPool:**

```bash
# Any machine with:
# - 512 MB RAM
# - 500 MB storage  
# - 1 Mbps internet
# - Python 3.10+ or prebuilt binary

# Download binary (no install required):
curl -L https://get.openpool.dev | sh

# Start contributing in 30 seconds:
openpool join --email jack@example.com
openpool daemon  # runs in background

# Done. You're now part of the network.
# Earn credits automatically. Spend them on your workloads.
```

No Docker required. No GPU required. No cryptocurrency. No root access.

**The protocol adapts to whatever hardware you have.**
