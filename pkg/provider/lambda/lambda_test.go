package lambda

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NavarchProject/navarch/pkg/provider"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid config",
			cfg:     Config{APIKey: "test-key"},
			wantErr: false,
		},
		{
			name:    "missing API key",
			cfg:     Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && p == nil {
				t.Error("New() returned nil provider")
			}
		})
	}
}

func TestProvider_Name(t *testing.T) {
	p, _ := New(Config{APIKey: "test"})
	if got := p.Name(); got != "lambda" {
		t.Errorf("Name() = %v, want lambda", got)
	}
}

func TestProvider_Provision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance-operations/launch" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing or invalid authorization header")
		}

		var req launchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.InstanceTypeName != "gpu_1x_a100_sxm4" {
			t.Errorf("unexpected instance type: %s", req.InstanceTypeName)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(launchResponse{
			Data: struct {
				InstanceIDs []string `json:"instance_ids"`
			}{
				InstanceIDs: []string{"i-12345"},
			},
		})
	}))
	defer server.Close()

	p, _ := New(Config{APIKey: "test-key", BaseURL: server.URL})

	node, err := p.Provision(context.Background(), provider.ProvisionRequest{
		Name:         "test-node",
		InstanceType: "gpu_1x_a100_sxm4",
		Region:       "us-west-2",
		SSHKeyNames:  []string{"my-key"},
	})
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
	if node.ID != "i-12345" {
		t.Errorf("node.ID = %v, want i-12345", node.ID)
	}
	if node.Provider != "lambda" {
		t.Errorf("node.Provider = %v, want lambda", node.Provider)
	}
	if node.Status != "provisioning" {
		t.Errorf("node.Status = %v, want provisioning", node.Status)
	}
}

func TestProvider_Terminate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance-operations/terminate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var req terminateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if len(req.InstanceIDs) != 1 || req.InstanceIDs[0] != "i-12345" {
			t.Errorf("unexpected instance IDs: %v", req.InstanceIDs)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	}))
	defer server.Close()

	p, _ := New(Config{APIKey: "test-key", BaseURL: server.URL})

	if err := p.Terminate(context.Background(), "i-12345"); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
}

func TestProvider_List(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instances" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(listInstancesResponse{
			Data: []instanceData{
				{
					ID:     "i-12345",
					Name:   "test-node",
					IP:     "10.0.0.1",
					Status: "running",
					Region: region{Name: "us-west-2"},
					InstanceType: struct {
						Name  string        `json:"name"`
						Specs instanceSpecs `json:"specs"`
					}{
						Name:  "gpu_1x_a100_sxm4",
						Specs: instanceSpecs{GPUs: 1, GPUDescription: "A100 SXM4"},
					},
				},
				{
					ID:     "i-67890",
					Name:   "test-node-2",
					IP:     "10.0.0.2",
					Status: "running",
					Region: region{Name: "us-east-1"},
					InstanceType: struct {
						Name  string        `json:"name"`
						Specs instanceSpecs `json:"specs"`
					}{
						Name:  "gpu_8x_a100_80gb_sxm4",
						Specs: instanceSpecs{GPUs: 8, GPUDescription: "A100 80GB SXM4"},
					},
				},
			},
		})
	}))
	defer server.Close()

	p, _ := New(Config{APIKey: "test-key", BaseURL: server.URL})

	nodes, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("List() returned %d nodes, want 2", len(nodes))
	}
	if nodes[0].ID != "i-12345" {
		t.Errorf("nodes[0].ID = %v, want i-12345", nodes[0].ID)
	}
	if nodes[0].GPUCount != 1 {
		t.Errorf("nodes[0].GPUCount = %v, want 1", nodes[0].GPUCount)
	}
	if nodes[1].GPUCount != 8 {
		t.Errorf("nodes[1].GPUCount = %v, want 8", nodes[1].GPUCount)
	}
}

func TestProvider_ListInstanceTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance-types" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(instanceTypesResponse{
			Data: map[string]instanceTypeData{
				"gpu_1x_a100_sxm4": {
					Specs: instanceSpecs{
						GPUs:           1,
						GPUDescription: "A100 SXM4 40GB",
						MemoryGB:       200,
						VCPUs:          30,
					},
					RegionsWithCapacity: []region{
						{Name: "us-west-2"},
						{Name: "us-east-1"},
					},
					PriceCentsPerHour: 110,
				},
				"gpu_8x_h100_sxm5": {
					Specs: instanceSpecs{
						GPUs:           8,
						GPUDescription: "H100 SXM5 80GB",
						MemoryGB:       1800,
						VCPUs:          208,
					},
					RegionsWithCapacity: []region{},
					PriceCentsPerHour:   2400,
				},
			},
		})
	}))
	defer server.Close()

	p, _ := New(Config{APIKey: "test-key", BaseURL: server.URL})

	types, err := p.ListInstanceTypes(context.Background())
	if err != nil {
		t.Fatalf("ListInstanceTypes() error = %v", err)
	}
	if len(types) != 2 {
		t.Fatalf("ListInstanceTypes() returned %d types, want 2", len(types))
	}

	var a100, h100 *provider.InstanceType
	for i := range types {
		if types[i].Name == "gpu_1x_a100_sxm4" {
			a100 = &types[i]
		}
		if types[i].Name == "gpu_8x_h100_sxm5" {
			h100 = &types[i]
		}
	}

	if a100 == nil {
		t.Fatal("gpu_1x_a100_sxm4 not found")
	}
	if !a100.Available {
		t.Error("gpu_1x_a100_sxm4 should be available")
	}
	if a100.PricePerHr != 1.10 {
		t.Errorf("a100.PricePerHr = %v, want 1.10", a100.PricePerHr)
	}

	if h100 == nil {
		t.Fatal("gpu_8x_h100_sxm5 not found")
	}
	if h100.Available {
		t.Error("gpu_8x_h100_sxm5 should not be available (no regions)")
	}
}

func TestProvider_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse{
			Error: struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}{
				Code:    "invalid_request",
				Message: "Instance type not available",
			},
		})
	}))
	defer server.Close()

	p, _ := New(Config{APIKey: "test-key", BaseURL: server.URL})

	_, err := p.Provision(context.Background(), provider.ProvisionRequest{
		InstanceType: "invalid-type",
		Region:       "us-west-2",
	})
	if err == nil {
		t.Fatal("Provision() should return error")
	}
	if err.Error() != "lambda API error: Instance type not available (code: invalid_request)" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_ImplementsInterface(t *testing.T) {
	var _ provider.Provider = (*Provider)(nil)
	var _ provider.InstanceTypeLister = (*Provider)(nil)
}

