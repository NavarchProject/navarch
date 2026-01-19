package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

// InMemDB is an in-memory implementation of the DB interface.
// Suitable for testing and development.
type InMemDB struct {
	mu           sync.RWMutex
	nodes        map[string]*NodeRecord
	healthChecks map[string][]*HealthCheckRecord // nodeID -> list of health checks
	commands     map[string]*CommandRecord       // commandID -> command
	nodeCommands map[string][]*CommandRecord     // nodeID -> list of commands
}

// NewInMemDB creates a new in-memory database.
func NewInMemDB() *InMemDB {
	return &InMemDB{
		nodes:        make(map[string]*NodeRecord),
		healthChecks: make(map[string][]*HealthCheckRecord),
		commands:     make(map[string]*CommandRecord),
		nodeCommands: make(map[string][]*CommandRecord),
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
		record.RegisteredAt = time.Now()
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
	return node, nil
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
		nodes = append(nodes, node)
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
			pending = append(pending, cmd)
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

// Close closes the database (no-op for in-memory).
func (db *InMemDB) Close() error {
	return nil
}