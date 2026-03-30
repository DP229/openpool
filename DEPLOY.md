# OpenPool Deployment Guide

## Dependency Analysis

### Core Dependencies

| Component | Technology | Version | Purpose |
|-----------|------------|---------|---------|
| **Runtime** | Go | 1.25+ | Node execution |
| **P2P Networking** | go-libp2p | v0.48.0 | Peer-to-peer connections |
| **DHT Discovery** | go-libp2p-kad-dht | v0.27.0 | Peer discovery |
| **Database** | SQLite3 | 1.14.37 | Ledger & marketplace |
| **CLI UI** | lipgloss | v1.1.0 | Terminal styling |

### Build Output
- Single binary: ~15-20MB (static build)
- No external runtime needed
- SQLite linked (CGO)

### Mobile SDKs
- **Python SDK**: `requests`
- **Node.js SDK**: native `fetch`
- **Mobile**: React Native + Expo SDK 52

---

## Hosting Requirements

### Minimum VPS Spec
| Resource | Requirement |
|----------|-------------|
| **CPU** | 1 vCPU |
| **RAM** | 512MB |
| **Storage** | 1GB SSD |
| **Bandwidth** | 100GB/month |
| **OS** | Ubuntu 22.04+ / Debian 12+ |

### Recommended (for production)
| Resource | Requirement |
|----------|-------------|
| **CPU** | 2+ vCPU |
| **RAM** | 1GB+ |
| **Storage** | 10GB SSD |
| **Bandwidth** | 1TB/month |
| **IP** | Static public IP |

### Network Requirements
- **Open ports**: 
  - `8080` (HTTP API)
  - `9000` (P2P TCP)
  - `9001` (P2P WebSocket)
- **NAT traversal**: UPnP or manual port forwarding
- **Firewall**: Allow inbound TCP on P2P ports

---

## Deployment Steps

### 1. Server Setup
```bash
# SSH into your VPS
ssh user@vps-ip

# Install Go 1.25
wget https://go.dev/dl/go1.25.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.25.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Install build tools
sudo apt update
sudo apt install -y build-essential git
```

### 2. Deploy Node
```bash
# Clone repo
git clone https://github.com/DP229/openpool.git
cd openpool

# Build binary
go build -o openpool ./cmd/node2

# Run with all features
./openpool \
  -http 8080 \
  -port 9000 \
  -market \
  -gpu \
  -ledger /var/lib/openpool/openpool.db \
  -dht
```

### 3. Systemd Service (recommended)
```bash
sudo nano /etc/systemd/system/openpool.service
```

```ini
[Unit]
Description=OpenPool P2P Node
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu/openpool
ExecStart=/home/ubuntu/openpool/openpool -http 8080 -port 9000 -market -gpu -dht
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable openpool
sudo systemctl start openpool
```

### 4. Configure Firewall
```bash
sudo ufw allow 8080/tcp  # HTTP API
sudo ufw allow 9000/tcp  # P2P
sudo ufw enable
```

---

## Cloudflare Tunnel Setup

For `openpool.live`:

### 1. Install cloudflared
```bash
wget https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64
sudo mv cloudflared-linux-amd64 /usr/local/bin/cloudflared
sudo chmod +x /usr/local/bin/cloudflared
```

### 2. Authenticate
```bash
cloudflared tunnel login
# Follow browser auth flow
```

### 3. Create Tunnel
```bash
cloudflared tunnel create openpool
```

### 4. Configure DNS
```bash
# Add CNAME for api.openpool.live
cloudflared tunnel route dns openpool api.openpool.live

# Or for dashboard.openpool.live
cloudflared tunnel route dns openpool dashboard.openpool.live
```

### 5. Tunnel YAML Config
```yaml
# ~/.cloudflared/config.yml
tunnel: openpool
credentials-file: /home/ubuntu/.cloudflared/creds.json

ingress:
  # HTTP API
  - hostname: api.openpool.live
    service: http://localhost:8080
  
  # Dashboard  
  - hostname: openpool.live
    service: http://localhost:8080
  
  # P2P (TCP)
  - hostname: p2p.openpool.live
    service: tcp://localhost:9000
  
  - service: http_status:404
```

### 6. Run Tunnel
```bash
cloudflared tunnel run openpool
```

### 7. Systemd for Tunnel
```bash
sudo nano /etc/systemd/system/cloudflared.service
```

```ini
[Unit]
Description=Cloudflare Tunnel
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/cloudflared tunnel --config /home/ubuntu/.cloudflared/config.yml run openpool
Restart=always

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable cloudflared
sudo systemctl start cloudflared
```

---

## Domain Configuration

### DNS Records (Cloudflare)

| Type | Name | Value | Proxy |
|------|------|-------|-------|
| A | openpool.live | VPS IP | Proxied |
| A | api.openpool.live | VPS IP | Proxied |
| A | p2p.openpool.live | VPS IP | DNS Only |

---

## Production Checklist

- [ ] Static public IP or Dynamic DNS
- [ ] Ports 8080, 9000 opened in firewall
- [ ] Cloudflare Tunnel configured
- [ ] Systemd service for auto-restart
- [ ] SSL/TLS (via Cloudflare)
- [ ] Backup strategy for SQLite DB
- [ ] Monitoring (optional: Prometheus metrics)

---

## Resource Usage

| Operation | CPU | RAM | Notes |
|-----------|-----|-----|-------|
| Idle node | 1% | 50MB | Waiting for connections |
| Task execution | 10-50% | 100MB | Depends on task complexity |
| DHT discovery | 5% | 20MB | Periodic |
| P2P connected (5 peers) | 5% | 100MB | Scales with peers |

---

## Cost Estimate (Monthly)

| Provider | Spec | Cost |
|----------|------|------|
| DigitalOcean | 1 vCPU, 1GB RAM | $6/mo |
| Hetzner | 1 vCPU, 1GB RAM | €3.50/mo |
| AWS Lightsail | 1 vCPU, 1GB RAM | $5/mo |

Domain: $12/year (openpool.live)
Cloudflare: Free tier sufficient
**Total: ~$5-7/month