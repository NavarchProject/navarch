package aws

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/NavarchProject/navarch/pkg/provider"
)

type mockEC2Client struct {
	runInstancesFn        func(ctx context.Context, params *ec2.RunInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	terminateInstancesFn  func(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
	describeInstancesFn   func(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	describeInstanceTypes func(ctx context.Context, params *ec2.DescribeInstanceTypesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error)
}

func (m *mockEC2Client) RunInstances(ctx context.Context, params *ec2.RunInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
	if m.runInstancesFn != nil {
		return m.runInstancesFn(ctx, params, optFns...)
	}
	return nil, nil
}

func (m *mockEC2Client) TerminateInstances(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	if m.terminateInstancesFn != nil {
		return m.terminateInstancesFn(ctx, params, optFns...)
	}
	return nil, nil
}

func (m *mockEC2Client) DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if m.describeInstancesFn != nil {
		return m.describeInstancesFn(ctx, params, optFns...)
	}
	return nil, nil
}

func (m *mockEC2Client) DescribeInstanceTypes(ctx context.Context, params *ec2.DescribeInstanceTypesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error) {
	if m.describeInstanceTypes != nil {
		return m.describeInstanceTypes(ctx, params, optFns...)
	}
	return nil, nil
}

func TestProvider_Name(t *testing.T) {
	p := NewWithClient(Config{Region: "us-east-1"}, &mockEC2Client{})
	if got := p.Name(); got != "aws" {
		t.Errorf("Name() = %q, want %q", got, "aws")
	}
}

func TestProvider_Provision(t *testing.T) {
	mock := &mockEC2Client{
		runInstancesFn: func(ctx context.Context, params *ec2.RunInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
			return &ec2.RunInstancesOutput{
				Instances: []types.Instance{
					{
						InstanceId:       aws.String("i-1234567890abcdef0"),
						InstanceType:     types.InstanceType("p5.48xlarge"),
						PrivateIpAddress: aws.String("10.0.0.1"),
						Placement: &types.Placement{
							AvailabilityZone: aws.String("us-east-1a"),
						},
						State: &types.InstanceState{
							Name: types.InstanceStateNamePending,
						},
					},
				},
			}, nil
		},
	}

	p := NewWithClient(Config{
		Region:     "us-east-1",
		DefaultAMI: "ami-12345678",
	}, mock)

	node, err := p.Provision(context.Background(), provider.ProvisionRequest{
		Name:         "test-node",
		InstanceType: "p5.48xlarge",
		Labels:       map[string]string{"env": "test"},
	})

	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	if node.ID != "i-1234567890abcdef0" {
		t.Errorf("node.ID = %q, want %q", node.ID, "i-1234567890abcdef0")
	}
	if node.Provider != "aws" {
		t.Errorf("node.Provider = %q, want %q", node.Provider, "aws")
	}
	if node.Region != "us-east-1" {
		t.Errorf("node.Region = %q, want %q", node.Region, "us-east-1")
	}
	if node.Zone != "us-east-1a" {
		t.Errorf("node.Zone = %q, want %q", node.Zone, "us-east-1a")
	}
	if node.GPUCount != 8 {
		t.Errorf("node.GPUCount = %d, want 8", node.GPUCount)
	}
	if node.GPUType != "NVIDIA H100 80GB" {
		t.Errorf("node.GPUType = %q, want %q", node.GPUType, "NVIDIA H100 80GB")
	}
	if node.Status != "provisioning" {
		t.Errorf("node.Status = %q, want %q", node.Status, "provisioning")
	}
}

func TestProvider_Provision_MissingInstanceType(t *testing.T) {
	p := NewWithClient(Config{Region: "us-east-1"}, &mockEC2Client{})

	_, err := p.Provision(context.Background(), provider.ProvisionRequest{
		Name: "test-node",
	})

	if err == nil {
		t.Error("Provision() expected error for missing instance type")
	}
}

func TestProvider_Terminate(t *testing.T) {
	terminateCalled := false
	mock := &mockEC2Client{
		terminateInstancesFn: func(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
			terminateCalled = true
			if len(params.InstanceIds) != 1 || params.InstanceIds[0] != "i-12345" {
				t.Errorf("unexpected instance ID: %v", params.InstanceIds)
			}
			return &ec2.TerminateInstancesOutput{}, nil
		},
	}

	p := NewWithClient(Config{Region: "us-east-1"}, mock)
	err := p.Terminate(context.Background(), "i-12345")

	if err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if !terminateCalled {
		t.Error("TerminateInstances was not called")
	}
}

func TestProvider_List(t *testing.T) {
	mock := &mockEC2Client{
		describeInstancesFn: func(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{
				Reservations: []types.Reservation{
					{
						Instances: []types.Instance{
							{
								InstanceId:       aws.String("i-gpu1"),
								InstanceType:     types.InstanceType("p5.48xlarge"),
								PrivateIpAddress: aws.String("10.0.0.1"),
								Placement: &types.Placement{
									AvailabilityZone: aws.String("us-east-1a"),
								},
								State: &types.InstanceState{
									Name: types.InstanceStateNameRunning,
								},
								Tags: []types.Tag{
									{Key: aws.String("navarch"), Value: aws.String("true")},
									{Key: aws.String("env"), Value: aws.String("prod")},
								},
							},
							{
								InstanceId:       aws.String("i-gpu2"),
								InstanceType:     types.InstanceType("g5.xlarge"),
								PublicIpAddress:  aws.String("54.1.2.3"),
								PrivateIpAddress: aws.String("10.0.0.2"),
								Placement: &types.Placement{
									AvailabilityZone: aws.String("us-east-1b"),
								},
								State: &types.InstanceState{
									Name: types.InstanceStateNameRunning,
								},
								Tags: []types.Tag{
									{Key: aws.String("navarch"), Value: aws.String("true")},
								},
							},
						},
					},
				},
			}, nil
		},
	}

	p := NewWithClient(Config{Region: "us-east-1"}, mock)
	nodes, err := p.List(context.Background())

	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("List() returned %d nodes, want 2", len(nodes))
	}

	// First node: P5
	if nodes[0].ID != "i-gpu1" {
		t.Errorf("nodes[0].ID = %q, want %q", nodes[0].ID, "i-gpu1")
	}
	if nodes[0].GPUCount != 8 {
		t.Errorf("nodes[0].GPUCount = %d, want 8", nodes[0].GPUCount)
	}
	if nodes[0].GPUType != "NVIDIA H100 80GB" {
		t.Errorf("nodes[0].GPUType = %q, want %q", nodes[0].GPUType, "NVIDIA H100 80GB")
	}
	if nodes[0].IPAddress != "10.0.0.1" {
		t.Errorf("nodes[0].IPAddress = %q, want %q", nodes[0].IPAddress, "10.0.0.1")
	}
	if nodes[0].Labels["env"] != "prod" {
		t.Errorf("nodes[0].Labels[env] = %q, want %q", nodes[0].Labels["env"], "prod")
	}

	// Second node: G5 with public IP
	if nodes[1].ID != "i-gpu2" {
		t.Errorf("nodes[1].ID = %q, want %q", nodes[1].ID, "i-gpu2")
	}
	if nodes[1].GPUCount != 1 {
		t.Errorf("nodes[1].GPUCount = %d, want 1", nodes[1].GPUCount)
	}
	if nodes[1].GPUType != "NVIDIA A10G" {
		t.Errorf("nodes[1].GPUType = %q, want %q", nodes[1].GPUType, "NVIDIA A10G")
	}
	if nodes[1].IPAddress != "54.1.2.3" {
		t.Errorf("nodes[1].IPAddress = %q, want %q (should prefer public IP)", nodes[1].IPAddress, "54.1.2.3")
	}
}

func TestParseGPUInstanceType(t *testing.T) {
	tests := []struct {
		instanceType string
		wantCount    int
		wantType     string
	}{
		{"p4d.24xlarge", 8, "NVIDIA A100 40GB"},
		{"p4de.24xlarge", 8, "NVIDIA A100 80GB"},
		{"p5.48xlarge", 8, "NVIDIA H100 80GB"},
		{"g5.xlarge", 1, "NVIDIA A10G"},
		{"g5.2xlarge", 1, "NVIDIA A10G"},
		{"g5.12xlarge", 4, "NVIDIA A10G"},
		{"g5.48xlarge", 8, "NVIDIA A10G"},
		{"g6.xlarge", 1, "NVIDIA L4"},
		{"g6.24xlarge", 4, "NVIDIA L4"},
		{"g6.48xlarge", 8, "NVIDIA L4"},
		{"p3.2xlarge", 1, "NVIDIA V100"},
		{"p3.8xlarge", 4, "NVIDIA V100"},
		{"p3.16xlarge", 8, "NVIDIA V100"},
		{"p3dn.24xlarge", 8, "NVIDIA V100"},
		{"t3.micro", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.instanceType, func(t *testing.T) {
			count, gpuType := parseGPUInstanceType(tt.instanceType)
			if count != tt.wantCount {
				t.Errorf("parseGPUInstanceType(%q) count = %d, want %d", tt.instanceType, count, tt.wantCount)
			}
			if gpuType != tt.wantType {
				t.Errorf("parseGPUInstanceType(%q) type = %q, want %q", tt.instanceType, gpuType, tt.wantType)
			}
		})
	}
}

func TestIsGPUInstanceType(t *testing.T) {
	tests := []struct {
		instanceType string
		want         bool
	}{
		{"p4d.24xlarge", true},
		{"p4de.24xlarge", true},
		{"p5.48xlarge", true},
		{"g5.xlarge", true},
		{"g6.xlarge", true},
		{"gr6.xlarge", true},
		{"p3.2xlarge", true},
		{"p3dn.24xlarge", true},
		{"t3.micro", false},
		{"m5.large", false},
		{"c5.xlarge", false},
	}

	for _, tt := range tests {
		t.Run(tt.instanceType, func(t *testing.T) {
			if got := isGPUInstanceType(tt.instanceType); got != tt.want {
				t.Errorf("isGPUInstanceType(%q) = %v, want %v", tt.instanceType, got, tt.want)
			}
		})
	}
}

func TestMapEC2State(t *testing.T) {
	tests := []struct {
		state *types.InstanceState
		want  string
	}{
		{&types.InstanceState{Name: types.InstanceStateNamePending}, "provisioning"},
		{&types.InstanceState{Name: types.InstanceStateNameRunning}, "running"},
		{&types.InstanceState{Name: types.InstanceStateNameStopping}, "terminating"},
		{&types.InstanceState{Name: types.InstanceStateNameShuttingDown}, "terminating"},
		{&types.InstanceState{Name: types.InstanceStateNameStopped}, "terminated"},
		{&types.InstanceState{Name: types.InstanceStateNameTerminated}, "terminated"},
		{nil, "unknown"},
	}

	for _, tt := range tests {
		name := "nil"
		if tt.state != nil {
			name = string(tt.state.Name)
		}
		t.Run(name, func(t *testing.T) {
			if got := mapEC2State(tt.state); got != tt.want {
				t.Errorf("mapEC2State() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractLabels(t *testing.T) {
	tags := []types.Tag{
		{Key: aws.String("navarch"), Value: aws.String("true")},
		{Key: aws.String("Name"), Value: aws.String("my-instance")},
		{Key: aws.String("aws:autoscaling:groupName"), Value: aws.String("group")},
		{Key: aws.String("env"), Value: aws.String("prod")},
		{Key: aws.String("team"), Value: aws.String("ml")},
	}

	labels := extractLabels(tags)

	if len(labels) != 2 {
		t.Errorf("extractLabels() returned %d labels, want 2", len(labels))
	}
	if labels["env"] != "prod" {
		t.Errorf("labels[env] = %q, want %q", labels["env"], "prod")
	}
	if labels["team"] != "ml" {
		t.Errorf("labels[team] = %q, want %q", labels["team"], "ml")
	}
	if _, ok := labels["navarch"]; ok {
		t.Error("labels should not contain navarch system tag")
	}
	if _, ok := labels["Name"]; ok {
		t.Error("labels should not contain Name system tag")
	}
}

func TestProvider_ImplementsInterface(t *testing.T) {
	var _ provider.Provider = (*Provider)(nil)
	var _ provider.InstanceTypeLister = (*Provider)(nil)
}
