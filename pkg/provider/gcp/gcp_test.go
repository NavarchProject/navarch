package gcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NavarchProject/navarch/pkg/provider"
)

func TestNewWithClient(t *testing.T) {
	client := &http.Client{}
	p := NewWithClient(Config{
		Project: "test-project",
		Zone:    "us-central1-a",
	}, client)

	if p == nil {
		t.Fatal("NewWithClient returned nil")
	}
	if p.project != "test-project" {
		t.Errorf("project = %v, want test-project", p.project)
	}
	if p.zone != "us-central1-a" {
		t.Errorf("zone = %v, want us-central1-a", p.zone)
	}
}

func TestProvider_Name(t *testing.T) {
	p := NewWithClient(Config{
		Project: "test-project",
		Zone:    "us-central1-a",
	}, &http.Client{})

	if got := p.Name(); got != "gcp" {
		t.Errorf("Name() = %v, want gcp", got)
	}
}

func TestProvider_Provision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/instances") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var req instanceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.Name != "test-gpu-node" {
			t.Errorf("unexpected instance name: %s", req.Name)
		}
		if !strings.Contains(req.MachineType, "a2-highgpu-8g") {
			t.Errorf("unexpected machine type: %s", req.MachineType)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(operationResponse{
			ID:         "op-12345",
			Name:       "operation-12345",
			Status:     "RUNNING",
			TargetLink: "https://compute.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/test-gpu-node",
		})
	}))
	defer server.Close()

	p := NewWithClient(Config{
		Project: "test-project",
		Zone:    "us-central1-a",
		BaseURL: server.URL,
	}, server.Client())

	node, err := p.Provision(context.Background(), provider.ProvisionRequest{
		Name:         "test-gpu-node",
		InstanceType: "a2-highgpu-8g",
		Region:       "us-central1",
		Labels:       map[string]string{"pool": "training"},
	})
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
	if node.ID != "test-gpu-node" {
		t.Errorf("node.ID = %v, want test-gpu-node", node.ID)
	}
	if node.Provider != "gcp" {
		t.Errorf("node.Provider = %v, want gcp", node.Provider)
	}
	if node.Status != "provisioning" {
		t.Errorf("node.Status = %v, want provisioning", node.Status)
	}
	if node.GPUCount != 8 {
		t.Errorf("node.GPUCount = %v, want 8", node.GPUCount)
	}
	if node.GPUType != "NVIDIA A100 40GB" {
		t.Errorf("node.GPUType = %v, want NVIDIA A100 40GB", node.GPUType)
	}
}

func TestProvider_Provision_WithZone(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(operationResponse{
			ID:         "op-12345",
			Name:       "operation-12345",
			Status:     "RUNNING",
			TargetLink: "https://compute.googleapis.com/compute/v1/projects/test-project/zones/us-west1-b/instances/test-node",
		})
	}))
	defer server.Close()

	p := NewWithClient(Config{
		Project: "test-project",
		Zone:    "us-central1-a",
		BaseURL: server.URL,
	}, server.Client())

	_, err := p.Provision(context.Background(), provider.ProvisionRequest{
		Name:         "test-node",
		InstanceType: "g2-standard-8",
		Zone:         "us-west1-b",
	})
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	if !strings.Contains(capturedPath, "us-west1-b") {
		t.Errorf("expected zone us-west1-b in path, got: %s", capturedPath)
	}
}

func TestProvider_Terminate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First request is GET to find instance, second is DELETE
		if r.Method == "GET" {
			if strings.Contains(r.URL.Path, "/aggregated/instances") {
				// Aggregated list - return empty to force default zone
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(aggregatedListResponse{
					Items: map[string]instancesScopedList{},
				})
				return
			}
			// Single instance get - return not found
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if r.Method != "DELETE" {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "test-node") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(operationResponse{
			ID:     "op-delete-123",
			Name:   "delete-operation",
			Status: "RUNNING",
		})
	}))
	defer server.Close()

	p := NewWithClient(Config{
		Project: "test-project",
		Zone:    "us-central1-a",
		BaseURL: server.URL,
	}, server.Client())

	if err := p.Terminate(context.Background(), "test-node"); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
}

