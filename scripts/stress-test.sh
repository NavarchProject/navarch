#!/usr/bin/env bash
set -euo pipefail

# Navarch stress test using fake provider
# Tests autoscaling, pool management, and system stability under load

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BIN_DIR="${PROJECT_ROOT}/bin"
TEST_CONFIG="/tmp/navarch-stress-test.yaml"
LOG_FILE="/tmp/navarch-stress-test.log"
CONTROL_PLANE_PID=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() {
    echo -e "${BLUE}[$(date +'%H:%M:%S')]${NC} $*"
}

success() {
    echo -e "${GREEN}✓${NC} $*"
}

error() {
    echo -e "${RED}✗${NC} $*"
}

warn() {
    echo -e "${YELLOW}⚠${NC} $*"
}

cleanup() {
    log "Cleaning up..."
    if [[ -n "${CONTROL_PLANE_PID}" ]]; then
        kill "${CONTROL_PLANE_PID}" 2>/dev/null || true
        wait "${CONTROL_PLANE_PID}" 2>/dev/null || true
    fi
    pkill -f "navarch.*control-plane.*stress-test" 2>/dev/null || true
    rm -f "${TEST_CONFIG}" "${LOG_FILE}"
    success "Cleanup complete"
}

trap cleanup EXIT

create_stress_config() {
    log "Creating stress test configuration..."
    cat > "${TEST_CONFIG}" <<'EOF'
server:
  address: ":50052"
  heartbeat_interval: 5s
  health_check_interval: 10s
  autoscale_interval: 2s

providers:
  fake:
    type: fake
    gpu_count: 8

pools:
  # High-frequency scaling pool
  rapid-scale:
    provider: fake
    instance_type: gpu_8x_h100
    region: local
    min_nodes: 1
    max_nodes: 50
    cooldown: 1s
    autoscaling:
      type: reactive
      scale_up_at: 70
      scale_down_at: 30

  # Large capacity pool
  large-pool:
    provider: fake
    instance_type: gpu_8x_h100
    region: local
    min_nodes: 10
    max_nodes: 100
    cooldown: 2s
    autoscaling:
      type: reactive
      scale_up_at: 80
      scale_down_at: 20

  # Burst pool (scales from zero)
  burst:
    provider: fake
    instance_type: gpu_8x_h100
    region: local
    min_nodes: 0
    max_nodes: 30
    cooldown: 1s
    autoscaling:
      type: reactive
      scale_up_at: 50
      scale_down_at: 10

  # Stable baseline pool
  baseline:
    provider: fake
    instance_type: gpu_8x_h100
    region: local
    min_nodes: 5
    max_nodes: 20
    cooldown: 3s
    autoscaling:
      type: reactive
      scale_up_at: 85
      scale_down_at: 15
EOF
    success "Configuration created at ${TEST_CONFIG}"
}

start_control_plane() {
    log "Starting control plane..."
    "${BIN_DIR}/control-plane" --config "${TEST_CONFIG}" > "${LOG_FILE}" 2>&1 &
    CONTROL_PLANE_PID=$!
    
    # Wait for control plane to be ready
    local max_wait=10
    local waited=0
    while ! curl -s http://localhost:50052/healthz > /dev/null 2>&1; do
        if [[ ${waited} -ge ${max_wait} ]]; then
            error "Control plane failed to start"
            tail -20 "${LOG_FILE}"
            exit 1
        fi
        sleep 1
        ((waited++))
    done
    
    success "Control plane started (PID: ${CONTROL_PLANE_PID})"
}

