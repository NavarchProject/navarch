package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/NavarchProject/navarch/pkg/clock"
	pb "github.com/NavarchProject/navarch/proto"
	"google.golang.org/protobuf/proto"
)

// InMemDB is an in-memory implementation of the DB interface.
// Suitable for testing and development.
type InMemDB struct {
	mu           sync.RWMutex
	clock        clock.Clock
	nodes        map[string]*NodeRecord
	healthChecks map[string][]*HealthCheckRecord // nodeID -> list of health checks
	commands     map[string]*CommandRecord       // commandID -> command
	nodeCommands map[string][]*CommandRecord     // nodeID -> list of commands
	metrics      map[string][]*MetricsRecord     // nodeID -> list of metrics (max 100 per node)
	instances    map[string]*InstanceRecord      // instanceID -> instance record
}

// NewInMemDB creates a new in-memory database.
func NewInMemDB() *InMemDB {
	return NewInMemDBWithClock(clock.Real())
}

// NewInMemDBWithClock creates a new in-memory database with a custom clock.
func NewInMemDBWithClock(clk clock.Clock) *InMemDB {
	if clk == nil {
		clk = clock.Real()
	}
	return &InMemDB{
		clock:        clk,
		nodes:        make(map[string]*NodeRecord),
		healthChecks: make(map[string][]*HealthCheckRecord),
		commands:     make(map[string]*CommandRecord),
		nodeCommands: make(map[string][]*CommandRecord),
		metrics:      make(map[string][]*MetricsRecord),
		instances:    make(map[string]*InstanceRecord),
	}
}

// RegisterNode registers a new node or updates an existing one.
func (db *InMemDB) RegisterNode(ctx context.Context, record *NodeRecord) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if existing, ok := db.nodes[record.NodeID]; ok {
		record.RegisteredAt = existing.RegisteredAt
		record.LastHeartbeat = existing.LastHeartbeat
		record.LastHealthCheck = existing.LastHealthCheck
	} else {
		record.RegisteredAt = db.clock.Now()
	}
	if record.Status == pb.NodeStatus_NODE_STATUS_UNKNOWN {
		record.Status = pb.NodeStatus_NODE_STATUS_ACTIVE
	}
	db.nodes[record.NodeID] = record
	return nil
}

// GetNode retrieves a node by ID.
func (db *InMemDB) GetNode(ctx context.Context, nodeID string) (*NodeRecord, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	node, ok := db.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}
	return db.copyNodeRecord(node), nil
}

// UpdateNodeStatus updates the status of a node.
func (db *InMemDB) UpdateNodeStatus(ctx context.Context, nodeID string, status pb.NodeStatus) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	node, ok := db.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	node.Status = status
	return nil
}

// UpdateNodeHeartbeat updates the last heartbeat time for a node.
func (db *InMemDB) UpdateNodeHeartbeat(ctx context.Context, nodeID string, timestamp time.Time) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	node, ok := db.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	node.LastHeartbeat = timestamp
	return nil
}

// ListNodes returns all registered nodes.
func (db *InMemDB) ListNodes(ctx context.Context) ([]*NodeRecord, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	
	nodes := make([]*NodeRecord, 0, len(db.nodes))
	for _, node := range db.nodes {
		nodes = append(nodes, db.copyNodeRecord(node))
	}
	
	return nodes, nil
}

// DeleteNode removes a node from the database.
func (db *InMemDB) DeleteNode(ctx context.Context, nodeID string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.nodes, nodeID)
	delete(db.healthChecks, nodeID)
	delete(db.nodeCommands, nodeID)
	return nil
}

// RecordHealthCheck stores a health check result.
func (db *InMemDB) RecordHealthCheck(ctx context.Context, record *HealthCheckRecord) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.healthChecks[record.NodeID] = append(db.healthChecks[record.NodeID], record)

	if node, ok := db.nodes[record.NodeID]; ok {
		wasUnhealthy := node.Status == pb.NodeStatus_NODE_STATUS_UNHEALTHY
		node.LastHealthCheck = record.Timestamp
		overallStatus := pb.HealthStatus_HEALTH_STATUS_HEALTHY
		for _, result := range record.Results {
			if result.Status == pb.HealthStatus_HEALTH_STATUS_UNHEALTHY {
				overallStatus = pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
				break
			}
			if result.Status == pb.HealthStatus_HEALTH_STATUS_DEGRADED &&
				overallStatus == pb.HealthStatus_HEALTH_STATUS_HEALTHY {
				overallStatus = pb.HealthStatus_HEALTH_STATUS_DEGRADED
			}
		}
		node.HealthStatus = overallStatus
		if overallStatus == pb.HealthStatus_HEALTH_STATUS_UNHEALTHY {
			node.Status = pb.NodeStatus_NODE_STATUS_UNHEALTHY
		} else if wasUnhealthy && overallStatus == pb.HealthStatus_HEALTH_STATUS_HEALTHY {
			node.Status = pb.NodeStatus_NODE_STATUS_ACTIVE
		}
	}

	return nil
}

