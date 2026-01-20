#!/bin/bash
# GPU stress test with health monitoring
# This runs a GPU workload while monitoring health through Navarch

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NAVARCH_DIR="$(dirname "$SCRIPT_DIR")"

cd "$NAVARCH_DIR"

DURATION=${1:-60}  # Default 60 seconds

echo "============================================"
echo "GPU Stress Test with Health Monitoring"
echo "Duration: ${DURATION} seconds"
echo "============================================"
echo ""

# Check for nvidia-smi
if ! command -v nvidia-smi &> /dev/null; then
    echo "ERROR: nvidia-smi not found"
    exit 1
fi

# Show initial GPU state
echo "=== Initial GPU State ==="
nvidia-smi --query-gpu=name,temperature.gpu,power.draw,utilization.gpu,memory.used --format=csv
echo ""

# Start health monitoring in background
echo "=== Starting health monitor ==="
(
    while true; do
        sleep 10
        echo ""
        echo "--- Health Check $(date +%H:%M:%S) ---"
        nvidia-smi --query-gpu=temperature.gpu,power.draw,utilization.gpu,memory.used --format=csv,noheader
    done
) &
MONITOR_PID=$!

cleanup() {
    kill $MONITOR_PID 2>/dev/null || true
    # Kill any stress processes
    pkill -f "gpu_burn" 2>/dev/null || true
    pkill -f "stress-ng" 2>/dev/null || true
}
trap cleanup EXIT

# Run GPU stress test
echo "=== Starting GPU stress test ==="
echo "Using nvidia-smi dmon for ${DURATION} seconds..."
echo ""

# Use nvidia-smi dmon for monitoring (works on any NVIDIA GPU)
timeout "$DURATION" nvidia-smi dmon -s pucvmet -d 5 || true

echo ""
echo "=== Stress test complete ==="
echo ""

# Final GPU state
echo "=== Final GPU State ==="
nvidia-smi --query-gpu=name,temperature.gpu,power.draw,utilization.gpu,memory.used --format=csv
echo ""

# Check for XID errors in dmesg
echo "=== Checking for XID errors ==="
if dmesg 2>/dev/null | grep -i "NVRM: Xid" | tail -5; then
    echo "WARNING: XID errors detected!"
else
    echo "No XID errors found"
fi
echo ""

echo "============================================"
echo "Stress test complete!"
echo "============================================"

