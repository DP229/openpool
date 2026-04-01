# OpenPool Deployment Guide

## Quick Start

### 1. Deploy to VPS
```bash
# SSH into your VPS
ssh root@195.35.22.4

# Download and run deployment script
curl -fsSL https://raw.githubusercontent.com/DP229/openpool/main/deploy/deploy.sh | bash
```

### 2. Configure DNS (Hostinger)
See `dns-records.txt` for DNS configuration.

### 3. Setup SSL (after DNS propagation)
```bash
bash /opt/openpool/deploy/setup-ssl.sh
```

### 4. Test API
```bash
curl http://195.35.22.4:8080/status
# or after DNS:
curl https://api.openpool.live/status
```

---

## Files

| File | Purpose |
|------|---------|
| `deploy.sh` | Main deployment script (run on VPS) |
| `setup-ssl.sh` | Generate SSL certificates |
| `dns-records.txt` | DNS configuration for Hostinger |
| `test-api.sh` | API endpoint tests |
| `run-client.sh` | Client node startup script |
| `healthcheck.sh` | Monitoring script |

---

## Services

```bash
# View OpenPool status
systemctl status openpool

# View logs
tail -f /opt/openpool/logs/openpool.log

# Restart service
systemctl restart openpool

# View nginx status
systemctl status nginx

# View nginx logs
tail -f /var/log/nginx/error.log
```

---

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/status` | GET | Node status |
| `/ledger` | GET | Credit balances |
| `/discover` | GET | DHT peer discovery |
| `/run` | POST | Run task locally |
| `/submit` | POST | Submit task via P2P |
| `/nodes` | GET | Marketplace nodes |
| `/tasks` | GET/POST | Task listings |
| `/bids` | GET/POST | Task bids |
| `/gpu` | GET | GPU status |

---

## Multi-Node Testing

### Bootstrap Node (VPS)
Running at: `195.35.22.4:9000`

### Client Node 1
```bash
./openpool -http 8081 -port 9001 \
  -connect "/ip4/195.35.22.4/tcp/9000/p2p/<PEER_ID>" \
  -dht -market
```

### Client Node 2
```bash
./openpool -http 8082 -port 9002 \
  -connect "/ip4/195.35.22.4/tcp/9000/p2p/<PEER_ID>" \
  -dht -market
```

---

## Troubleshooting

### API Not Responding
```bash
systemctl restart openpool
journalctl -u openpool -n 50
```

### DNS Not Propagating
```bash
dig openpool.live +short
# Should return: 195.35.22.4
```

### SSL Certificate Failed
```bash
# Check if port 80 is open
curl -I http://openpool.live

# Manually generate cert
certbot certonly --standalone -d openpool.live -d api.openpool.live
```

### Firewall Issues
```bash
ufw status
ufw allow 8080/tcp
ufw allow 9000/tcp
```

---

## Cron Jobs

```bash
# Add health check (every 5 minutes)
crontab -e

*/5 * * * * /opt/openpool/deploy/healthcheck.sh
```

---

## Cost

- VPS: $5-6/month
- Domain: $12/year (already owned)
- SSL: Free (Let's Encrypt)
- DNS: Free (Hostinger)
- Total: ~$6/month