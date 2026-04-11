#!/bin/bash
# OpenPool Deployment Script
# Run this script as root on your VPS
# Usage: bash deploy.sh
#
# This script deploys the integrated binary (cmd/integrated)
# with CGO_ENABLED=1 (required for go-sqlite3).
# No GPU support — go-nvml gracefully falls back to CPU mode.

set -e

echo "========================================"
echo "OpenPool Deployment Script"
echo "VPS IP: 195.35.22.4"
echo "Domain: openpool.live"
echo "========================================"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# ========================================
# PHASE 1: System Preparation
# ========================================
log_info "Phase 1: System Preparation"

log_info "Updating system packages..."
apt update -y
apt upgrade -y

log_info "Installing dependencies..."
apt install -y build-essential git curl wget sqlite3 ufw jq certbot nginx

log_info "Configuring firewall..."
ufw --force reset
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 8080/tcp
ufw allow 9000/tcp
ufw --force enable

log_info "Firewall status:"
ufw status

# ========================================
# PHASE 2: Install Go
# ========================================
log_info "Phase 2: Installing Go 1.25..."

export PATH=$PATH:/usr/local/go/bin

if command -v go &> /dev/null; then
    GO_VERSION=$(go version | awk '{print $3}')
    log_info "Go already installed: $GO_VERSION"
else
    cd /tmp
    wget -q https://go.dev/dl/go1.25.7.linux-amd64.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf go1.25.7.linux-amd64.tar.gz
    rm go1.25.7.linux-amd64.tar.gz

    if ! grep -q '/usr/local/go/bin' /root/.bashrc; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> /root/.bashrc
    fi
    export PATH=$PATH:/usr/local/go/bin

    log_info "Go installed: $(go version)"
fi

# ========================================
# PHASE 3: Create Directory Structure
# ========================================
log_info "Phase 3: Creating directory structure..."

mkdir -p /opt/openpool/{data,logs,wasm,ui}
mkdir -p /opt/openpool/data

# ========================================
# PHASE 4: Download OpenPool
# ========================================
log_info "Phase 4: Downloading OpenPool..."

cd /opt/openpool

if [ -d ".git" ]; then
    log_info "Repository already exists, pulling latest..."
    git pull
else
    log_info "Cloning repository..."
    git clone https://github.com/DP229/openpool.git .
fi

# ========================================
# PHASE 5: Build OpenPool
# ========================================
log_info "Phase 5: Building OpenPool..."

export PATH=$PATH:/usr/local/go/bin
export CGO_ENABLED=1
cd /opt/openpool

log_info "Downloading Go modules..."
go mod download

log_info "Building binary (CGO_ENABLED=1, cmd/integrated)..."
go build -o openpool ./cmd/integrated

log_info "Build complete: $(ls -lh openpool)"

# ========================================
# PHASE 6: Create Systemd Service
# ========================================
log_info "Phase 6: Creating systemd service..."

cat > /etc/systemd/system/openpool.service << 'EOF'
[Unit]
Description=OpenPool P2P Node
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/openpool
Environment="PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
Environment="CGO_ENABLED=1"
ExecStart=/opt/openpool/openpool \
  -http 8080 \
  -port 9000 \
  -db /opt/openpool/data/openpool.db \
  -auth-db /opt/openpool/data/openpool_auth.db \
  -peerstore /opt/openpool/data/peerstore.json \
  -wasm /opt/openpool/wasm \
  -dht
Restart=always
RestartSec=10
StandardOutput=append:/opt/openpool/logs/openpool.log
StandardError=append:/opt/openpool/logs/openpool.log

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable openpool

log_info "Systemd service created"

# ========================================
# PHASE 7: Configure Nginx
# ========================================
log_info "Phase 7: Configuring Nginx..."

cat > /etc/nginx/sites-available/openpool << 'EOF'
# API endpoint - HTTP redirect
server {
    listen 80;
    server_name api.openpool.live;
    return 301 https://$server_name$request_uri;
}

