package lambda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

const (
	defaultBaseURL = "https://cloud.lambdalabs.com/api/v1"
	defaultTimeout = 30 * time.Second
)

// Provider implements the provider.Provider interface for Lambda Labs Cloud.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// Config holds configuration for the Lambda Labs provider.
type Config struct {
	APIKey  string
	BaseURL string // Optional, defaults to Lambda Labs API
	Timeout time.Duration
}

// New creates a new Lambda Labs provider.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	return &Provider{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
	}, nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "lambda"
}

// Provision creates a new GPU instance on Lambda Labs.
func (p *Provider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	launchReq := launchRequest{
		InstanceTypeName: req.InstanceType,
		RegionName:       req.Region,
		SSHKeyNames:      req.SSHKeyNames,
		Name:             req.Name,
	}

	body, err := json.Marshal(launchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/instance-operations/launch", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to launch instance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, p.parseError(resp)
	}

	var launchResp launchResponse
	if err := json.NewDecoder(resp.Body).Decode(&launchResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(launchResp.Data.InstanceIDs) == 0 {
		return nil, fmt.Errorf("no instance ID returned")
	}

	instanceID := launchResp.Data.InstanceIDs[0]

	return &provider.Node{
		ID:           instanceID,
		Provider:     "lambda",
		Region:       req.Region,
		InstanceType: req.InstanceType,
		Status:       "provisioning",
		Labels:       req.Labels,
	}, nil
}

// Terminate destroys a GPU instance.
func (p *Provider) Terminate(ctx context.Context, nodeID string) error {
	termReq := terminateRequest{
		InstanceIDs: []string{nodeID},
	}

	body, err := json.Marshal(termReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/instance-operations/terminate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to terminate instance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return p.parseError(resp)
	}

	return nil
}

// List returns all running instances.
func (p *Provider) List(ctx context.Context) ([]*provider.Node, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/instances", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.parseError(resp)
	}

	var listResp listInstancesResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	nodes := make([]*provider.Node, 0, len(listResp.Data))
	for _, inst := range listResp.Data {
		nodes = append(nodes, &provider.Node{
			ID:           inst.ID,
			Provider:     "lambda",
			Region:       inst.Region.Name,
			InstanceType: inst.InstanceType.Name,
			Status:       inst.Status,
			IPAddress:    inst.IP,
			GPUCount:     inst.InstanceType.Specs.GPUs,
			GPUType:      inst.InstanceType.Specs.GPUDescription,
		})
	}

	return nodes, nil
}

// ListInstanceTypes returns available GPU instance types.
func (p *Provider) ListInstanceTypes(ctx context.Context) ([]provider.InstanceType, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/instance-types", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list instance types: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.parseError(resp)
	}

	var typesResp instanceTypesResponse
	if err := json.NewDecoder(resp.Body).Decode(&typesResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var types []provider.InstanceType
	for name, data := range typesResp.Data {
		regions := make([]string, 0, len(data.RegionsWithCapacity))
		for _, r := range data.RegionsWithCapacity {
			regions = append(regions, r.Name)
		}

		types = append(types, provider.InstanceType{
			Name:       name,
			GPUCount:   data.Specs.GPUs,
			GPUType:    data.Specs.GPUDescription,
			MemoryGB:   data.Specs.MemoryGB,
			VCPUs:      data.Specs.VCPUs,
			PricePerHr: data.PriceCentsPerHour / 100.0,
			Regions:    regions,
			Available:  len(data.RegionsWithCapacity) > 0,
		})
	}

	return types, nil
}

func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
}

func (p *Provider) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var errResp errorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return fmt.Errorf("lambda API error: %s (code: %s)", errResp.Error.Message, errResp.Error.Code)
	}
	return fmt.Errorf("lambda API error: status %d, body: %s", resp.StatusCode, string(body))
}

type launchRequest struct {
	InstanceTypeName string   `json:"instance_type_name"`
	RegionName       string   `json:"region_name"`
	SSHKeyNames      []string `json:"ssh_key_names"`
	Name             string   `json:"name,omitempty"`
}

type launchResponse struct {
	Data struct {
		InstanceIDs []string `json:"instance_ids"`
	} `json:"data"`
}

type terminateRequest struct {
	InstanceIDs []string `json:"instance_ids"`
}

type listInstancesResponse struct {
	Data []instanceData `json:"data"`
}

type instanceData struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	IP           string `json:"ip"`
	Status       string `json:"status"`
	Region       region `json:"region"`
	InstanceType struct {
		Name  string        `json:"name"`
		Specs instanceSpecs `json:"specs"`
	} `json:"instance_type"`
}

type region struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type instanceTypesResponse struct {
	Data map[string]instanceTypeData `json:"data"`
}

type instanceTypeData struct {
	Specs               instanceSpecs `json:"instance_type"`
	RegionsWithCapacity []region      `json:"regions_with_capacity_available"`
	PriceCentsPerHour   float64       `json:"price_cents_per_hour"`
}

type instanceSpecs struct {
	GPUs           int    `json:"gpus"`
	GPUDescription string `json:"gpu_description,omitempty"`
	MemoryGB       int    `json:"memory_gib"`
	VCPUs          int    `json:"vcpus"`
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