func TestProvider_List(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/aggregated/instances") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(aggregatedListResponse{
			Items: map[string]instancesScopedList{
				"zones/us-central1-a": {
					Instances: []instanceData{
						{
							ID:          "12345",
							Name:        "gpu-node-1",
							Zone:        "projects/test-project/zones/us-central1-a",
							MachineType: "zones/us-central1-a/machineTypes/a2-highgpu-8g",
							Status:      "RUNNING",
							NetworkInterfaces: []networkInterface{
								{
									NetworkIP: "10.0.0.5",
									AccessConfigs: []accessConfig{
										{NatIP: "35.192.0.1"},
									},
								},
							},
							Labels: map[string]string{"pool": "training"},
						},
					},
				},
				"zones/us-west1-b": {
					Instances: []instanceData{
						{
							ID:          "67890",
							Name:        "gpu-node-2",
							Zone:        "projects/test-project/zones/us-west1-b",
							MachineType: "zones/us-west1-b/machineTypes/a3-highgpu-8g",
							Status:      "RUNNING",
							NetworkInterfaces: []networkInterface{
								{
									NetworkIP: "10.0.0.6",
								},
							},
						},
						{
							// Non-GPU instance should be filtered out
							ID:          "99999",
							Name:        "regular-vm",
							Zone:        "projects/test-project/zones/us-west1-b",
							MachineType: "zones/us-west1-b/machineTypes/n1-standard-4",
							Status:      "RUNNING",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	p := NewWithClient(Config{
		Project: "test-project",
		Zone:    "us-central1-a",
		BaseURL: server.URL,
	}, server.Client())

	nodes, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Should only include GPU instances (filter out n1-standard-4)
	if len(nodes) != 2 {
		t.Fatalf("List() returned %d nodes, want 2", len(nodes))
	}

	// Find the nodes by name
	var node1, node2 *provider.Node
	for _, n := range nodes {
		if n.ID == "gpu-node-1" {
			node1 = n
		}
		if n.ID == "gpu-node-2" {
			node2 = n
		}
	}

	if node1 == nil {
		t.Fatal("gpu-node-1 not found")
	}
	if node1.IPAddress != "35.192.0.1" {
		t.Errorf("node1.IPAddress = %v, want 35.192.0.1", node1.IPAddress)
	}
	if node1.GPUCount != 8 {
		t.Errorf("node1.GPUCount = %v, want 8", node1.GPUCount)
	}
	if node1.GPUType != "NVIDIA A100 40GB" {
		t.Errorf("node1.GPUType = %v, want NVIDIA A100 40GB", node1.GPUType)
	}
	if node1.Region != "us-central1" {
		t.Errorf("node1.Region = %v, want us-central1", node1.Region)
	}
	if node1.Status != "running" {
		t.Errorf("node1.Status = %v, want running", node1.Status)
	}

	if node2 == nil {
		t.Fatal("gpu-node-2 not found")
	}
	if node2.IPAddress != "10.0.0.6" {
		t.Errorf("node2.IPAddress = %v, want 10.0.0.6 (internal IP)", node2.IPAddress)
	}
	if node2.GPUCount != 8 {
		t.Errorf("node2.GPUCount = %v, want 8", node2.GPUCount)
	}
	if node2.GPUType != "NVIDIA H100 80GB" {
		t.Errorf("node2.GPUType = %v, want NVIDIA H100 80GB", node2.GPUType)
	}
}

func TestProvider_List_WithGuestAccelerators(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(aggregatedListResponse{
			Items: map[string]instancesScopedList{
				"zones/us-central1-a": {
					Instances: []instanceData{
						{
							ID:          "12345",
							Name:        "custom-gpu-node",
							Zone:        "projects/test-project/zones/us-central1-a",
							MachineType: "zones/us-central1-a/machineTypes/n1-standard-8",
							Status:      "RUNNING",
							GuestAccelerators: []accelerator{
								{
									AcceleratorType:  "zones/us-central1-a/acceleratorTypes/nvidia-tesla-v100",
									AcceleratorCount: 4,
								},
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	p := NewWithClient(Config{
		Project: "test-project",
		Zone:    "us-central1-a",
		BaseURL: server.URL,
	}, server.Client())

	nodes, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("List() returned %d nodes, want 1", len(nodes))
	}

	if nodes[0].GPUCount != 4 {
		t.Errorf("GPUCount = %v, want 4", nodes[0].GPUCount)
	}
	if !strings.Contains(nodes[0].GPUType, "V100") {
		t.Errorf("GPUType = %v, want to contain V100", nodes[0].GPUType)
	}
}

func TestProvider_ListInstanceTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/machineTypes") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(machineTypesResponse{
			Items: []machineType{
				{
					Name:      "a2-highgpu-1g",
					GuestCPUs: 12,
					MemoryMB:  87040,
				},
				{
					Name:      "a2-highgpu-8g",
					GuestCPUs: 96,
					MemoryMB:  696320,
				},
				{
					Name:      "g2-standard-8",
					GuestCPUs: 8,
					MemoryMB:  32768,
				},
				{
					// Non-GPU type should be filtered
					Name:      "n1-standard-4",
					GuestCPUs: 4,
					MemoryMB:  15360,
				},
			},
		})
	}))
	defer server.Close()

	p := NewWithClient(Config{
		Project: "test-project",
		Zone:    "us-central1-a",
		BaseURL: server.URL,
	}, server.Client())

	types, err := p.ListInstanceTypes(context.Background())
	if err != nil {
		t.Fatalf("ListInstanceTypes() error = %v", err)
	}

	// Should only return GPU types
	if len(types) != 3 {
		t.Fatalf("ListInstanceTypes() returned %d types, want 3", len(types))
	}

	// Check a2-highgpu-8g
	var a2_8g *provider.InstanceType
	for i := range types {
		if types[i].Name == "a2-highgpu-8g" {
			a2_8g = &types[i]
			break
		}
	}
	if a2_8g == nil {
		t.Fatal("a2-highgpu-8g not found")
	}
	if a2_8g.GPUCount != 8 {
		t.Errorf("a2-highgpu-8g.GPUCount = %v, want 8", a2_8g.GPUCount)
	}
	if a2_8g.GPUType != "NVIDIA A100 40GB" {
		t.Errorf("a2-highgpu-8g.GPUType = %v, want NVIDIA A100 40GB", a2_8g.GPUType)
	}
	if a2_8g.VCPUs != 96 {
		t.Errorf("a2-highgpu-8g.VCPUs = %v, want 96", a2_8g.VCPUs)
	}
}

func TestProvider_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse{
			Error: struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			}{
				Code:    400,
				Message: "Invalid machine type",
				Status:  "INVALID_ARGUMENT",
			},
		})
	}))
	defer server.Close()

	p := NewWithClient(Config{
		Project: "test-project",
		Zone:    "us-central1-a",
		BaseURL: server.URL,
	}, server.Client())

	_, err := p.Provision(context.Background(), provider.ProvisionRequest{
		Name:         "test-node",
		InstanceType: "invalid-type",
	})
	if err == nil {
		t.Fatal("Provision() should return error")
	}
	if !strings.Contains(err.Error(), "Invalid machine type") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_ImplementsInterface(t *testing.T) {
	var _ provider.Provider = (*Provider)(nil)
	var _ provider.InstanceTypeLister = (*Provider)(nil)
}

