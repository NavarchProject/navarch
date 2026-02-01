package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/NavarchProject/navarch/pkg/provider"
)

const (
	defaultTimeout = 60 * time.Second
	tagKeyNavarch  = "navarch"
	tagKeyName     = "Name"
)

// Provider implements the provider.Provider interface for Amazon Web Services.
type Provider struct {
	region         string
	client         EC2Client
	defaultAMI     string
	defaultSubnet  string
	securityGroups []string
	keyPairName    string
}

// EC2Client abstracts the EC2 API for testing.
type EC2Client interface {
	RunInstances(ctx context.Context, params *ec2.RunInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	TerminateInstances(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
	DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeInstanceTypes(ctx context.Context, params *ec2.DescribeInstanceTypesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error)
}

// Config holds configuration for the AWS provider.
type Config struct {
	Region          string   // AWS region (e.g., "us-east-1")
	DefaultAMI      string   // Default AMI ID (Deep Learning AMI recommended)
	DefaultSubnet   string   // Default subnet ID
	SecurityGroups  []string // Security group IDs
	KeyPairName     string   // SSH key pair name
	AccessKeyID     string   // Optional, uses default credentials if empty
	SecretAccessKey string   // Optional, uses default credentials if empty
}

// New creates a new AWS provider using the default credential chain.
func New(cfg Config) (*Provider, error) {
	if cfg.Region == "" {
		return nil, fmt.Errorf("region is required")
	}

	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := ec2.NewFromConfig(awsCfg)

	return &Provider{
		region:         cfg.Region,
		client:         client,
		defaultAMI:     cfg.DefaultAMI,
		defaultSubnet:  cfg.DefaultSubnet,
		securityGroups: cfg.SecurityGroups,
		keyPairName:    cfg.KeyPairName,
	}, nil
}

// NewWithClient creates a new AWS provider with a custom EC2 client (for testing).
func NewWithClient(cfg Config, client EC2Client) *Provider {
	return &Provider{
		region:         cfg.Region,
		client:         client,
		defaultAMI:     cfg.DefaultAMI,
		defaultSubnet:  cfg.DefaultSubnet,
		securityGroups: cfg.SecurityGroups,
		keyPairName:    cfg.KeyPairName,
	}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "aws"
}

// Provision creates a new GPU instance on AWS EC2.
func (p *Provider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	if req.InstanceType == "" {
		return nil, fmt.Errorf("instance type is required")
	}

	ami := p.defaultAMI
	if ami == "" {
		ami = p.getDefaultAMI()
	}

	input := &ec2.RunInstancesInput{
		ImageId:      aws.String(ami),
		InstanceType: types.InstanceType(req.InstanceType),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeInstance,
				Tags:         p.buildTags(req.Name, req.Labels),
			},
		},
	}

	// Add subnet if specified
	subnet := p.defaultSubnet
	if req.Zone != "" {
		// Zone can be used to override subnet
		subnet = req.Zone
	}
	if subnet != "" {
		input.SubnetId = aws.String(subnet)
	}

	// Add security groups
	if len(p.securityGroups) > 0 {
		input.SecurityGroupIds = p.securityGroups
	}

	// Add key pair
	keyPair := p.keyPairName
	if len(req.SSHKeyNames) > 0 {
		keyPair = req.SSHKeyNames[0]
	}
	if keyPair != "" {
		input.KeyName = aws.String(keyPair)
	}

	// Add user data (startup script)
	if req.UserData != "" {
		input.UserData = aws.String(req.UserData)
	}

	result, err := p.client.RunInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to run instance: %w", err)
	}

	if len(result.Instances) == 0 {
		return nil, fmt.Errorf("no instances created")
	}

	instance := result.Instances[0]
	gpuCount, gpuType := parseGPUInstanceType(req.InstanceType)

	node := &provider.Node{
		ID:           aws.ToString(instance.InstanceId),
		Provider:     "aws",
		Region:       p.region,
		Zone:         aws.ToString(instance.Placement.AvailabilityZone),
		InstanceType: req.InstanceType,
		Status:       mapEC2State(instance.State),
		GPUCount:     gpuCount,
		GPUType:      gpuType,
		Labels:       req.Labels,
	}

	// Get IP address if available
	if instance.PublicIpAddress != nil {
		node.IPAddress = aws.ToString(instance.PublicIpAddress)
	} else if instance.PrivateIpAddress != nil {
		node.IPAddress = aws.ToString(instance.PrivateIpAddress)
	}

	return node, nil
}