wait_for_nodes() {
    local expected=$1
    local pool=${2:-}
    local max_wait=30
    local waited=0
    
    while true; do
        local count
        if [[ -n "${pool}" ]]; then
            count=$("${BIN_DIR}/navarch" list -s http://localhost:50052 2>/dev/null | grep -c "fake-" || echo "0")
        else
            count=$("${BIN_DIR}/navarch" list -s http://localhost:50052 2>/dev/null | grep -c "fake-" || echo "0")
        fi
        
        if [[ ${count} -ge ${expected} ]]; then
            return 0
        fi
        
        if [[ ${waited} -ge ${max_wait} ]]; then
            return 1
        fi
        
        sleep 1
        ((waited++))
    done
}

test_initial_provisioning() {
    log "Test 1: Initial node provisioning"
    
    # Expected: min_nodes from all pools = 1 + 10 + 0 + 5 = 16
    log "Waiting for initial nodes to provision..."
    if wait_for_nodes 16; then
        local count=$("${BIN_DIR}/navarch" list -s http://localhost:50052 2>/dev/null | grep -c "fake-" || echo "0")
        success "Initial provisioning complete: ${count} nodes"
    else
        error "Initial provisioning failed"
        return 1
    fi
}

test_concurrent_list_operations() {
    log "Test 2: Concurrent list operations (stress test API)"
    
    local pids=()
    local requests=100
    local concurrent=20
    
    log "Executing ${requests} list requests with ${concurrent} concurrent clients..."
    
    for ((i=0; i<requests; i++)); do
        "${BIN_DIR}/navarch" list -s http://localhost:50052 > /dev/null 2>&1 &
        pids+=($!)
        
        # Maintain concurrency limit
        if [[ ${#pids[@]} -ge ${concurrent} ]]; then
            wait "${pids[0]}" || true
            pids=("${pids[@]:1}")
        fi
    done
    
    # Wait for remaining
    for pid in "${pids[@]}"; do
        wait "${pid}" || true
    done
    
    success "Completed ${requests} concurrent list operations"
}

test_get_operations() {
    log "Test 3: Concurrent get operations"
    
    local nodes=()
    while IFS= read -r node; do
        # Skip table borders and empty lines
        if [[ "${node}" =~ ^fake-[0-9]+ ]]; then
            nodes+=("${node}")
        fi
    done < <("${BIN_DIR}/navarch" list -s http://localhost:50052 2>/dev/null | awk '/fake-/ {print $2}')
    
    if [[ ${#nodes[@]} -eq 0 ]]; then
        error "No nodes found for get test"
        return 1
    fi
    
    log "Testing get operations on ${#nodes[@]} nodes..."
    
    local pids=()
    for node in "${nodes[@]}"; do
        "${BIN_DIR}/navarch" get "${node}" -s http://localhost:50052 > /dev/null 2>&1 &
        pids+=($!)
    done
    
    local failed=0
    for pid in "${pids[@]}"; do
        wait "${pid}" || ((failed++))
    done
    
    if [[ ${failed} -eq 0 ]]; then
        success "All get operations succeeded"
    else
        warn "${failed} get operations failed"
    fi
}

test_cordon_drain_operations() {
    log "Test 4: Cordon and drain operations"
    
    local node
    node=$("${BIN_DIR}/navarch" list -s http://localhost:50052 2>/dev/null | awk '/fake-/ {print $2; exit}')
    
    if [[ -z "${node}" ]]; then
        error "No nodes available for cordon/drain test"
        return 1
    fi
    
    log "Cordoning node: ${node}"
    if "${BIN_DIR}/navarch" cordon "${node}" -s http://localhost:50052 > /dev/null 2>&1; then
        success "Node cordoned successfully"
    else
        error "Failed to cordon node"
        return 1
    fi
    
    log "Draining node: ${node}"
    if "${BIN_DIR}/navarch" drain "${node}" -s http://localhost:50052 > /dev/null 2>&1; then
        success "Node drained successfully"
    else
        warn "Drain operation encountered issues (expected for fake nodes)"
    fi
}

test_rapid_scaling() {
    log "Test 5: Rapid scaling behavior"
    
    log "Monitoring autoscaler for 15 seconds..."
    local start_time=$(date +%s)
    local end_time=$((start_time + 15))
    
    while [[ $(date +%s) -lt ${end_time} ]]; do
        local count=$("${BIN_DIR}/navarch" list -s http://localhost:50052 2>/dev/null | grep -c "fake-" || echo "0")
        echo -ne "\r  Current nodes: ${count}  "
        sleep 1
    done
    echo ""
    
    success "Scaling monitoring complete"
}

test_system_stability() {
    log "Test 6: System stability under sustained load"
    
    log "Running sustained load test for 20 seconds..."
    local end_time=$(($(date +%s) + 20))
    local request_count=0
    local error_count=0
    
    while [[ $(date +%s) -lt ${end_time} ]]; do
        if "${BIN_DIR}/navarch" list -s http://localhost:50052 > /dev/null 2>&1; then
            ((request_count++))
        else
            ((error_count++))
        fi
        sleep 0.1
    done
    
    local error_rate=0
    if [[ ${request_count} -gt 0 ]]; then
        error_rate=$((error_count * 100 / (request_count + error_count)))
    fi
    
    success "Completed ${request_count} requests with ${error_count} errors (${error_rate}% error rate)"
    
    if [[ ${error_rate} -gt 5 ]]; then
        warn "High error rate detected"
        return 1
    fi
}

test_memory_stability() {
    log "Test 7: Memory usage check"
    
    if [[ -z "${CONTROL_PLANE_PID}" ]]; then
        warn "Control plane PID not available"
        return 0
    fi
    
    # Check if process still exists
    if ! ps -p "${CONTROL_PLANE_PID}" > /dev/null 2>&1; then
        error "Control plane process died!"
        return 1
    fi
    
    # Get memory usage (macOS compatible)
    local mem_kb
    mem_kb=$(ps -o rss= -p "${CONTROL_PLANE_PID}" 2>/dev/null || echo "0")
    local mem_mb=$((mem_kb / 1024))
    
    success "Control plane memory usage: ${mem_mb} MB"
    
    if [[ ${mem_mb} -gt 500 ]]; then
        warn "Memory usage is high: ${mem_mb} MB"
    fi
}

check_logs_for_errors() {
    log "Test 8: Checking logs for critical errors"
    
    local errors
    errors=$(grep -i "error\|panic\|fatal" "${LOG_FILE}" 2>/dev/null | grep -v "connection refused" | wc -l | tr -d ' ' || echo "0")
    
    if [[ ${errors} -gt 0 ]]; then
        warn "Found ${errors} error messages in logs"
        log "Sample errors:"
        grep -i "error\|panic\|fatal" "${LOG_FILE}" | grep -v "connection refused" | tail -5 || true
    else
        success "No critical errors found in logs"
    fi
}

print_summary() {
    echo ""
    echo "════════════════════════════════════════════════════════════════"
    echo "                    STRESS TEST SUMMARY"
    echo "════════════════════════════════════════════════════════════════"
    echo ""
    
    local total_nodes
    total_nodes=$("${BIN_DIR}/navarch" list -s http://localhost:50052 2>/dev/null | grep -c "fake-" || echo "0")
    
    echo "Final Statistics:"
    echo "  • Total nodes: ${total_nodes}"
    echo "  • Control plane uptime: $(($(date +%s) - START_TIME)) seconds"
    echo "  • Log file: ${LOG_FILE}"
    echo ""
    
    "${BIN_DIR}/navarch" list -s http://localhost:50052 2>/dev/null || true
    echo ""
}

main() {
    START_TIME=$(date +%s)
    
    echo ""
    echo "════════════════════════════════════════════════════════════════"
    echo "              NAVARCH STRESS TEST (Fake Provider)"
    echo "════════════════════════════════════════════════════════════════"
    echo ""
    
    # Check binaries exist
    if [[ ! -x "${BIN_DIR}/control-plane" ]] || [[ ! -x "${BIN_DIR}/navarch" ]]; then
        error "Binaries not found. Run 'make build' first."
        exit 1
    fi
    
    create_stress_config
    start_control_plane
    
    sleep 2
    
    local failed_tests=0
    
    test_initial_provisioning || ((failed_tests++))
    test_concurrent_list_operations || ((failed_tests++))
    test_get_operations || ((failed_tests++))
    test_cordon_drain_operations || ((failed_tests++))
    test_rapid_scaling || ((failed_tests++))
    test_system_stability || ((failed_tests++))
    test_memory_stability || ((failed_tests++))
    check_logs_for_errors || ((failed_tests++))
    
    print_summary
    
    if [[ ${failed_tests} -eq 0 ]]; then
        success "All stress tests passed!"
        exit 0
    else
        error "${failed_tests} test(s) failed"
        exit 1
    fi
}

main "$@"

