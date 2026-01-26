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

// MetricsRecord represents metrics collected from a node at a point in time.
type MetricsRecord struct {
	NodeID    string
	Timestamp time.Time
	Metrics   *pb.NodeMetrics
}

// InstanceRecord represents a cloud instance tracked by the control plane.
// This is separate from NodeRecord to capture the full instance lifecycle,
// including cases where nodes fail to register or boot properly.
type InstanceRecord struct {
	// InstanceID is the provider-assigned instance ID (e.g., GCP instance name).
	InstanceID string

	// Provider is the cloud provider name (e.g., "gcp", "aws", "lambda").
	Provider string

	// Region is the cloud region where the instance is running.
	Region string

	// Zone is the availability zone within the region.
	Zone string

	// InstanceType is the instance type (e.g., "a3-highgpu-8g").
	InstanceType string

	// State is the current lifecycle state of the instance.
	State pb.InstanceState

	// PoolName is the name of the pool that provisioned this instance (empty if standalone).
	PoolName string

	// CreatedAt is when the instance provisioning was requested.
	CreatedAt time.Time

	// ReadyAt is when the node registered (instance became ready), if applicable.
	ReadyAt time.Time

	// TerminatedAt is when the instance was terminated, if applicable.
	TerminatedAt time.Time

	// NodeID is the ID of the registered node, if the node has registered.
	// Empty if the instance is still pending registration or failed.
	NodeID string

	// StatusMessage is a human-readable status message (e.g., error details).
	StatusMessage string

	// Labels are user-defined labels copied from the pool configuration.
	Labels map[string]string
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
	
	// Metrics operations
	RecordMetrics(ctx context.Context, record *MetricsRecord) error
	GetRecentMetrics(ctx context.Context, nodeID string, duration time.Duration) ([]*MetricsRecord, error)

	// Instance operations (cloud resource lifecycle tracking)
	CreateInstance(ctx context.Context, record *InstanceRecord) error
	GetInstance(ctx context.Context, instanceID string) (*InstanceRecord, error)
	UpdateInstanceState(ctx context.Context, instanceID string, state pb.InstanceState, message string) error
	UpdateInstanceNodeID(ctx context.Context, instanceID string, nodeID string) error
	ListInstances(ctx context.Context) ([]*InstanceRecord, error)
	ListInstancesByState(ctx context.Context, state pb.InstanceState) ([]*InstanceRecord, error)
	ListInstancesByPool(ctx context.Context, poolName string) ([]*InstanceRecord, error)
	DeleteInstance(ctx context.Context, instanceID string) error

	// Cleanup
	Close() error
}