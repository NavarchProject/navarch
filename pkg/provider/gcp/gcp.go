package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
	"golang.org/x/oauth2/google"
)

const (
	defaultBaseURL = "https://compute.googleapis.com/compute/v1"
	defaultTimeout = 60 * time.Second
)

// Provider implements the provider.Provider interface for Google Cloud Platform.
type Provider struct {
	project string
	zone    string
	baseURL string
	client  *http.Client
}

// Config holds configuration for the GCP provider.
type Config struct {
	Project string        // GCP project ID
	Zone    string        // Default zone (e.g., "us-central1-a")
	BaseURL string        // Optional, for testing
	Timeout time.Duration // HTTP client timeout
}

// New creates a new GCP provider using Application Default Credentials.
func New(cfg Config) (*Provider, error) {
	if cfg.Project == "" {
		return nil, fmt.Errorf("project is required")
	}
	if cfg.Zone == "" {
		return nil, fmt.Errorf("zone is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	// Use Application Default Credentials
	ctx := context.Background()
	client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/compute")
	if err != nil {
		return nil, fmt.Errorf("failed to create authenticated client: %w", err)
	}
	client.Timeout = timeout

	return &Provider{
		project: cfg.Project,
		zone:    cfg.Zone,
		baseURL: baseURL,
		client:  client,
	}, nil
}

// NewWithClient creates a new GCP provider with a custom HTTP client (for testing).
func NewWithClient(cfg Config, client *http.Client) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Provider{
		project: cfg.Project,
		zone:    cfg.Zone,
		baseURL: baseURL,
		client:  client,
	}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "gcp"
}

// Provision creates a new GPU instance on GCP.
func (p *Provider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	zone := p.zone
	if req.Zone != "" {
		zone = req.Zone
	}

	instanceReq := p.buildInstanceRequest(req, zone)

	body, err := json.Marshal(instanceReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/projects/%s/zones/%s/instances", p.baseURL, p.project, zone)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, p.parseError(resp)
	}

	var opResp operationResponse
	if err := json.NewDecoder(resp.Body).Decode(&opResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract instance name from the target link or use the request name
	instanceName := req.Name
	if opResp.TargetLink != "" {
		parts := strings.Split(opResp.TargetLink, "/")
		if len(parts) > 0 {
			instanceName = parts[len(parts)-1]
		}
	}

	gpuCount, gpuType := p.parseGPUFromMachineType(req.InstanceType)

	return &provider.Node{
		ID:           instanceName,
		Provider:     "gcp",
		Region:       extractRegion(zone),
		Zone:         zone,
		InstanceType: req.InstanceType,
		Status:       "provisioning",
		GPUCount:     gpuCount,
		GPUType:      gpuType,
		Labels:       req.Labels,
	}, nil
}

// Terminate deletes a GPU instance.
func (p *Provider) Terminate(ctx context.Context, nodeID string) error {
	// Try to find the instance to get its zone
	instance, err := p.getInstance(ctx, nodeID)
	if err != nil {
		// If we can't find it, try with default zone
		return p.terminateInZone(ctx, nodeID, p.zone)
	}

	zone := p.zone
	if instance.Zone != "" {
		// Zone comes as full URL, extract just the zone name
		parts := strings.Split(instance.Zone, "/")
		if len(parts) > 0 {
			zone = parts[len(parts)-1]
		}
	}

	return p.terminateInZone(ctx, nodeID, zone)
}

func (p *Provider) terminateInZone(ctx context.Context, nodeID, zone string) error {
	url := fmt.Sprintf("%s/projects/%s/zones/%s/instances/%s", p.baseURL, p.project, zone, nodeID)
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to terminate instance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return p.parseError(resp)
	}

	return nil
}

// List returns all running instances in the configured zone.
func (p *Provider) List(ctx context.Context) ([]*provider.Node, error) {
	// List instances across all zones in the project using aggregated list
	url := fmt.Sprintf("%s/projects/%s/aggregated/instances?filter=status!=TERMINATED", p.baseURL, p.project)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.parseError(resp)
	}

	var listResp aggregatedListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var nodes []*provider.Node
	for zonePath, scopedList := range listResp.Items {
		// zonePath is like "zones/us-central1-a"
		zone := strings.TrimPrefix(zonePath, "zones/")

		for _, inst := range scopedList.Instances {
			// Only include GPU instances (A2, G2, or instances with guest accelerators)
			if !p.isGPUInstance(inst) {
				continue
			}

			gpuCount, gpuType := p.extractGPUInfo(inst)

			node := &provider.Node{
				ID:           inst.Name,
				Provider:     "gcp",
				Region:       extractRegion(zone),
				Zone:         zone,
				InstanceType: extractMachineTypeName(inst.MachineType),
				Status:       mapGCPStatus(inst.Status),
				GPUCount:     gpuCount,
				GPUType:      gpuType,
				Labels:       inst.Labels,
			}

			// Get external IP if available
			for _, iface := range inst.NetworkInterfaces {
				for _, ac := range iface.AccessConfigs {
					if ac.NatIP != "" {
						node.IPAddress = ac.NatIP
						break
					}
				}
				if node.IPAddress != "" {
					break
				}
			}
			// Fall back to internal IP
			if node.IPAddress == "" && len(inst.NetworkInterfaces) > 0 {
				node.IPAddress = inst.NetworkInterfaces[0].NetworkIP
			}

			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}

// ListInstanceTypes returns available GPU instance types.
func (p *Provider) ListInstanceTypes(ctx context.Context) ([]provider.InstanceType, error) {
	url := fmt.Sprintf("%s/projects/%s/zones/%s/machineTypes", p.baseURL, p.project, p.zone)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list machine types: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.parseError(resp)
	}

	var listResp machineTypesResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var types []provider.InstanceType
	for _, mt := range listResp.Items {
		// Only include GPU machine types (A2, A3, G2 families)
		if !isGPUMachineType(mt.Name) {
			continue
		}

		gpuCount, gpuType := parseGPUMachineType(mt.Name)

		types = append(types, provider.InstanceType{
			Name:     mt.Name,
			GPUCount: gpuCount,
			GPUType:  gpuType,
			MemoryGB: int(mt.MemoryMB / 1024),
			VCPUs:    mt.GuestCPUs,
			Regions:  []string{extractRegion(p.zone)},
			// Note: Pricing not available via Compute API
			// Would need Cloud Billing API for accurate pricing
			Available: true,
		})
	}

	return types, nil
}

func (p *Provider) getInstance(ctx context.Context, name string) (*instanceData, error) {
	// First try default zone
	instance, err := p.getInstanceInZone(ctx, name, p.zone)
	if err == nil {
		return instance, nil
	}

	// If not found in default zone, search across all zones
	nodes, err := p.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, node := range nodes {
		if node.ID == name {
			return &instanceData{
				Name:   node.ID,
				Zone:   fmt.Sprintf("zones/%s", node.Zone),
				Status: node.Status,
			}, nil
		}
	}

	return nil, fmt.Errorf("instance %s not found", name)
}

func (p *Provider) getInstanceInZone(ctx context.Context, name, zone string) (*instanceData, error) {
	url := fmt.Sprintf("%s/projects/%s/zones/%s/instances/%s", p.baseURL, p.project, zone, name)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("instance not found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, p.parseError(resp)
	}

	var inst instanceData
	if err := json.NewDecoder(resp.Body).Decode(&inst); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &inst, nil
}

func (p *Provider) buildInstanceRequest(req provider.ProvisionRequest, zone string) instanceRequest {
	machineType := fmt.Sprintf("zones/%s/machineTypes/%s", zone, req.InstanceType)

	instance := instanceRequest{
		Name:        req.Name,
		MachineType: machineType,
		Disks: []diskConfig{
			{
				Boot:       true,
				AutoDelete: true,
				InitializeParams: &diskInitParams{
					// Default to Deep Learning VM image with CUDA
					SourceImage: "projects/deeplearning-platform-release/global/images/family/common-cu121-debian-11-py310",
					DiskSizeGB:  200,
					DiskType:    fmt.Sprintf("zones/%s/diskTypes/pd-ssd", zone),
				},
			},
		},
		NetworkInterfaces: []networkInterface{
			{
				Network: "global/networks/default",
				AccessConfigs: []accessConfig{
					{
						Type: "ONE_TO_ONE_NAT",
						Name: "External NAT",
					},
				},
			},
		},
		Labels: req.Labels,
		Metadata: &metadata{
			Items: []metadataItem{
				{
					Key:   "enable-oslogin",
					Value: "TRUE",
				},
			},
		},
		Scheduling: &scheduling{
			OnHostMaintenance: "TERMINATE",
			AutomaticRestart:  false,
		},
	}

	// Add SSH keys if provided
	if len(req.SSHKeyNames) > 0 {
		// GCP expects SSH keys in metadata format: "username:ssh-rsa AAAA..."
		// For navarch, we'll expect full SSH key strings
		sshKeysValue := strings.Join(req.SSHKeyNames, "\n")
		instance.Metadata.Items = append(instance.Metadata.Items, metadataItem{
			Key:   "ssh-keys",
			Value: sshKeysValue,
		})
	}

	// Add startup script if provided
	if req.UserData != "" {
		instance.Metadata.Items = append(instance.Metadata.Items, metadataItem{
			Key:   "startup-script",
			Value: req.UserData,
		})
	}

	return instance
}

func (p *Provider) isGPUInstance(inst instanceData) bool {
	machineType := extractMachineTypeName(inst.MachineType)

	// Check for GPU machine type families
	if strings.HasPrefix(machineType, "a2-") ||
		strings.HasPrefix(machineType, "a3-") ||
		strings.HasPrefix(machineType, "g2-") {
		return true
	}

	// Check for guest accelerators
	return len(inst.GuestAccelerators) > 0
}

func (p *Provider) extractGPUInfo(inst instanceData) (int, string) {
	// First check guest accelerators
	if len(inst.GuestAccelerators) > 0 {
		total := 0
		gpuType := ""
		for _, acc := range inst.GuestAccelerators {
			total += acc.AcceleratorCount
			if gpuType == "" {
				gpuType = extractAcceleratorName(acc.AcceleratorType)
			}
		}
		return total, gpuType
	}

	// Otherwise parse from machine type
	machineType := extractMachineTypeName(inst.MachineType)
	return p.parseGPUFromMachineType(machineType)
}

func (p *Provider) parseGPUFromMachineType(machineType string) (int, string) {
	return parseGPUMachineType(machineType)
}

func (p *Provider) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var errResp errorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return fmt.Errorf("GCP API error: %s (code: %d)", errResp.Error.Message, errResp.Error.Code)
	}
	return fmt.Errorf("GCP API error: status %d, body: %s", resp.StatusCode, string(body))
}