# API endpoint - HTTPS
server {
    listen 443 ssl http2;
    server_name api.openpool.live;
    
    ssl_certificate /etc/letsencrypt/live/openpool.live/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/openpool.live/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 86400;
    }
}

# Dashboard - HTTP redirect
server {
    listen 80;
    server_name openpool.live www.openpool.live dashboard.openpool.live;
    return 301 https://$server_name$request_uri;
}

# Dashboard - HTTPS
server {
    listen 443 ssl http2;
    server_name openpool.live www.openpool.live dashboard.openpool.live;
    
    ssl_certificate /etc/letsencrypt/live/openpool.live/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/openpool.live/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    
    root /opt/openpool/ui;
    index index.html;
    
    location / {
        try_files $uri $uri/ /index.html;
    }
    
    # Proxy API requests
    location /api/ {
        proxy_pass http://127.0.0.1:8080/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
EOF

# Remove default site and enable openpool
rm -f /etc/nginx/sites-enabled/default
ln -sf /etc/nginx/sites-available/openpool /etc/nginx/sites-enabled/

# Test nginx config (will fail without SSL certs, that's ok)
nginx -t 2>/dev/null || log_warn "Nginx config test failed (SSL certs not yet generated)"

# ========================================
# PHASE 8: Generate SSL Certificates
# ========================================
log_info "Phase 8: Generating SSL certificates..."

# Stop any service on port 80 temporarily
systemctl stop nginx 2>/dev/null || true
systemctl stop openpool 2>/dev/null || true

log_info "Requesting Let's Encrypt certificate..."
certbot certonly --standalone \
    -d openpool.live \
    -d www.openpool.live \
    -d api.openpool.live \
    -d dashboard.openpool.live \
    --non-interactive \
    --agree-tos \
    --email admin@openpool.live \
    --no-eff-email || log_warn "Certbot failed - DNS may not be propagated yet"

if [ -f /etc/letsencrypt/live/openpool.live/fullchain.pem ]; then
    log_info "SSL certificate generated successfully"
else
    log_warn "SSL certificate generation failed - run manually after DNS propagation"
fi

# ========================================
# PHASE 9: Start Services
# ========================================
log_info "Phase 9: Starting services..."

systemctl start openpool
sleep 3
systemctl restart nginx

log_info "OpenPool status:"
systemctl status openpool --no-pager || true

log_info "Nginx status:"
systemctl status nginx --no-pager || true

# ========================================
# PHASE 10: Verification
# ========================================
log_info "Phase 10: Verification..."

sleep 5

log_info "Testing local API..."
curl -s http://localhost:8080/status | jq . || log_warn "API not responding yet (may need a few seconds)"

log_info ""
log_info "========================================"
log_info "Deployment Complete!"
log_info "========================================"
log_info ""
log_info "Services:"
log_info "  - OpenPool: systemctl status openpool"
log_info "  - Nginx:    systemctl status nginx"
log_info ""
log_info "Logs:"
log_info "  - OpenPool: tail -f /opt/openpool/logs/openpool.log"
log_info "  - Journal:  journalctl -u openpool -f"
log_info ""
log_info "API Endpoints:"
log_info "  - Local:  http://localhost:8080/status"
log_info "  - Public:  http://195.35.22.4:8080/status"
log_info "  - Domain:  https://api.openpool.live/status (after DNS)"
log_info ""
log_info "Next Steps:"
log_info "  1. Configure DNS on Hostinger (see deploy/dns-records.txt)"
log_info "  2. Run: bash deploy/setup-ssl.sh (after DNS propagation)"
log_info "  3. Test:  curl https://api.openpool.live/status"
log_info ""
log_info "Health Check (add to crontab):"
log_info "  */5 * * * * /opt/openpool/deploy/healthcheck.sh"
log_info ""