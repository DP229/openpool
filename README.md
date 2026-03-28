# OpenPool

> **Democratizing compute — one node at a time.**

A global peer-to-peer network that distributes computational workloads across volunteered hardware, enabling anyone to contribute or consume processing power without relying on centralized cloud providers.

## The Problem

High-computation tasks — CFD, rendering, AI inference, scientific simulations — require expensive centralized cloud infrastructure. This concentrates power in the hands of a few providers and creates barriers for researchers, students, and developers in under-resourced regions.

## The Vision

A world where idle CPU/GPU cycles are shared freely. A researcher in rural India can run a CFD simulation on a cluster of gaming PCs in Berlin. A student in Nigeria can access distributed AI inference without an AWS bill. A retired workstation in Tokyo contributes to a medical imaging pipeline overnight.

## Core Principles

1. **Open** — Open source, open protocol, no vendor lock-in
2. **Democratic** — No single point of control or failure
3. **Fair** — Contributors earn proportional access; no mining-style waste
4. **Secure** — End-to-end encryption, verified computation, privacy-first
5. **Ephemeral** — Tasks are short-lived; hardware returns to its owner

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         OPENPOOL NETWORK                          │
│                                                                   │
│  Peer Registry (bootstrapped, DHT-based)                          │
│                                                                   │
│  ┌─────────┐   ┌─────────┐   ┌─────────┐   ┌─────────┐         │
│  │ Node A  │◄─►│ Node B  │◄─►│ Node C  │◄─►│ Node D  │  ...  │
│  │ (GPU,   │   │ (CPU,   │   │ (GPU,   │   │ (CPU,   │         │
│  │  Germany)│   │  India) │   │  USA)   │   │  Japan) │         │
│  └────┬────┘   └────┬────┘   └────┬────┘   └────┬────┘         │
│       │             │             │             │                  │
│       └─────────────┴─────────────┴─────────────┘                  │
│                    P2P Task Distribution                         │
│                                                                   │
│  ┌─────────┐   ┌─────────┐   ┌─────────┐                         │
│  │ Task 1  │   │ Task 2  │   │ Task 3  │   ← Work Requester    │
│  │ (CFD)   │   │ (Render)│   │ (AI)    │                      │
│  └─────────┘   └─────────┘   └─────────┘                         │
└─────────────────────────────────────────────────────────────────┘
```

## Design Goals

- [ ] **P2P Protocol** — libp2p or similar for NAT traversal, DHT peer discovery
- [ ] **Task Scheduler** — divide large workloads into chunks, reassemble results
- [ ] **Verification** — cryptographic proof of completed work (verifiable delay functions or redundant verification)
- [ ] **Incentive Layer** — fair credit system, not proof-of-waste
- [ ] **Sandboxing** — workloads run isolated (WASM, containers)
- [ ] **SDK** — Python/JS clients for submitting and consuming tasks
- [ ] **Web UI** — dashboard for node operators and task submitters

## Technology Stack

| Layer | Technology |
|---|---|
| P2P Networking | libp2p / Ethereum P2P |
| Task Scheduling | Custom / Airflow-lite |
| Work Verification | VDF / Redundant compute |
| Sandboxing | WebAssembly / Docker |
| SDK | Python, TypeScript |
| Dashboard | React |

## Comparison

| Platform | Model | Cost | Open |
|---|---|---|---|
| AWS/GCP/Azure | Centralized | Pay-per-use | ❌ |
| Render/Fly.io | Centralized + containers | Pay-per-use | ❌ |
| BOINC/SETI@home | Volunteer compute | Free | ✅ |
| Golem/IdleGamer | P2P + incentive | Token-based | ✅ |
| **OpenPool** | **P2P + verifiable** | **Free / credit-based** | **✅** |

## Status

🟡 **Early stage** — concept, architecture planning

## Contributing

This is an open project. Ideas, code, and feedback welcome.

## License

MIT