// GetLatestHealthCheck retrieves the most recent health check for a node.
func (db *InMemDB) GetLatestHealthCheck(ctx context.Context, nodeID string) (*HealthCheckRecord, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	checks, ok := db.healthChecks[nodeID]
	if !ok || len(checks) == 0 {
		return nil, fmt.Errorf("no health checks found for node: %s", nodeID)
	}
	return checks[len(checks)-1], nil
}

// CreateCommand creates a new command for a node.
func (db *InMemDB) CreateCommand(ctx context.Context, record *CommandRecord) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	
	if record.Status == "" {
		record.Status = "pending"
	}
	
	db.commands[record.CommandID] = record
	db.nodeCommands[record.NodeID] = append(db.nodeCommands[record.NodeID], record)
	
	return nil
}

// GetPendingCommands retrieves all pending commands for a node.
func (db *InMemDB) GetPendingCommands(ctx context.Context, nodeID string) ([]*CommandRecord, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	commands := db.nodeCommands[nodeID]
	pending := make([]*CommandRecord, 0)

	for _, cmd := range commands {
		if cmd.Status == "pending" {
			pending = append(pending, db.copyCommandRecord(cmd))
		}
	}

	return pending, nil
}

// UpdateCommandStatus updates the status of a command.
func (db *InMemDB) UpdateCommandStatus(ctx context.Context, commandID, status string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	
	cmd, ok := db.commands[commandID]
	if !ok {
		return fmt.Errorf("command not found: %s", commandID)
	}
	
	cmd.Status = status
	return nil
}

// copyNodeRecord creates a deep copy of a NodeRecord to prevent data races.
// Pass-by-value won't work here because NodeRecord contains pointer fields
// (Metadata, Config, GPUs) that would share underlying protobuf data.
func (db *InMemDB) copyNodeRecord(src *NodeRecord) *NodeRecord {
	if src == nil {
		return nil
	}

	dst := &NodeRecord{
		NodeID:          src.NodeID,
		Provider:        src.Provider,
		Region:          src.Region,
		Zone:            src.Zone,
		InstanceType:    src.InstanceType,
		Status:          src.Status,
		LastHeartbeat:   src.LastHeartbeat,
		LastHealthCheck: src.LastHealthCheck,
		HealthStatus:    src.HealthStatus,
		RegisteredAt:    src.RegisteredAt,
	}

	if src.Metadata != nil {
		dst.Metadata = proto.Clone(src.Metadata).(*pb.NodeMetadata)
	}

	if src.Config != nil {
		dst.Config = proto.Clone(src.Config).(*pb.NodeConfig)
	}

	if len(src.GPUs) > 0 {
		dst.GPUs = make([]*pb.GPUInfo, len(src.GPUs))
		for i, gpu := range src.GPUs {
			if gpu != nil {
				dst.GPUs[i] = proto.Clone(gpu).(*pb.GPUInfo)
			}
		}
	}

	return dst
}

// copyCommandRecord creates a deep copy of a CommandRecord to prevent data races.
// Pass-by-value won't work here because CommandRecord.Parameters is a map, which
// is a reference type. A value copy would share the underlying map data.
func (db *InMemDB) copyCommandRecord(src *CommandRecord) *CommandRecord {
	if src == nil {
		return nil
	}

	dst := &CommandRecord{
		CommandID: src.CommandID,
		NodeID:    src.NodeID,
		Type:      src.Type,
		IssuedAt:  src.IssuedAt,
		Status:    src.Status,
	}

	if src.Parameters != nil {
		dst.Parameters = make(map[string]string, len(src.Parameters))
		for k, v := range src.Parameters {
			dst.Parameters[k] = v
		}
	}

	return dst
}

// copyMetricsRecord creates a deep copy of a MetricsRecord to prevent data races.
// Pass-by-value won't work here because MetricsRecord.Metrics is a pointer to a
// protobuf message (*pb.NodeMetrics). A value copy would still share the underlying
// protobuf data, leading to data races if the original is modified while the
// caller is reading the copy. We use proto.Clone for proper deep copying.
func (db *InMemDB) copyMetricsRecord(src *MetricsRecord) *MetricsRecord {
	if src == nil {
		return nil
	}

	dst := &MetricsRecord{
		NodeID:    src.NodeID,
		Timestamp: src.Timestamp,
	}

	if src.Metrics != nil {
		dst.Metrics = proto.Clone(src.Metrics).(*pb.NodeMetrics)
	}

	return dst
}

