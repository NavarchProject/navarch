#!/bin/bash
# End-to-end test: Start control plane and node, verify communication
# Run this on a GPU instance

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NAVARCH_DIR="$(dirname "$SCRIPT_DIR")"

cd "$NAVARCH_DIR"

echo "============================================"
echo "Navarch End-to-End Test"
echo "============================================"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "=== Cleaning up ==="
    pkill -f "go run ./cmd/control-plane" 2>/dev/null || true
    pkill -f "go run ./cmd/node" 2>/dev/null || true
    echo "Cleanup complete"
}
trap cleanup EXIT

# Build binaries (faster than go run)
echo "=== Building binaries ==="
go build -o /tmp/control-plane ./cmd/control-plane
go build -o /tmp/node ./cmd/node
go build -o /tmp/navarch ./cmd/navarch
echo "Build complete"
echo ""

# Start control plane
echo "=== Starting control plane ==="
/tmp/control-plane > /tmp/cp.log 2>&1 &
CP_PID=$!
sleep 2

if ! kill -0 $CP_PID 2>/dev/null; then
    echo "ERROR: Control plane failed to start"
    cat /tmp/cp.log
    exit 1
fi
echo "Control plane started (PID: $CP_PID)"
echo ""

# Verify control plane is healthy
echo "=== Verifying control plane health ==="
if ! curl -s http://localhost:50051/healthz | grep -q "ok"; then
    echo "ERROR: Control plane health check failed"
    exit 1
fi
echo "Control plane is healthy"
echo ""

# Start node daemon
echo "=== Starting node daemon ==="
NODE_ID="test-$(hostname)-$$"
/tmp/node --node-id "$NODE_ID" --provider test --region us-test-1 > /tmp/node.log 2>&1 &
NODE_PID=$!
sleep 3

if ! kill -0 $NODE_PID 2>/dev/null; then
    echo "ERROR: Node daemon failed to start"
    cat /tmp/node.log
    exit 1
fi
echo "Node daemon started (PID: $NODE_PID, ID: $NODE_ID)"
echo ""

# Check node registration
echo "=== Checking node registration ==="
/tmp/navarch list
echo ""

# Get node details
echo "=== Getting node details ==="
/tmp/navarch get "$NODE_ID"
echo ""

# Wait for health checks
echo "=== Waiting for health checks (65 seconds) ==="
sleep 65

# Check health status
echo "=== Checking health status ==="
HEALTH=$(/tmp/navarch get "$NODE_ID" | grep "Health:" | awk '{print $2}')
echo "Health status: $HEALTH"

if [ "$HEALTH" != "Healthy" ]; then
    echo "WARNING: Node health is not Healthy (got: $HEALTH)"
    echo ""
    echo "Node logs:"
    cat /tmp/node.log
fi
echo ""

# Test cordon command
echo "=== Testing cordon command ==="
/tmp/navarch cordon "$NODE_ID"
sleep 12

# Check if command was received
echo "=== Checking command delivery ==="
if grep -q "received command.*CORDON" /tmp/node.log; then
    echo "PASS: Cordon command received by node"
else
    echo "FAIL: Cordon command not received"
    exit 1
fi
echo ""

# Final status
echo "=== Final node status ==="
/tmp/navarch get "$NODE_ID"
echo ""

echo "============================================"
echo "End-to-end test complete!"
echo "============================================"

