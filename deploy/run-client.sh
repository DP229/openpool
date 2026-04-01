#!/bin/bash
# Run on client devices to connect to OpenPool bootstrap node

BOOTSTRAP_IP="195.35.22.4"
BOOTSTRAP_PORT="9000"

echo "========================================"
echo "OpenPool Client Setup"
echo "========================================"

# Get bootstrap peer ID
echo ""
echo "Getting bootstrap node info..."
BOOTSTRAP_INFO=$(curl -s http://$BOOTSTRAP_IP:8080/status)

if [ -z "$BOOTSTRAP_INFO" ]; then
    echo "ERROR: Cannot connect to bootstrap node"
    echo "Make sure the VPS is running: http://$BOOTSTRAP_IP:8080"
    exit 1
fi

PEER_INFO=$(echo $BOOTSTRAP_INFO | jq -r '.peer_info')
echo "Bootstrap node: $PEER_INFO"

echo ""
echo "Starting client node..."
echo ""

# Run as client
./openpool \
    -http 8081 \
    -port 9001 \
    -connect "$PEER_INFO" \
    -dht \
    -market \
    -wasm ./wasm/sandbox.wasm