#!/bin/bash
# Health check script for OpenPool
# Add to crontab: */5 * * * * /opt/openpool/scripts/healthcheck.sh

LOG_FILE="/opt/openpool/logs/health.log"
API_URL="http://localhost:8080/status"

# Check API response
RESPONSE=$(curl -s --max-time 10 "$API_URL")

if [ -z "$RESPONSE" ]; then
    echo "$(date '+%Y-%m-%d %H:%M:%S') - ERROR: API not responding" >> "$LOG_FILE"
    
    # Try to restart
    systemctl restart openpool
    
    echo "$(date '+%Y-%m-%d %H:%M:%S') - ACTION: Restarted openpool service" >> "$LOG_FILE"
else
    NODE_ID=$(echo "$RESPONSE" | jq -r '.node_id // "unknown"')
    PEERS=$(echo "$RESPONSE" | jq -r '.connected_peers // 0')
    
    echo "$(date '+%Y-%m-%d %H:%M:%S') - OK: node=$NODE_ID peers=$PEERS" >> "$LOG_FILE"
fi