// Terminate terminates an EC2 instance.
func (p *Provider) Terminate(ctx context.Context, nodeID string) error {
	input := &ec2.TerminateInstancesInput{
		InstanceIds: []string{nodeID},
	}

	_, err := p.client.TerminateInstances(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to terminate instance %s: %w", nodeID, err)
	}

	return nil
}

// List returns all navarch-managed GPU instances.
func (p *Provider) List(ctx context.Context) ([]*provider.Node, error) {
	input := &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("tag-key"),
				Values: []string{tagKeyNavarch},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"pending", "running", "stopping", "stopped"},
			},
		},
	}

	result, err := p.client.DescribeInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe instances: %w", err)
	}

	var nodes []*provider.Node
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			instanceType := string(instance.InstanceType)
			if !isGPUInstanceType(instanceType) {
				continue
			}

			gpuCount, gpuType := parseGPUInstanceType(instanceType)
			labels := extractLabels(instance.Tags)

			node := &provider.Node{
				ID:           aws.ToString(instance.InstanceId),
				Provider:     "aws",
				Region:       p.region,
				Zone:         aws.ToString(instance.Placement.AvailabilityZone),
				InstanceType: instanceType,
				Status:       mapEC2State(instance.State),
				GPUCount:     gpuCount,
				GPUType:      gpuType,
				Labels:       labels,
			}

			if instance.PublicIpAddress != nil {
				node.IPAddress = aws.ToString(instance.PublicIpAddress)
			} else if instance.PrivateIpAddress != nil {
				node.IPAddress = aws.ToString(instance.PrivateIpAddress)
			}

			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}

// ListInstanceTypes returns available GPU instance types.
func (p *Provider) ListInstanceTypes(ctx context.Context) ([]provider.InstanceType, error) {
	gpuFamilies := []string{"p4d", "p4de", "p5", "g5", "g6", "gr6"}

	var instanceTypes []provider.InstanceType

	for _, family := range gpuFamilies {
		input := &ec2.DescribeInstanceTypesInput{
			Filters: []types.Filter{
				{
					Name:   aws.String("instance-type"),
					Values: []string{family + ".*"},
				},
			},
		}

		result, err := p.client.DescribeInstanceTypes(ctx, input)
		if err != nil {
			continue // Skip families that aren't available
		}

		for _, it := range result.InstanceTypes {
			name := string(it.InstanceType)
			gpuCount, gpuType := parseGPUInstanceType(name)

			memoryMB := int64(0)
			if it.MemoryInfo != nil && it.MemoryInfo.SizeInMiB != nil {
				memoryMB = *it.MemoryInfo.SizeInMiB
			}

			vcpus := 0
			if it.VCpuInfo != nil && it.VCpuInfo.DefaultVCpus != nil {
				vcpus = int(*it.VCpuInfo.DefaultVCpus)
			}

			instanceTypes = append(instanceTypes, provider.InstanceType{
				Name:      name,
				GPUCount:  gpuCount,
				GPUType:   gpuType,
				MemoryGB:  int(memoryMB / 1024),
				VCPUs:     vcpus,
				Regions:   []string{p.region},
				Available: true,
			})
		}
	}

	return instanceTypes, nil
}

