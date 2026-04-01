#!/bin/bash
# Quick test script for OpenPool API

API_URL="http://195.35.22.4:8080"
# After DNS + SSL: API_URL="https://api.openpool.live"

echo "========================================"
echo "OpenPool API Tests"
echo "Target: $API_URL"
echo "========================================"
echo ""

# Test 1: Status
echo "Test 1: GET /status"
curl -s "$API_URL/status" | jq .
echo ""
echo "---"

# Test 2: Ledger
echo "Test 2: GET /ledger"
curl -s "$API_URL/ledger" | jq .
echo ""
echo "---"

# Test 3: Peers
echo "Test 3: GET /discover"
curl -s "$API_URL/discover" | jq .
echo ""
echo "---"

# Test 4: Local execution
echo "Test 4: POST /run - Fibonacci(30)"
curl -s -X POST "$API_URL/run" \
    -H "Content-Type: application/json" \
    -d '{"op":"fib","arg":30}' | jq .
echo ""
echo "---"

# Test 5: GPU status
echo "Test 5: GET /gpu"
curl -s "$API_URL/gpu" | jq .
echo ""
echo "---"

# Test 6: Marketplace nodes
echo "Test 6: GET /nodes"
curl -s "$API_URL/nodes" | jq .
echo ""
echo "---"

# Test 7: Submit task
echo "Test 7: POST /submit - Fibonacci(20)"
curl -s -X POST "$API_URL/submit" \
    -H "Content-Type: application/json" \
    -d '{"op":"fib","arg":20,"credits":10,"timeout_sec":30}' | jq .
echo ""

echo "========================================"
echo "Tests Complete!"
echo "========================================"