// Helper functions

func extractRegion(zone string) string {
	// Zone format: "us-central1-a" -> "us-central1"
	parts := strings.Split(zone, "-")
	if len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-1], "-")
	}
	return zone
}

func extractMachineTypeName(fullPath string) string {
	// MachineType comes as full URL, extract just the name
	parts := strings.Split(fullPath, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return fullPath
}

func extractAcceleratorName(fullPath string) string {
	// AcceleratorType comes as full URL
	parts := strings.Split(fullPath, "/")
	if len(parts) > 0 {
		name := parts[len(parts)-1]
		// Convert "nvidia-tesla-a100" to "NVIDIA A100"
		name = strings.ReplaceAll(name, "nvidia-", "NVIDIA ")
		name = strings.ReplaceAll(name, "tesla-", "")
		name = strings.ReplaceAll(name, "-", " ")
		return strings.ToUpper(name)
	}
	return fullPath
}

func mapGCPStatus(status string) string {
	switch status {
	case "PROVISIONING", "STAGING":
		return "provisioning"
	case "RUNNING":
		return "running"
	case "STOPPING", "SUSPENDING":
		return "terminating"
	case "TERMINATED", "SUSPENDED":
		return "terminated"
	default:
		return strings.ToLower(status)
	}
}

func isGPUMachineType(name string) bool {
	return strings.HasPrefix(name, "a2-") ||
		strings.HasPrefix(name, "a3-") ||
		strings.HasPrefix(name, "g2-")
}

func parseGPUMachineType(name string) (int, string) {
	// A2 machine types: a2-highgpu-1g (1 A100), a2-highgpu-2g (2 A100), etc.
	// A2 Ultra: a2-ultragpu-1g (1 A100 80GB), etc.
	// A3 machine types: a3-highgpu-8g (8 H100), etc.
	// G2 machine types: g2-standard-4 (1 L4), g2-standard-8 (1 L4), etc.

	switch {
	case strings.HasPrefix(name, "a2-ultragpu-"):
		suffix := strings.TrimPrefix(name, "a2-ultragpu-")
		count := parseGPUCount(suffix)
		return count, "NVIDIA A100 80GB"

	case strings.HasPrefix(name, "a2-megagpu-"):
		suffix := strings.TrimPrefix(name, "a2-megagpu-")
		count := parseGPUCount(suffix)
		return count, "NVIDIA A100 80GB"

	case strings.HasPrefix(name, "a2-highgpu-"):
		suffix := strings.TrimPrefix(name, "a2-highgpu-")
		count := parseGPUCount(suffix)
		return count, "NVIDIA A100 40GB"

	case strings.HasPrefix(name, "a3-highgpu-"):
		suffix := strings.TrimPrefix(name, "a3-highgpu-")
		count := parseGPUCount(suffix)
		return count, "NVIDIA H100 80GB"

	case strings.HasPrefix(name, "a3-megagpu-"):
		suffix := strings.TrimPrefix(name, "a3-megagpu-")
		count := parseGPUCount(suffix)
		return count, "NVIDIA H100 80GB"

	case strings.HasPrefix(name, "g2-standard-"):
		// G2 instances have L4 GPUs
		// g2-standard-4, g2-standard-8, g2-standard-12 = 1 L4
		// g2-standard-16, g2-standard-24, g2-standard-32 = 1 L4
		// g2-standard-48 = 4 L4
		// g2-standard-96 = 8 L4
		suffix := strings.TrimPrefix(name, "g2-standard-")
		switch suffix {
		case "48":
			return 4, "NVIDIA L4"
		case "96":
			return 8, "NVIDIA L4"
		default:
			return 1, "NVIDIA L4"
		}

	default:
		return 0, ""
	}
}

func parseGPUCount(suffix string) int {
	// Parse "1g", "2g", "4g", "8g", "16g"
	suffix = strings.TrimSuffix(suffix, "g")
	switch suffix {
	case "1":
		return 1
	case "2":
		return 2
	case "4":
		return 4
	case "8":
		return 8
	case "16":
		return 16
	default:
		return 1
	}
}

// API types

type instanceRequest struct {
	Name              string             `json:"name"`
	MachineType       string             `json:"machineType"`
	Disks             []diskConfig       `json:"disks"`
	NetworkInterfaces []networkInterface `json:"networkInterfaces"`
	Labels            map[string]string  `json:"labels,omitempty"`
	Metadata          *metadata          `json:"metadata,omitempty"`
	Scheduling        *scheduling        `json:"scheduling,omitempty"`
	GuestAccelerators []accelerator      `json:"guestAccelerators,omitempty"`
}

type diskConfig struct {
	Boot             bool            `json:"boot"`
	AutoDelete       bool            `json:"autoDelete"`
	InitializeParams *diskInitParams `json:"initializeParams,omitempty"`
}

type diskInitParams struct {
	SourceImage string `json:"sourceImage"`
	DiskSizeGB  int    `json:"diskSizeGb"`
	DiskType    string `json:"diskType"`
}

type networkInterface struct {
	Network       string         `json:"network"`
	Subnetwork    string         `json:"subnetwork,omitempty"`
	AccessConfigs []accessConfig `json:"accessConfigs,omitempty"`
	NetworkIP     string         `json:"networkIP,omitempty"`
}

type accessConfig struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	NatIP string `json:"natIP,omitempty"`
}

