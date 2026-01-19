# Critical User Journeys (CUJs)

This document defines the critical user journeys for Navarch. These journeys serve as the foundation for integration tests and validate the system's core functionality.

## CUJ-1: Node Registration and Configuration

**Actors:** GPU Node, Control Plane  
**Goal:** New node registers with control plane and receives configuration

### Happy Path
1. Node starts up and connects to control plane
2. Node sends RegisterNode request with:
   - Node ID (instance ID)
   - Provider (gcp, aws, etc.)
   - Region and zone
   - Instance type
   - GPU information
   - System metadata (hostname, IPs)
3. Control plane validates request
4. Control plane stores node in database with ACTIVE status
5. Control plane returns success with configuration:
   - Health check interval
   - Heartbeat interval
   - Enabled health checks
6. Node applies configuration

### Failure Cases
- Missing required fields (node_id) → rejected with error message
- Duplicate registration → updates existing node record

### Expected State
- Node record exists in database
- Node status is ACTIVE
- Node has received configuration

---

## CUJ-2: Heartbeat and Liveness Monitoring

**Actors:** GPU Node, Control Plane  
**Goal:** Node maintains connection with control plane to prove liveness

### Happy Path
1. Node sends periodic heartbeat with:
   - Node ID
   - Timestamp
   - Optional metrics (CPU, memory, GPU utilization)
2. Control plane receives heartbeat
3. Control plane updates last_heartbeat timestamp
4. Control plane responds with acknowledgment

### Failure Cases
- Node not registered → heartbeat fails
- Missed heartbeats → (future: node marked as unhealthy)

### Expected State
- Last heartbeat timestamp is updated
- Node remains ACTIVE

---

## CUJ-3: Health Check Reporting and Status Updates

**Actors:** GPU Node, Control Plane  
**Goal:** Node runs health checks and reports results; control plane updates node status

### Happy Path - Healthy Node
1. Node runs configured health checks (boot, nvml, xid)
2. All checks pass
3. Node sends ReportHealth request with results
4. Control plane records health check
5. Control plane responds with ACTIVE status

### Failure Path - Unhealthy Node
1. Node runs health checks
2. One or more checks fail (e.g., GPU XID error detected)
3. Node sends ReportHealth request with UNHEALTHY results
4. Control plane records health check
5. Control plane updates node status to UNHEALTHY
6. Control plane responds with UNHEALTHY status

### Degraded Node
1. Health check returns DEGRADED (warnings but functional)
2. Control plane updates health status to DEGRADED
3. Node status remains ACTIVE but health_status is DEGRADED

### Expected State
- Health check results are stored
- Node status reflects health (ACTIVE → UNHEALTHY if checks fail)
- Health status is updated (HEALTHY, DEGRADED, or UNHEALTHY)

---

## CUJ-4: Command Issuance and Execution

**Actors:** Control Plane, GPU Node, Operator  
**Goal:** Control plane issues commands to nodes (cordon, drain, terminate)

### Happy Path - Cordon Node
1. Operator or system decides node should be cordoned
2. Control plane creates CORDON command for node
3. Node polls for commands via GetNodeCommands
4. Control plane returns pending CORDON command
5. Command is marked as "acknowledged"
6. Node executes cordon (stops accepting new workloads)
7. (Future: Node confirms completion)

### Other Command Types
- **DRAIN**: Node gracefully drains existing workloads
- **RUN_DIAGNOSTIC**: Node runs specific diagnostic test
- **TERMINATE**: Node prepares for shutdown

### Expected State
- Command is created with "pending" status
- Node retrieves command
- Command status changes to "acknowledged"
- (Future: Command completes and status → "completed")

---

## CUJ-5: Full Node Lifecycle

**Actors:** GPU Node, Control Plane, Workload Scheduler  
**Goal:** Complete lifecycle from registration to termination

### Full Journey
1. **Registration**: Node registers (CUJ-1) → status: ACTIVE
2. **Normal Operation**: 
   - Heartbeats every 30s (CUJ-2)
   - Health checks every 60s (CUJ-3)
   - Status remains ACTIVE
3. **Health Degradation**:
   - Health check fails → status: UNHEALTHY
   - Control plane issues CORDON command
4. **Cordoning**:
   - Node receives CORDON command (CUJ-4)
   - Node stops accepting new workloads
   - Status changes to CORDONED
5. **Draining**:
   - Control plane issues DRAIN command
   - Node drains existing workloads
   - Status changes to DRAINING
6. **Termination**:
   - Control plane issues TERMINATE command
   - Node prepares for shutdown
   - Status changes to TERMINATED
   - Node is removed from active pool

### Expected State Transitions
```
UNKNOWN → ACTIVE → UNHEALTHY → CORDONED → DRAINING → TERMINATED
         ↑         ↓
         └─ ACTIVE (if health recovers)
```

---

## CUJ-6: Multi-Node Fleet Management

**Actors:** Multiple GPU Nodes, Control Plane  
**Goal:** Control plane manages multiple nodes simultaneously

### Happy Path
1. Multiple nodes register concurrently
2. Each node maintains independent heartbeat
3. Each node reports health independently
4. Control plane tracks all nodes correctly
5. Commands can be issued to individual nodes
6. Control plane can list all nodes with their status

### Expected State
- All nodes are tracked independently
- No interference between nodes
- ListNodes returns all registered nodes
- Each node has unique state

---

## Test Coverage Requirements

Each CUJ should have:
- ✅ Happy path test
- ✅ Error case tests
- ✅ State verification
- ✅ Concurrent operation tests (where applicable)

## Performance Requirements

- Node registration: < 100ms
- Heartbeat processing: < 10ms
- Health check processing: < 50ms
- Command retrieval: < 20ms
- Support 1000+ nodes per control plane instance

