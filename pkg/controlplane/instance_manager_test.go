package controlplane

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

// TestInstanceManager_TrackProvisioning tests the provisioning tracking flow
func TestInstanceManager_TrackProvisioning(t *testing.T) {
	t.Run("track_new_instance", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()

		im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
		ctx := context.Background()

		err := im.TrackProvisioning(ctx, "i-12345", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "gpu-pool", map[string]string{"env": "prod"})
		if err != nil {
			t.Fatalf("TrackProvisioning failed: %v", err)
		}

		// Verify instance was created
		instance, err := im.GetInstance(ctx, "i-12345")
		if err != nil {
			t.Fatalf("GetInstance failed: %v", err)
		}

		if instance.InstanceID != "i-12345" {
			t.Errorf("Expected instance_id i-12345, got %s", instance.InstanceID)
		}
		if instance.Provider != "gcp" {
			t.Errorf("Expected provider gcp, got %s", instance.Provider)
		}
		if instance.State != pb.InstanceState_INSTANCE_STATE_PROVISIONING {
			t.Errorf("Expected state PROVISIONING, got %v", instance.State)
		}
		if instance.PoolName != "gpu-pool" {
			t.Errorf("Expected pool_name gpu-pool, got %s", instance.PoolName)
		}
		if instance.Labels["env"] != "prod" {
			t.Error("Expected labels to be preserved")
		}
	})

	t.Run("duplicate_instance_fails", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()

		im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
		ctx := context.Background()

		// First creation should succeed
		err := im.TrackProvisioning(ctx, "i-12345", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
		if err != nil {
			t.Fatalf("First TrackProvisioning failed: %v", err)
		}

		// Duplicate should fail
		err = im.TrackProvisioning(ctx, "i-12345", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
		if err == nil {
			t.Error("Expected error for duplicate instance")
		}
	})
}

// TestInstanceManager_ProvisioningComplete tests transitioning to pending_registration state
func TestInstanceManager_ProvisioningComplete(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// Create instance in provisioning state
	im.TrackProvisioning(ctx, "i-12345", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)

	// Mark provisioning complete
	err := im.TrackProvisioningComplete(ctx, "i-12345")
	if err != nil {
		t.Fatalf("TrackProvisioningComplete failed: %v", err)
	}

	// Verify state changed
	instance, _ := im.GetInstance(ctx, "i-12345")
	if instance.State != pb.InstanceState_INSTANCE_STATE_PENDING_REGISTRATION {
		t.Errorf("Expected state PENDING_REGISTRATION, got %v", instance.State)
	}
}

// TestInstanceManager_ProvisioningFailed tests marking instance as failed
func TestInstanceManager_ProvisioningFailed(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// Create instance
	im.TrackProvisioning(ctx, "i-12345", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)

	// Mark as failed
	err := im.TrackProvisioningFailed(ctx, "i-12345", "quota exceeded")
	if err != nil {
		t.Fatalf("TrackProvisioningFailed failed: %v", err)
	}

	// Verify state
	instance, _ := im.GetInstance(ctx, "i-12345")
	if instance.State != pb.InstanceState_INSTANCE_STATE_FAILED {
		t.Errorf("Expected state FAILED, got %v", instance.State)
	}
	if instance.StatusMessage != "quota exceeded" {
		t.Errorf("Expected status message 'quota exceeded', got %s", instance.StatusMessage)
	}
}