// RecordMetrics stores metrics from a node heartbeat.
func (db *InMemDB) RecordMetrics(ctx context.Context, record *MetricsRecord) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.nodes[record.NodeID]; !exists {
		return fmt.Errorf("node %s not found", record.NodeID)
	}

	history := db.metrics[record.NodeID]
	history = append(history, record)

	// Keep only the most recent 100 metrics per node
	if len(history) > 100 {
		history = history[len(history)-100:]
	}

	db.metrics[record.NodeID] = history
	return nil
}

// GetRecentMetrics retrieves metrics for a node within the specified duration.
func (db *InMemDB) GetRecentMetrics(ctx context.Context, nodeID string, duration time.Duration) ([]*MetricsRecord, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if _, exists := db.nodes[nodeID]; !exists {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}

	cutoff := db.clock.Now().Add(-duration)
	history := db.metrics[nodeID]

	var recent []*MetricsRecord
	for _, record := range history {
		if record.Timestamp.After(cutoff) {
			recent = append(recent, db.copyMetricsRecord(record))
		}
	}

	return recent, nil
}

// CreateInstance creates a new instance record.
func (db *InMemDB) CreateInstance(ctx context.Context, record *InstanceRecord) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if record.InstanceID == "" {
		return fmt.Errorf("instance_id is required")
	}

	if _, exists := db.instances[record.InstanceID]; exists {
		return fmt.Errorf("instance already exists: %s", record.InstanceID)
	}

	db.instances[record.InstanceID] = db.copyInstanceRecord(record)
	return nil
}

// GetInstance retrieves an instance by ID.
func (db *InMemDB) GetInstance(ctx context.Context, instanceID string) (*InstanceRecord, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	instance, ok := db.instances[instanceID]
	if !ok {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}
	return db.copyInstanceRecord(instance), nil
}

// UpdateInstanceState updates the state and status message of an instance.
func (db *InMemDB) UpdateInstanceState(ctx context.Context, instanceID string, state pb.InstanceState, message string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	instance, ok := db.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	instance.State = state
	instance.StatusMessage = message

	// Set timestamps based on state transitions
	now := db.clock.Now()
	switch state {
	case pb.InstanceState_INSTANCE_STATE_RUNNING:
		if instance.ReadyAt.IsZero() {
			instance.ReadyAt = now
		}
	case pb.InstanceState_INSTANCE_STATE_TERMINATED:
		if instance.TerminatedAt.IsZero() {
			instance.TerminatedAt = now
		}
	}

	return nil
}

// UpdateInstanceNodeID links an instance to a registered node.
func (db *InMemDB) UpdateInstanceNodeID(ctx context.Context, instanceID string, nodeID string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	instance, ok := db.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	instance.NodeID = nodeID
	return nil
}

// ListInstances returns all instance records.
func (db *InMemDB) ListInstances(ctx context.Context) ([]*InstanceRecord, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	instances := make([]*InstanceRecord, 0, len(db.instances))
	for _, instance := range db.instances {
		instances = append(instances, db.copyInstanceRecord(instance))
	}
	return instances, nil
}

// ListInstancesByState returns all instances in a specific state.
func (db *InMemDB) ListInstancesByState(ctx context.Context, state pb.InstanceState) ([]*InstanceRecord, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var instances []*InstanceRecord
	for _, instance := range db.instances {
		if instance.State == state {
			instances = append(instances, db.copyInstanceRecord(instance))
		}
	}
	return instances, nil
}

// ListInstancesByPool returns all instances belonging to a specific pool.
func (db *InMemDB) ListInstancesByPool(ctx context.Context, poolName string) ([]*InstanceRecord, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var instances []*InstanceRecord
	for _, instance := range db.instances {
		if instance.PoolName == poolName {
			instances = append(instances, db.copyInstanceRecord(instance))
		}
	}
	return instances, nil
}

// DeleteInstance removes an instance record.
func (db *InMemDB) DeleteInstance(ctx context.Context, instanceID string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	delete(db.instances, instanceID)
	return nil
}

// copyInstanceRecord creates a deep copy of an InstanceRecord to prevent data races.
func (db *InMemDB) copyInstanceRecord(src *InstanceRecord) *InstanceRecord {
	if src == nil {
		return nil
	}

	dst := &InstanceRecord{
		InstanceID:    src.InstanceID,
		Provider:      src.Provider,
		Region:        src.Region,
		Zone:          src.Zone,
		InstanceType:  src.InstanceType,
		State:         src.State,
		PoolName:      src.PoolName,
		CreatedAt:     src.CreatedAt,
		ReadyAt:       src.ReadyAt,
		TerminatedAt:  src.TerminatedAt,
		NodeID:        src.NodeID,
		StatusMessage: src.StatusMessage,
	}

	if src.Labels != nil {
		dst.Labels = make(map[string]string, len(src.Labels))
		for k, v := range src.Labels {
			dst.Labels[k] = v
		}
	}

	return dst
}

// Close closes the database (no-op for in-memory).
func (db *InMemDB) Close() error {
	return nil
}