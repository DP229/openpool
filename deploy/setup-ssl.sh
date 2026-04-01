#!/bin/bash
# Run this script after DNS propagation to generate SSL certificates

set -e

echo "========================================"
echo "OpenPool SSL Certificate Setup"
echo "========================================"

# Check DNS resolution
echo ""
echo "Checking DNS resolution..."

DOMAIN_IP=$(dig +short openpool.live | tail -1)
API_IP=$(dig +short api.openpool.live | tail -1)

echo "openpool.live resolves to: $DOMAIN_IP"
echo "api.openpool.live resolves to: $API_IP"

if [ "$DOMAIN_IP" != "195.35.22.4" ] || [ "$API_IP" != "195.35.22.4" ]; then
    echo ""
    echo "WARNING: DNS not pointing to VPS IP (195.35.22.4)"
    echo "Please wait for DNS propagation and try again."
    echo ""
    read -p "Continue anyway? (y/n): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Stop services temporarily
echo ""
echo "Stopping services for certificate generation..."
systemctl stop nginx
systemctl stop openpool

# Generate certificates
echo ""
echo "Generating Let's Encrypt certificates..."
certbot certonly --standalone \
    -d openpool.live \
    -d www.openpool.live \
    -d api.openpool.live \
    -d dashboard.openpool.live \
    --non-interactive \
    --agree-tos \
    --email admin@openpool.live \
    --no-eff-email

# Start services
echo ""
echo "Starting services..."
systemctl start openpool
systemctl restart nginx

# Verify
echo ""
echo "Testing HTTPS..."
sleep 5

if curl -sSf https://api.openpool.live/status > /dev/null 2>&1; then
    echo "SUCCESS: HTTPS working!"
    echo ""
    echo "Endpoints:"
    echo "  https://openpool.live/"
    echo "  https://api.openpool.live/status"
    echo "  https://api.openpool.live/nodes"
else
    echo "WARNING: HTTPS test failed. Check nginx logs:"
    echo "  journalctl -u nginx -n 20"
fi

echo ""
echo "SSL Setup Complete!"
echo "Certificate auto-renewal is enabled."