package db

import (
	"context"
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

// NodeRecord represents a registered node with its metadata and status.
type NodeRecord struct {
	NodeID       string
	Provider     string
	Region       string
	Zone         string
	InstanceType string
	GPUs         []*pb.GPUInfo
	Metadata     *pb.NodeMetadata
	
	// Runtime state
	Status              pb.NodeStatus
	LastHeartbeat       time.Time
	LastHealthCheck     time.Time
	HealthStatus        pb.HealthStatus
	RegisteredAt        time.Time
	
	// Configuration
	Config *pb.NodeConfig
}

// HealthCheckRecord represents a historical health check result.
type HealthCheckRecord struct {
	NodeID    string
	Timestamp time.Time
	Results   []*pb.HealthCheckResult
}

// CommandRecord represents a command issued to a node.
type CommandRecord struct {
	CommandID  string
	NodeID     string
	Type       pb.NodeCommandType
	Parameters map[string]string
	IssuedAt   time.Time
	Status     string // "pending", "acknowledged", "completed", "failed"
}

// DB is the interface for control plane data storage.
type DB interface {
	// Node operations
	RegisterNode(ctx context.Context, record *NodeRecord) error
	GetNode(ctx context.Context, nodeID string) (*NodeRecord, error)
	UpdateNodeStatus(ctx context.Context, nodeID string, status pb.NodeStatus) error
	UpdateNodeHeartbeat(ctx context.Context, nodeID string, timestamp time.Time) error
	ListNodes(ctx context.Context) ([]*NodeRecord, error)
	DeleteNode(ctx context.Context, nodeID string) error
	
	// Health check operations
	RecordHealthCheck(ctx context.Context, record *HealthCheckRecord) error
	GetLatestHealthCheck(ctx context.Context, nodeID string) (*HealthCheckRecord, error)
	
	// Command operations
	CreateCommand(ctx context.Context, record *CommandRecord) error
	GetPendingCommands(ctx context.Context, nodeID string) ([]*CommandRecord, error)
	UpdateCommandStatus(ctx context.Context, commandID, status string) error
	
	// Cleanup
	Close() error
}