func TestExtractRegion(t *testing.T) {
	tests := []struct {
		zone   string
		region string
	}{
		{"us-central1-a", "us-central1"},
		{"us-west1-b", "us-west1"},
		{"europe-west4-c", "europe-west4"},
		{"asia-east1-a", "asia-east1"},
	}

	for _, tt := range tests {
		got := extractRegion(tt.zone)
		if got != tt.region {
			t.Errorf("extractRegion(%q) = %q, want %q", tt.zone, got, tt.region)
		}
	}
}

func TestMapGCPStatus(t *testing.T) {
	tests := []struct {
		gcpStatus string
		expected  string
	}{
		{"PROVISIONING", "provisioning"},
		{"STAGING", "provisioning"},
		{"RUNNING", "running"},
		{"STOPPING", "terminating"},
		{"SUSPENDING", "terminating"},
		{"TERMINATED", "terminated"},
		{"SUSPENDED", "terminated"},
	}

	for _, tt := range tests {
		got := mapGCPStatus(tt.gcpStatus)
		if got != tt.expected {
			t.Errorf("mapGCPStatus(%q) = %q, want %q", tt.gcpStatus, got, tt.expected)
		}
	}
}

func TestParseGPUMachineType(t *testing.T) {
	tests := []struct {
		machineType string
		gpuCount    int
		gpuType     string
	}{
		{"a2-highgpu-1g", 1, "NVIDIA A100 40GB"},
		{"a2-highgpu-2g", 2, "NVIDIA A100 40GB"},
		{"a2-highgpu-4g", 4, "NVIDIA A100 40GB"},
		{"a2-highgpu-8g", 8, "NVIDIA A100 40GB"},
		{"a2-ultragpu-1g", 1, "NVIDIA A100 80GB"},
		{"a2-ultragpu-2g", 2, "NVIDIA A100 80GB"},
		{"a2-megagpu-16g", 16, "NVIDIA A100 80GB"},
		{"a3-highgpu-8g", 8, "NVIDIA H100 80GB"},
		{"g2-standard-4", 1, "NVIDIA L4"},
		{"g2-standard-8", 1, "NVIDIA L4"},
		{"g2-standard-24", 2, "NVIDIA L4"},
		{"g2-standard-48", 4, "NVIDIA L4"},
		{"g2-standard-96", 8, "NVIDIA L4"},
		{"n1-standard-4", 0, ""},
	}

	for _, tt := range tests {
		count, gpuType := parseGPUMachineType(tt.machineType)
		if count != tt.gpuCount {
			t.Errorf("parseGPUMachineType(%q) count = %d, want %d", tt.machineType, count, tt.gpuCount)
		}
		if gpuType != tt.gpuType {
			t.Errorf("parseGPUMachineType(%q) gpuType = %q, want %q", tt.machineType, gpuType, tt.gpuType)
		}
	}
}

