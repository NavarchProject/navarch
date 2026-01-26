// Package controlplane provides the control plane server implementation.
package controlplane

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

// InstanceManager manages the lifecycle of cloud instances from provisioning
// through termination. It tracks instances separately from nodes to capture
// cases where provisioning fails or nodes never register.
//
// Key responsibilities:
//   - Create instance records when provisioning starts
//   - Update instance state when nodes register
//   - Detect stale instances (provisioned but never registered)
//   - Clean up terminated instance records
type InstanceManager struct {
	db     db.DB
	logger *slog.Logger
	config InstanceManagerConfig

	mu         sync.Mutex
	started    bool
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	onStale    func(instance *db.InstanceRecord) // callback for stale instance detection
	onFailed   func(instance *db.InstanceRecord) // callback for failed instance detection
}

// InstanceManagerConfig configures the instance manager behavior.
type InstanceManagerConfig struct {
	// RegistrationTimeout is how long to wait for a node to register after
	// provisioning before marking the instance as failed. Default: 10 minutes.
	RegistrationTimeout time.Duration

	// StaleCheckInterval is how often to check for stale instances.
	// Default: 1 minute.
	StaleCheckInterval time.Duration

	// RetainTerminatedDuration is how long to keep terminated instance records
	// before deleting them. Default: 24 hours.
	RetainTerminatedDuration time.Duration
}

// DefaultInstanceManagerConfig returns sensible defaults for the instance manager.
func DefaultInstanceManagerConfig() InstanceManagerConfig {
	return InstanceManagerConfig{
		RegistrationTimeout:      10 * time.Minute,
		StaleCheckInterval:       1 * time.Minute,
		RetainTerminatedDuration: 24 * time.Hour,
	}
}

// NewInstanceManager creates a new instance manager.
func NewInstanceManager(database db.DB, config InstanceManagerConfig, logger *slog.Logger) *InstanceManager {
	if logger == nil {
		logger = slog.Default()
	}
	if config.RegistrationTimeout == 0 {
		config.RegistrationTimeout = DefaultInstanceManagerConfig().RegistrationTimeout
	}
	if config.StaleCheckInterval == 0 {
		config.StaleCheckInterval = DefaultInstanceManagerConfig().StaleCheckInterval
	}
	if config.RetainTerminatedDuration == 0 {
		config.RetainTerminatedDuration = DefaultInstanceManagerConfig().RetainTerminatedDuration
	}

	return &InstanceManager{
		db:     database,
		logger: logger,
		config: config,
	}
}

// Start begins the background stale instance detection loop.
// Calling Start multiple times has no effect - subsequent calls are ignored.
func (t *InstanceManager) Start(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.started {
		t.logger.Debug("instance manager already started, ignoring Start() call")
		return
	}

	loopCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.started = true

	t.wg.Add(1)
	go t.staleCheckLoop(loopCtx)

	t.logger.Info("instance manager started",
		slog.Duration("registration_timeout", t.config.RegistrationTimeout),
		slog.Duration("stale_check_interval", t.config.StaleCheckInterval),
	)
}

// Stop halts the background stale instance detection loop.
// After Stop returns, Start can be called again to restart the manager.
func (t *InstanceManager) Stop() {
	t.mu.Lock()
	if !t.started {
		t.mu.Unlock()
		return
	}
	if t.cancel != nil {
		t.cancel()
	}
	t.started = false
	t.mu.Unlock()

	t.wg.Wait()
	t.logger.Info("instance manager stopped")
}

// OnStaleInstance sets a callback that is called when a stale instance is detected.
// A stale instance is one that was provisioned but the node never registered.
func (t *InstanceManager) OnStaleInstance(callback func(instance *db.InstanceRecord)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onStale = callback
}

// OnFailedInstance sets a callback that is called when an instance is marked as failed.
func (t *InstanceManager) OnFailedInstance(callback func(instance *db.InstanceRecord)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onFailed = callback
}

// TrackProvisioning creates an instance record when provisioning starts.
// Call this before calling provider.Provision().
func (t *InstanceManager) TrackProvisioning(ctx context.Context, instanceID, provider, region, zone, instanceType, poolName string, labels map[string]string) error {
	record := &db.InstanceRecord{
		InstanceID:   instanceID,
		Provider:     provider,
		Region:       region,
		Zone:         zone,
		InstanceType: instanceType,
		State:        pb.InstanceState_INSTANCE_STATE_PROVISIONING,
		PoolName:     poolName,
		CreatedAt:    time.Now(),
		Labels:       labels,
	}

	if err := t.db.CreateInstance(ctx, record); err != nil {
		return err
	}

	t.logger.Info("tracking instance provisioning",
		slog.String("instance_id", instanceID),
		slog.String("provider", provider),
		slog.String("pool", poolName),
	)
	return nil
}