type metadata struct {
	Items []metadataItem `json:"items"`
}

type metadataItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type scheduling struct {
	OnHostMaintenance string `json:"onHostMaintenance"`
	AutomaticRestart  bool   `json:"automaticRestart"`
}

type accelerator struct {
	AcceleratorType  string `json:"acceleratorType"`
	AcceleratorCount int    `json:"acceleratorCount"`
}

type operationResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	TargetLink string `json:"targetLink"`
}

type aggregatedListResponse struct {
	Items map[string]instancesScopedList `json:"items"`
}

type instancesScopedList struct {
	Instances []instanceData `json:"instances"`
}

type instanceData struct {
	ID                string             `json:"id"`
	Name              string             `json:"name"`
	Zone              string             `json:"zone"`
	MachineType       string             `json:"machineType"`
	Status            string             `json:"status"`
	NetworkInterfaces []networkInterface `json:"networkInterfaces"`
	Labels            map[string]string  `json:"labels"`
	GuestAccelerators []accelerator      `json:"guestAccelerators"`
}

type machineTypesResponse struct {
	Items []machineType `json:"items"`
}

type machineType struct {
	Name       string `json:"name"`
	GuestCPUs  int    `json:"guestCpus"`
	MemoryMB   int64  `json:"memoryMb"`
	MaximumPD  int    `json:"maximumPersistentDisks"`
	MaximumPDS int64  `json:"maximumPersistentDisksSizeGb"`
}

type errorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}