func TestIsGPUMachineType(t *testing.T) {
	tests := []struct {
		name   string
		isGPU  bool
	}{
		{"a2-highgpu-1g", true},
		{"a2-highgpu-8g", true},
		{"a3-highgpu-8g", true},
		{"g2-standard-8", true},
		{"n1-standard-4", false},
		{"e2-medium", false},
		{"c2-standard-8", false},
	}

	for _, tt := range tests {
		got := isGPUMachineType(tt.name)
		if got != tt.isGPU {
			t.Errorf("isGPUMachineType(%q) = %v, want %v", tt.name, got, tt.isGPU)
		}
	}
}

func TestBuildInstanceRequest(t *testing.T) {
	p := NewWithClient(Config{
		Project: "test-project",
		Zone:    "us-central1-a",
	}, &http.Client{})

	req := p.buildInstanceRequest(provider.ProvisionRequest{
		Name:         "test-node",
		InstanceType: "a2-highgpu-8g",
		Labels:       map[string]string{"env": "prod"},
		SSHKeyNames:  []string{"user:ssh-rsa AAAA..."},
		UserData:     "#!/bin/bash\necho hello",
	}, "us-central1-a")

	if req.Name != "test-node" {
		t.Errorf("Name = %q, want test-node", req.Name)
	}
	if !strings.Contains(req.MachineType, "a2-highgpu-8g") {
		t.Errorf("MachineType = %q, want to contain a2-highgpu-8g", req.MachineType)
	}
	if len(req.Disks) != 1 {
		t.Errorf("Disks count = %d, want 1", len(req.Disks))
	}
	if !req.Disks[0].Boot {
		t.Error("Boot disk should be marked as boot")
	}
	if req.Scheduling.OnHostMaintenance != "TERMINATE" {
		t.Error("GPU instances should terminate on host maintenance")
	}
	if req.Scheduling.AutomaticRestart {
		t.Error("GPU instances should not auto-restart")
	}

	// Check metadata items
	foundSSHKeys := false
	foundStartupScript := false
	for _, item := range req.Metadata.Items {
		if item.Key == "ssh-keys" {
			foundSSHKeys = true
			if !strings.Contains(item.Value, "ssh-rsa") {
				t.Error("SSH keys not properly set")
			}
		}
		if item.Key == "startup-script" {
			foundStartupScript = true
			if !strings.Contains(item.Value, "echo hello") {
				t.Error("Startup script not properly set")
			}
		}
	}
	if !foundSSHKeys {
		t.Error("SSH keys metadata not found")
	}
	if !foundStartupScript {
		t.Error("Startup script metadata not found")
	}
}