// TrackProvisioningComplete updates an instance to pending_registration state.
// Call this after provider.Provision() returns successfully.
func (t *InstanceManager) TrackProvisioningComplete(ctx context.Context, instanceID string) error {
	if err := t.db.UpdateInstanceState(ctx, instanceID, pb.InstanceState_INSTANCE_STATE_PENDING_REGISTRATION, "waiting for node to register"); err != nil {
		return err
	}

	t.logger.Debug("instance provisioning complete, waiting for registration",
		slog.String("instance_id", instanceID),
	)
	return nil
}

// TrackProvisioningFailed marks an instance as failed when provisioning fails.
// Call this if provider.Provision() returns an error.
func (t *InstanceManager) TrackProvisioningFailed(ctx context.Context, instanceID string, reason string) error {
	if err := t.db.UpdateInstanceState(ctx, instanceID, pb.InstanceState_INSTANCE_STATE_FAILED, reason); err != nil {
		return err
	}

	t.logger.Warn("instance provisioning failed",
		slog.String("instance_id", instanceID),
		slog.String("reason", reason),
	)

	// Trigger callback if set
	t.mu.Lock()
	callback := t.onFailed
	t.mu.Unlock()

	if callback != nil {
		instance, err := t.db.GetInstance(ctx, instanceID)
		if err == nil {
			callback(instance)
		}
	}

	return nil
}

// TrackNodeRegistered updates an instance when its node registers successfully.
// Call this from the RegisterNode RPC handler.
func (t *InstanceManager) TrackNodeRegistered(ctx context.Context, instanceID, nodeID string) error {
	// First, update the node ID
	if err := t.db.UpdateInstanceNodeID(ctx, instanceID, nodeID); err != nil {
		// Instance might not exist if it was provisioned outside of the control plane
		t.logger.Debug("instance not found for node registration (may be external)",
			slog.String("instance_id", instanceID),
			slog.String("node_id", nodeID),
		)
		return nil // Not an error - node may have been provisioned externally
	}

	// Then update the state to running
	if err := t.db.UpdateInstanceState(ctx, instanceID, pb.InstanceState_INSTANCE_STATE_RUNNING, "node registered successfully"); err != nil {
		return err
	}

	t.logger.Info("instance ready, node registered",
		slog.String("instance_id", instanceID),
		slog.String("node_id", nodeID),
	)
	return nil
}

// TrackTerminating marks an instance as terminating.
// Call this before calling provider.Terminate().
func (t *InstanceManager) TrackTerminating(ctx context.Context, instanceID string) error {
	if err := t.db.UpdateInstanceState(ctx, instanceID, pb.InstanceState_INSTANCE_STATE_TERMINATING, "termination in progress"); err != nil {
		return err
	}

	t.logger.Debug("instance terminating",
		slog.String("instance_id", instanceID),
	)
	return nil
}

// TrackTerminated marks an instance as terminated.
// Call this after provider.Terminate() returns successfully.
func (t *InstanceManager) TrackTerminated(ctx context.Context, instanceID string) error {
	if err := t.db.UpdateInstanceState(ctx, instanceID, pb.InstanceState_INSTANCE_STATE_TERMINATED, "instance terminated"); err != nil {
		return err
	}

	t.logger.Info("instance terminated",
		slog.String("instance_id", instanceID),
	)
	return nil
}

// GetInstance returns the current state of an instance.
func (t *InstanceManager) GetInstance(ctx context.Context, instanceID string) (*db.InstanceRecord, error) {
	return t.db.GetInstance(ctx, instanceID)
}

// ListInstances returns all tracked instances.
func (t *InstanceManager) ListInstances(ctx context.Context) ([]*db.InstanceRecord, error) {
	return t.db.ListInstances(ctx)
}

// ListPendingRegistration returns instances waiting for node registration.
func (t *InstanceManager) ListPendingRegistration(ctx context.Context) ([]*db.InstanceRecord, error) {
	return t.db.ListInstancesByState(ctx, pb.InstanceState_INSTANCE_STATE_PENDING_REGISTRATION)
}