// buildTags creates EC2 tags from the instance name and labels.
func (p *Provider) buildTags(name string, labels map[string]string) []types.Tag {
	tags := []types.Tag{
		{
			Key:   aws.String(tagKeyNavarch),
			Value: aws.String("true"),
		},
	}

	if name != "" {
		tags = append(tags, types.Tag{
			Key:   aws.String(tagKeyName),
			Value: aws.String(name),
		})
	}

	for k, v := range labels {
		tags = append(tags, types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	return tags
}

// getDefaultAMI returns the default Deep Learning AMI for the region.
func (p *Provider) getDefaultAMI() string {
	// AWS Deep Learning AMI (Ubuntu 22.04) with CUDA
	// These AMI IDs vary by region; this is a placeholder
	// In production, you'd look up the latest AMI dynamically
	return "ami-0" // Will fail if not set - user must provide AMI
}

// mapEC2State converts EC2 instance state to provider status.
func mapEC2State(state *types.InstanceState) string {
	if state == nil {
		return "unknown"
	}
	switch state.Name {
	case types.InstanceStateNamePending:
		return "provisioning"
	case types.InstanceStateNameRunning:
		return "running"
	case types.InstanceStateNameStopping, types.InstanceStateNameShuttingDown:
		return "terminating"
	case types.InstanceStateNameStopped, types.InstanceStateNameTerminated:
		return "terminated"
	default:
		return "unknown"
	}
}

// extractLabels extracts user labels from EC2 tags, excluding system tags.
func extractLabels(tags []types.Tag) map[string]string {
	labels := make(map[string]string)
	for _, tag := range tags {
		key := aws.ToString(tag.Key)
		if key == tagKeyNavarch || key == tagKeyName || strings.HasPrefix(key, "aws:") {
			continue
		}
		labels[key] = aws.ToString(tag.Value)
	}
	return labels
}

// isGPUInstanceType checks if an instance type has GPUs.
func isGPUInstanceType(instanceType string) bool {
	gpuPrefixes := []string{"p4d.", "p4de.", "p5.", "g5.", "g6.", "gr6.", "p3.", "p3dn."}
	for _, prefix := range gpuPrefixes {
		if strings.HasPrefix(instanceType, prefix) {
			return true
		}
	}
	return false
}

// parseGPUInstanceType extracts GPU count and type from instance type name.
func parseGPUInstanceType(instanceType string) (int, string) {
	// P4d instances: 8x A100 40GB
	if strings.HasPrefix(instanceType, "p4d.") {
		return 8, "NVIDIA A100 40GB"
	}

	// P4de instances: 8x A100 80GB
	if strings.HasPrefix(instanceType, "p4de.") {
		return 8, "NVIDIA A100 80GB"
	}

	// P5 instances: 8x H100 80GB
	if strings.HasPrefix(instanceType, "p5.") {
		return 8, "NVIDIA H100 80GB"
	}

	// G5 instances: A10G GPUs
	if strings.HasPrefix(instanceType, "g5.") {
		return parseG5GPUCount(instanceType), "NVIDIA A10G"
	}

	// G6 instances: L4 GPUs
	if strings.HasPrefix(instanceType, "g6.") {
		return parseG6GPUCount(instanceType), "NVIDIA L4"
	}

	// Gr6 instances: L4 GPUs (graphics optimized)
	if strings.HasPrefix(instanceType, "gr6.") {
		return parseG6GPUCount(instanceType), "NVIDIA L4"
	}

	// P3 instances: V100 GPUs (legacy)
	if strings.HasPrefix(instanceType, "p3.") || strings.HasPrefix(instanceType, "p3dn.") {
		return parseP3GPUCount(instanceType), "NVIDIA V100"
	}

	return 0, ""
}

// parseG5GPUCount extracts GPU count from G5 instance type.
func parseG5GPUCount(instanceType string) int {
	// g5.xlarge, g5.2xlarge, g5.4xlarge, g5.8xlarge, g5.16xlarge = 1 GPU
	// g5.12xlarge = 4 GPUs
	// g5.24xlarge = 4 GPUs
	// g5.48xlarge = 8 GPUs
	switch {
	case strings.HasSuffix(instanceType, ".48xlarge"):
		return 8
	case strings.HasSuffix(instanceType, ".24xlarge"), strings.HasSuffix(instanceType, ".12xlarge"):
		return 4
	default:
		return 1
	}
}

// parseG6GPUCount extracts GPU count from G6 instance type.
func parseG6GPUCount(instanceType string) int {
	// g6.xlarge through g6.16xlarge = 1 GPU
	// g6.24xlarge = 4 GPUs
	// g6.48xlarge = 8 GPUs
	switch {
	case strings.HasSuffix(instanceType, ".48xlarge"):
		return 8
	case strings.HasSuffix(instanceType, ".24xlarge"):
		return 4
	default:
		return 1
	}
}

// parseP3GPUCount extracts GPU count from P3 instance type.
func parseP3GPUCount(instanceType string) int {
	// p3.2xlarge = 1 V100
	// p3.8xlarge = 4 V100
	// p3.16xlarge = 8 V100
	// p3dn.24xlarge = 8 V100
	switch {
	case strings.HasSuffix(instanceType, ".16xlarge"), strings.HasSuffix(instanceType, ".24xlarge"):
		return 8
	case strings.HasSuffix(instanceType, ".8xlarge"):
		return 4
	default:
		return 1
	}
}
