#!/usr/bin/env bash
set -e

# Test metrics collection and autoscaling
echo "=== Metrics Collection Test ==="

# Create test config
cat > /tmp/navarch-metrics-test.yaml <<EOF
server:
  address: ":50052"
  heartbeat_interval: 2s
  health_check_interval: 5s
  autoscale_interval: 5s

providers:
  fake:
    type: fake
    gpu_count: 8

pools:
  metrics-test:
    provider: fake
    instance_type: fake-8xgpu
    min_nodes: 2
    max_nodes: 10
    cooldown: 15s
    autoscaling:
      type: reactive
      scale_up_at: 80
      scale_down_at: 20
    labels:
      pool: metrics-test
EOF

echo "Starting control plane..."
go run ./cmd/control-plane --config /tmp/navarch-metrics-test.yaml &
CP_PID=$!

sleep 5
echo ""
echo "✅ Control plane started (PID $CP_PID)"
echo ""

# Let it run for 30 seconds to collect metrics
echo "Collecting metrics for 30 seconds..."
sleep 30

echo ""
echo "Checking pool status..."
go run ./cmd/navarch list

echo ""
echo "Shutting down..."
kill $CP_PID
wait $CP_PID 2>/dev/null || true

echo ""
echo "✅ Test complete"