// ListFailed returns instances that failed provisioning or registration.
func (t *InstanceManager) ListFailed(ctx context.Context) ([]*db.InstanceRecord, error) {
	return t.db.ListInstancesByState(ctx, pb.InstanceState_INSTANCE_STATE_FAILED)
}

// staleCheckLoop periodically checks for stale and expired instances.
func (t *InstanceManager) staleCheckLoop(ctx context.Context) {
	defer t.wg.Done()

	ticker := time.NewTicker(t.config.StaleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.checkStaleInstances(ctx)
			t.cleanupTerminatedInstances(ctx)
		}
	}
}

// checkStaleInstances finds instances that are stuck in pending_registration
// and marks them as failed.
func (t *InstanceManager) checkStaleInstances(ctx context.Context) {
	pending, err := t.db.ListInstancesByState(ctx, pb.InstanceState_INSTANCE_STATE_PENDING_REGISTRATION)
	if err != nil {
		t.logger.Error("failed to list pending instances",
			slog.String("error", err.Error()),
		)
		return
	}

	now := time.Now()
	for _, instance := range pending {
		age := now.Sub(instance.CreatedAt)
		if age > t.config.RegistrationTimeout {
			t.logger.Warn("instance registration timeout exceeded",
				slog.String("instance_id", instance.InstanceID),
				slog.Duration("age", age),
				slog.Duration("timeout", t.config.RegistrationTimeout),
			)

			// Mark as failed
			if err := t.db.UpdateInstanceState(ctx, instance.InstanceID, pb.InstanceState_INSTANCE_STATE_FAILED, "registration timeout exceeded"); err != nil {
				t.logger.Error("failed to mark instance as failed",
					slog.String("instance_id", instance.InstanceID),
					slog.String("error", err.Error()),
				)
				continue
			}

			// Trigger callbacks
			t.mu.Lock()
			staleCallback := t.onStale
			failedCallback := t.onFailed
			t.mu.Unlock()

			// Get updated instance for callbacks
			updatedInstance, _ := t.db.GetInstance(ctx, instance.InstanceID)
			if updatedInstance != nil {
				if staleCallback != nil {
					staleCallback(updatedInstance)
				}
				if failedCallback != nil {
					failedCallback(updatedInstance)
				}
			}
		}
	}
}

// cleanupTerminatedInstances removes old terminated instance records.
func (t *InstanceManager) cleanupTerminatedInstances(ctx context.Context) {
	terminated, err := t.db.ListInstancesByState(ctx, pb.InstanceState_INSTANCE_STATE_TERMINATED)
	if err != nil {
		t.logger.Error("failed to list terminated instances",
			slog.String("error", err.Error()),
		)
		return
	}

	now := time.Now()
	for _, instance := range terminated {
		if !instance.TerminatedAt.IsZero() {
			age := now.Sub(instance.TerminatedAt)
			if age > t.config.RetainTerminatedDuration {
				if err := t.db.DeleteInstance(ctx, instance.InstanceID); err != nil {
					t.logger.Error("failed to delete old terminated instance",
						slog.String("instance_id", instance.InstanceID),
						slog.String("error", err.Error()),
					)
					continue
				}
				t.logger.Debug("deleted old terminated instance record",
					slog.String("instance_id", instance.InstanceID),
					slog.Duration("age", age),
				)
			}
		}
	}
}

// Stats returns statistics about tracked instances.
func (t *InstanceManager) Stats(ctx context.Context) (InstanceStats, error) {
	instances, err := t.db.ListInstances(ctx)
	if err != nil {
		return InstanceStats{}, err
	}

	stats := InstanceStats{}
	for _, instance := range instances {
		stats.Total++
		switch instance.State {
		case pb.InstanceState_INSTANCE_STATE_PROVISIONING:
			stats.Provisioning++
		case pb.InstanceState_INSTANCE_STATE_PENDING_REGISTRATION:
			stats.PendingRegistration++
		case pb.InstanceState_INSTANCE_STATE_RUNNING:
			stats.Running++
		case pb.InstanceState_INSTANCE_STATE_TERMINATING:
			stats.Terminating++
		case pb.InstanceState_INSTANCE_STATE_TERMINATED:
			stats.Terminated++
		case pb.InstanceState_INSTANCE_STATE_FAILED:
			stats.Failed++
		}
	}
	return stats, nil
}

// InstanceStats contains counts of instances in each state.
type InstanceStats struct {
	Total               int
	Provisioning        int
	PendingRegistration int
	Running             int
	Terminating         int
	Terminated          int
	Failed              int
}