// TestInstanceManager_NodeRegistered tests transitioning to running state when node registers
func TestInstanceManager_NodeRegistered(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// Create instance and mark as pending registration
	im.TrackProvisioning(ctx, "i-12345", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningComplete(ctx, "i-12345")

	// Simulate node registration
	err := im.TrackNodeRegistered(ctx, "i-12345", "node-1")
	if err != nil {
		t.Fatalf("TrackNodeRegistered failed: %v", err)
	}

	// Verify state
	instance, _ := im.GetInstance(ctx, "i-12345")
	if instance.State != pb.InstanceState_INSTANCE_STATE_RUNNING {
		t.Errorf("Expected state RUNNING, got %v", instance.State)
	}
	if instance.NodeID != "node-1" {
		t.Errorf("Expected node_id node-1, got %s", instance.NodeID)
	}
	if instance.ReadyAt.IsZero() {
		t.Error("Expected ReadyAt to be set")
	}
}

// TestInstanceManager_NodeRegistered_UnknownInstance tests registering node for unknown instance
func TestInstanceManager_NodeRegistered_UnknownInstance(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// Try to register node for non-existent instance
	// This should not error - node may have been provisioned externally
	err := im.TrackNodeRegistered(ctx, "unknown-instance", "node-1")
	if err != nil {
		t.Errorf("Expected no error for unknown instance, got: %v", err)
	}
}

// TestInstanceManager_Termination tests the termination flow
func TestInstanceManager_Termination(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// Create and start instance
	im.TrackProvisioning(ctx, "i-12345", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningComplete(ctx, "i-12345")
	im.TrackNodeRegistered(ctx, "i-12345", "node-1")

	// Mark as terminating
	err := im.TrackTerminating(ctx, "i-12345")
	if err != nil {
		t.Fatalf("TrackTerminating failed: %v", err)
	}

	instance, _ := im.GetInstance(ctx, "i-12345")
	if instance.State != pb.InstanceState_INSTANCE_STATE_TERMINATING {
		t.Errorf("Expected state TERMINATING, got %v", instance.State)
	}

	// Mark as terminated
	err = im.TrackTerminated(ctx, "i-12345")
	if err != nil {
		t.Fatalf("TrackTerminated failed: %v", err)
	}

	instance, _ = im.GetInstance(ctx, "i-12345")
	if instance.State != pb.InstanceState_INSTANCE_STATE_TERMINATED {
		t.Errorf("Expected state TERMINATED, got %v", instance.State)
	}
	if instance.TerminatedAt.IsZero() {
		t.Error("Expected TerminatedAt to be set")
	}
}

// TestInstanceManager_ListInstances tests listing instances
func TestInstanceManager_ListInstances(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// Create multiple instances
	im.TrackProvisioning(ctx, "i-1", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool-1", nil)
	im.TrackProvisioning(ctx, "i-2", "aws", "us-west-2", "us-west-2a", "p5.48xlarge", "pool-2", nil)
	im.TrackProvisioning(ctx, "i-3", "gcp", "us-central1", "us-central1-b", "a3-highgpu-8g", "pool-1", nil)

	// List all instances
	instances, err := im.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(instances) != 3 {
		t.Errorf("Expected 3 instances, got %d", len(instances))
	}
}

// TestInstanceManager_ListPendingRegistration tests listing pending instances
func TestInstanceManager_ListPendingRegistration(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// Create instances in different states
	im.TrackProvisioning(ctx, "i-1", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningComplete(ctx, "i-1") // pending_registration

	im.TrackProvisioning(ctx, "i-2", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningComplete(ctx, "i-2")
	im.TrackNodeRegistered(ctx, "i-2", "node-2") // running

	im.TrackProvisioning(ctx, "i-3", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningComplete(ctx, "i-3") // pending_registration

	// List pending instances
	pending, err := im.ListPendingRegistration(ctx)
	if err != nil {
		t.Fatalf("ListPendingRegistration failed: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("Expected 2 pending instances, got %d", len(pending))
	}
}

// TestInstanceManager_Stats tests statistics gathering
func TestInstanceManager_Stats(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// Create instances in various states
	im.TrackProvisioning(ctx, "i-1", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)

	im.TrackProvisioning(ctx, "i-2", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningComplete(ctx, "i-2")

	im.TrackProvisioning(ctx, "i-3", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningComplete(ctx, "i-3")
	im.TrackNodeRegistered(ctx, "i-3", "node-3")

	im.TrackProvisioning(ctx, "i-4", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningFailed(ctx, "i-4", "error")

	stats, err := im.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.Total != 4 {
		t.Errorf("Expected 4 total, got %d", stats.Total)
	}
	if stats.Provisioning != 1 {
		t.Errorf("Expected 1 provisioning, got %d", stats.Provisioning)
	}
	if stats.PendingRegistration != 1 {
		t.Errorf("Expected 1 pending, got %d", stats.PendingRegistration)
	}
	if stats.Running != 1 {
		t.Errorf("Expected 1 running, got %d", stats.Running)
	}
	if stats.Failed != 1 {
		t.Errorf("Expected 1 failed, got %d", stats.Failed)
	}
}

// TestInstanceManager_StaleInstanceDetection tests the stale instance detection loop
func TestInstanceManager_StaleInstanceDetection(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	// Use very short timeout for testing
	config := InstanceManagerConfig{
		RegistrationTimeout:      100 * time.Millisecond,
		StaleCheckInterval:       50 * time.Millisecond,
		RetainTerminatedDuration: time.Hour,
	}

	im := NewInstanceManager(database, config, nil)
	ctx := context.Background()

	// Track if callback was called
	staleCallbackCalled := false
	im.OnStaleInstance(func(instance *db.InstanceRecord) {
		staleCallbackCalled = true
	})

	// Create an instance that will become stale
	im.TrackProvisioning(ctx, "i-stale", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningComplete(ctx, "i-stale")

	// Start the manager
	im.Start(ctx)
	defer im.Stop()

	// Wait for stale detection to trigger
	time.Sleep(200 * time.Millisecond)

	// Verify instance was marked as failed
	instance, err := im.GetInstance(ctx, "i-stale")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if instance.State != pb.InstanceState_INSTANCE_STATE_FAILED {
		t.Errorf("Expected stale instance to be marked FAILED, got %v", instance.State)
	}
	if !staleCallbackCalled {
		t.Error("Expected stale callback to be called")
	}
}

// TestInstanceManager_OnFailedCallback tests the failed instance callback
func TestInstanceManager_OnFailedCallback(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// Track if callback was called
	var failedInstance *db.InstanceRecord
	im.OnFailedInstance(func(instance *db.InstanceRecord) {
		failedInstance = instance
	})

	// Create instance and mark as failed
	im.TrackProvisioning(ctx, "i-12345", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningFailed(ctx, "i-12345", "quota exceeded")

	if failedInstance == nil {
		t.Error("Expected failed callback to be called")
	}
	if failedInstance != nil && failedInstance.InstanceID != "i-12345" {
		t.Errorf("Expected instance_id i-12345, got %s", failedInstance.InstanceID)
	}
}

// TestServer_ListInstances tests the ListInstances RPC handler
func TestServer_ListInstances(t *testing.T) {
	t.Run("list_all_instances", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()

		im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
		srv := NewServer(database, DefaultConfig(), im, nil)
		ctx := context.Background()

		// Create some instances
		im.TrackProvisioning(ctx, "i-1", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool-1", nil)
		im.TrackProvisioning(ctx, "i-2", "aws", "us-west-2", "us-west-2a", "p5.48xlarge", "pool-2", nil)

		// List all instances
		resp, err := srv.ListInstances(ctx, connect.NewRequest(&pb.ListInstancesRequest{}))
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}
		if len(resp.Msg.Instances) != 2 {
			t.Errorf("Expected 2 instances, got %d", len(resp.Msg.Instances))
		}
	})

	t.Run("filter_by_provider", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()

		im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
		srv := NewServer(database, DefaultConfig(), im, nil)
		ctx := context.Background()

		im.TrackProvisioning(ctx, "i-1", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
		im.TrackProvisioning(ctx, "i-2", "aws", "us-west-2", "us-west-2a", "p5.48xlarge", "pool", nil)

		resp, err := srv.ListInstances(ctx, connect.NewRequest(&pb.ListInstancesRequest{
			Provider: "gcp",
		}))
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}
		if len(resp.Msg.Instances) != 1 {
			t.Errorf("Expected 1 GCP instance, got %d", len(resp.Msg.Instances))
		}
	})

	t.Run("filter_by_pool", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()

		im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
		srv := NewServer(database, DefaultConfig(), im, nil)
		ctx := context.Background()

		im.TrackProvisioning(ctx, "i-1", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool-a", nil)
		im.TrackProvisioning(ctx, "i-2", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool-b", nil)

		resp, err := srv.ListInstances(ctx, connect.NewRequest(&pb.ListInstancesRequest{
			PoolName: "pool-a",
		}))
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}
		if len(resp.Msg.Instances) != 1 {
			t.Errorf("Expected 1 instance in pool-a, got %d", len(resp.Msg.Instances))
		}
	})

	t.Run("filter_by_state", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()

		im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
		srv := NewServer(database, DefaultConfig(), im, nil)
		ctx := context.Background()

		im.TrackProvisioning(ctx, "i-1", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
		im.TrackProvisioningComplete(ctx, "i-1")
		im.TrackNodeRegistered(ctx, "i-1", "node-1") // RUNNING

		im.TrackProvisioning(ctx, "i-2", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
		im.TrackProvisioningFailed(ctx, "i-2", "error") // FAILED

		resp, err := srv.ListInstances(ctx, connect.NewRequest(&pb.ListInstancesRequest{
			State: pb.InstanceState_INSTANCE_STATE_RUNNING,
		}))
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}
		if len(resp.Msg.Instances) != 1 {
			t.Errorf("Expected 1 running instance, got %d", len(resp.Msg.Instances))
		}
	})
}

// TestServer_GetInstance tests the GetInstance RPC handler
func TestServer_GetInstance(t *testing.T) {
	t.Run("get_existing_instance", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()

		im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
		srv := NewServer(database, DefaultConfig(), im, nil)
		ctx := context.Background()

		im.TrackProvisioning(ctx, "i-12345", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "gpu-pool", map[string]string{"env": "prod"})
		im.TrackProvisioningComplete(ctx, "i-12345")
		im.TrackNodeRegistered(ctx, "i-12345", "node-1")

		resp, err := srv.GetInstance(ctx, connect.NewRequest(&pb.GetInstanceRequest{
			InstanceId: "i-12345",
		}))
		if err != nil {
			t.Fatalf("GetInstance failed: %v", err)
		}

		instance := resp.Msg.Instance
		if instance.InstanceId != "i-12345" {
			t.Errorf("Expected instance_id i-12345, got %s", instance.InstanceId)
		}
		if instance.Provider != "gcp" {
			t.Errorf("Expected provider gcp, got %s", instance.Provider)
		}
		if instance.State != pb.InstanceState_INSTANCE_STATE_RUNNING {
			t.Errorf("Expected state RUNNING, got %v", instance.State)
		}
		if instance.NodeId != "node-1" {
			t.Errorf("Expected node_id node-1, got %s", instance.NodeId)
		}
		if instance.PoolName != "gpu-pool" {
			t.Errorf("Expected pool_name gpu-pool, got %s", instance.PoolName)
		}
		if instance.Labels["env"] != "prod" {
			t.Error("Expected labels to be preserved")
		}
	})

	t.Run("instance_not_found", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()

		im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
		srv := NewServer(database, DefaultConfig(), im, nil)
		ctx := context.Background()

		_, err := srv.GetInstance(ctx, connect.NewRequest(&pb.GetInstanceRequest{
			InstanceId: "nonexistent",
		}))
		if err == nil {
			t.Error("Expected error for nonexistent instance")
		}
	})

	t.Run("missing_instance_id", func(t *testing.T) {
		database := db.NewInMemDB()
		defer database.Close()

		im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
		srv := NewServer(database, DefaultConfig(), im, nil)
		ctx := context.Background()

		_, err := srv.GetInstance(ctx, connect.NewRequest(&pb.GetInstanceRequest{
			InstanceId: "",
		}))
		if err == nil {
			t.Error("Expected error for missing instance_id")
		}
	})
}

// TestServer_RegisterNode_UpdatesInstanceTracking tests that node registration updates instance tracking
func TestServer_RegisterNode_UpdatesInstanceTracking(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	srv := NewServer(database, DefaultConfig(), im, nil)
	ctx := context.Background()

	// First, provision an instance
	im.TrackProvisioning(ctx, "node-1", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)
	im.TrackProvisioningComplete(ctx, "node-1")

	// Verify instance is pending registration
	instance, _ := im.GetInstance(ctx, "node-1")
	if instance.State != pb.InstanceState_INSTANCE_STATE_PENDING_REGISTRATION {
		t.Errorf("Expected state PENDING_REGISTRATION, got %v", instance.State)
	}

	// Now register the node (using the same ID as instance ID, as is typical)
	req := connect.NewRequest(&pb.RegisterNodeRequest{
		NodeId:       "node-1",
		Provider:     "gcp",
		Region:       "us-central1",
		Zone:         "us-central1-a",
		InstanceType: "a3-highgpu-8g",
	})
	_, err := srv.RegisterNode(ctx, req)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	// Verify instance is now running
	instance, _ = im.GetInstance(ctx, "node-1")
	if instance.State != pb.InstanceState_INSTANCE_STATE_RUNNING {
		t.Errorf("Expected state RUNNING after node registration, got %v", instance.State)
	}
	if instance.NodeID != "node-1" {
		t.Errorf("Expected node_id node-1, got %s", instance.NodeID)
	}
}

// TestInstanceLifecycle_Complete tests the complete instance lifecycle
func TestInstanceLifecycle_Complete(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// 1. Start provisioning
	err := im.TrackProvisioning(ctx, "i-lifecycle", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "gpu-pool", nil)
	if err != nil {
		t.Fatalf("TrackProvisioning failed: %v", err)
	}

	instance, _ := im.GetInstance(ctx, "i-lifecycle")
	if instance.State != pb.InstanceState_INSTANCE_STATE_PROVISIONING {
		t.Errorf("Step 1: Expected PROVISIONING, got %v", instance.State)
	}

	// 2. Provisioning complete
	im.TrackProvisioningComplete(ctx, "i-lifecycle")
	instance, _ = im.GetInstance(ctx, "i-lifecycle")
	if instance.State != pb.InstanceState_INSTANCE_STATE_PENDING_REGISTRATION {
		t.Errorf("Step 2: Expected PENDING_REGISTRATION, got %v", instance.State)
	}

	// 3. Node registers
	im.TrackNodeRegistered(ctx, "i-lifecycle", "node-1")
	instance, _ = im.GetInstance(ctx, "i-lifecycle")
	if instance.State != pb.InstanceState_INSTANCE_STATE_RUNNING {
		t.Errorf("Step 3: Expected RUNNING, got %v", instance.State)
	}
	if instance.NodeID != "node-1" {
		t.Error("Step 3: Expected node_id to be set")
	}
	if instance.ReadyAt.IsZero() {
		t.Error("Step 3: Expected ReadyAt to be set")
	}

	// 4. Terminating
	im.TrackTerminating(ctx, "i-lifecycle")
	instance, _ = im.GetInstance(ctx, "i-lifecycle")
	if instance.State != pb.InstanceState_INSTANCE_STATE_TERMINATING {
		t.Errorf("Step 4: Expected TERMINATING, got %v", instance.State)
	}

	// 5. Terminated
	im.TrackTerminated(ctx, "i-lifecycle")
	instance, _ = im.GetInstance(ctx, "i-lifecycle")
	if instance.State != pb.InstanceState_INSTANCE_STATE_TERMINATED {
		t.Errorf("Step 5: Expected TERMINATED, got %v", instance.State)
	}
	if instance.TerminatedAt.IsZero() {
		t.Error("Step 5: Expected TerminatedAt to be set")
	}
}

// TestInstanceLifecycle_ProvisioningFailure tests the failure path
func TestInstanceLifecycle_ProvisioningFailure(t *testing.T) {
	database := db.NewInMemDB()
	defer database.Close()

	im := NewInstanceManager(database, DefaultInstanceManagerConfig(), nil)
	ctx := context.Background()

	// 1. Start provisioning
	im.TrackProvisioning(ctx, "i-fail", "gcp", "us-central1", "us-central1-a", "a3-highgpu-8g", "pool", nil)

	// 2. Provisioning fails
	im.TrackProvisioningFailed(ctx, "i-fail", "API error: quota exceeded")

	instance, _ := im.GetInstance(ctx, "i-fail")
	if instance.State != pb.InstanceState_INSTANCE_STATE_FAILED {
		t.Errorf("Expected FAILED, got %v", instance.State)
	}
	if instance.StatusMessage != "API error: quota exceeded" {
		t.Errorf("Expected error message, got %s", instance.StatusMessage)
	}
}
