// Package docker provides a container-based provider for integration testing.
// It spawns Docker containers with SSH enabled, allowing realistic bootstrap testing.
package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"github.com/NavarchProject/navarch/pkg/provider"
)

// Config configures the Docker provider.
type Config struct {
	// Image is the Docker image to use. Must have SSH server installed.
	// Default: "navarch-ssh-test" (built from embedded Dockerfile)
	Image string

	// SSHPublicKey is the public key to authorize for SSH access.
	// If empty, reads from SSHPublicKeyPath.
	SSHPublicKey string

	// SSHPublicKeyPath is the path to the SSH public key file.
	// Default: ~/.ssh/id_rsa.pub
	SSHPublicKeyPath string

	// Network is the Docker network to attach containers to.
	// Default: "bridge"
	Network string

	// Logger for provider operations.
	Logger *slog.Logger
}

// Provider manages Docker containers as fake GPU nodes.
type Provider struct {
	client       *client.Client
	config       Config
	mu           sync.RWMutex
	containers   map[string]*containerInfo
	nextID       atomic.Uint64
	logger       *slog.Logger
	sshPublicKey string
}

type containerInfo struct {
	containerID string
	node        *provider.Node
	sshPort     string
}

// New creates a new Docker provider.
func New(cfg Config) (*Provider, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	if cfg.Image == "" {
		cfg.Image = "navarch-ssh-test"
	}
	if cfg.Network == "" {
		cfg.Network = "bridge"
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Load SSH public key
	sshPubKey := cfg.SSHPublicKey
	if sshPubKey == "" {
		keyPath := cfg.SSHPublicKeyPath
		if keyPath == "" {
			home, _ := os.UserHomeDir()
			keyPath = filepath.Join(home, ".ssh", "id_rsa.pub")
		}
		if strings.HasPrefix(keyPath, "~/") {
			home, _ := os.UserHomeDir()
			keyPath = filepath.Join(home, keyPath[2:])
		}
		keyBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read SSH public key from %s: %w", keyPath, err)
		}
		sshPubKey = strings.TrimSpace(string(keyBytes))
	}

	return &Provider{
		client:       cli,
		config:       cfg,
		containers:   make(map[string]*containerInfo),
		logger:       logger,
		sshPublicKey: sshPubKey,
	}, nil
}

func (p *Provider) Name() string {
	return "docker"
}

func (p *Provider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	id := fmt.Sprintf("docker-%d", p.nextID.Add(1))

	// Create container with SSH server
	containerCfg := &container.Config{
		Image: p.config.Image,
		Env: []string{
			fmt.Sprintf("PUBLIC_KEY=%s", p.sshPublicKey),
			"PUID=1000",
			"PGID=1000",
		},
		ExposedPorts: nat.PortSet{
			"22/tcp": struct{}{},
		},
		Labels: map[string]string{
			"navarch.node.id":   id,
			"navarch.pool":      req.Labels["pool"],
			"navarch.provider":  "docker",
			"navarch.managed":   "true",
		},
	}

	hostCfg := &container.HostConfig{
		PortBindings: nat.PortMap{
			"22/tcp": []nat.PortBinding{
				{HostIP: "127.0.0.1", HostPort: ""}, // Random port
			},
		},
		AutoRemove: true,
	}

	networkCfg := &network.NetworkingConfig{}

	resp, err := p.client.ContainerCreate(ctx, containerCfg, hostCfg, networkCfg, nil, id)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	if err := p.client.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		p.client.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{Force: true})
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	// Get the assigned port
	inspect, err := p.client.ContainerInspect(ctx, resp.ID)
	if err != nil {
		timeout := 10 * time.Second
		p.client.ContainerStop(ctx, resp.ID, &timeout)
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	portBindings := inspect.NetworkSettings.Ports["22/tcp"]
	if len(portBindings) == 0 {
		timeout := 10 * time.Second
		p.client.ContainerStop(ctx, resp.ID, &timeout)
		return nil, fmt.Errorf("no SSH port binding found")
	}
	sshPort := portBindings[0].HostPort

	var sshPortInt int
	fmt.Sscanf(sshPort, "%d", &sshPortInt)

	node := &provider.Node{
		ID:           id,
		Provider:     "docker",
		Region:       req.Region,
		Zone:         req.Zone,
		InstanceType: req.InstanceType,
		Status:       "running",
		IPAddress:    "127.0.0.1",
		SSHPort:      sshPortInt,
		GPUCount:     0,
		GPUType:      "none (container)",
		Labels:       req.Labels,
	}

	p.mu.Lock()
	p.containers[id] = &containerInfo{
		containerID: resp.ID,
		node:        node,
		sshPort:     sshPort,
	}
	p.mu.Unlock()

	p.logger.Info("container provisioned",
		slog.String("node_id", id),
		slog.String("container_id", resp.ID[:12]),
		slog.String("ssh_port", sshPort),
	)

	return node, nil
}

func (p *Provider) Terminate(ctx context.Context, nodeID string) error {
	p.mu.Lock()
	info, ok := p.containers[nodeID]
	if ok {
		delete(p.containers, nodeID)
	}
	p.mu.Unlock()

	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	timeout := 10 * time.Second
	if err := p.client.ContainerStop(ctx, info.containerID, &timeout); err != nil {
		p.logger.Warn("failed to stop container",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()),
		)
	}

	p.logger.Info("container terminated",
		slog.String("node_id", nodeID),
		slog.String("container_id", info.containerID[:12]),
	)

	return nil
}

func (p *Provider) List(ctx context.Context) ([]*provider.Node, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	nodes := make([]*provider.Node, 0, len(p.containers))
	for _, info := range p.containers {
		nodes = append(nodes, info.node)
	}
	return nodes, nil
}

// TerminateAll stops all containers managed by this provider.
func (p *Provider) TerminateAll() {
	p.mu.Lock()
	containers := make(map[string]*containerInfo)
	for k, v := range p.containers {
		containers[k] = v
	}
	p.containers = make(map[string]*containerInfo)
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for nodeID, info := range containers {
		timeout := 5 * time.Second
		if err := p.client.ContainerStop(ctx, info.containerID, &timeout); err != nil {
			p.logger.Warn("failed to stop container during cleanup",
				slog.String("node_id", nodeID),
				slog.String("error", err.Error()),
			)
		}
	}
}

// EnsureImage builds or pulls the SSH test image if not present.
func (p *Provider) EnsureImage(ctx context.Context) error {
	// Check if image exists
	_, _, err := p.client.ImageInspectWithRaw(ctx, p.config.Image)
	if err == nil {
		return nil // Image exists
	}

	// Try to pull linuxserver/openssh-server as a fallback
	p.logger.Info("pulling SSH test image", slog.String("image", "lscr.io/linuxserver/openssh-server:latest"))
	reader, err := p.client.ImagePull(ctx, "lscr.io/linuxserver/openssh-server:latest", types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()
	io.Copy(io.Discard, reader) // Wait for pull to complete

	// Tag it as our image name
	if err := p.client.ImageTag(ctx, "lscr.io/linuxserver/openssh-server:latest", p.config.Image); err != nil {
		return fmt.Errorf("failed to tag image: %w", err)
	}

	return nil
}

// Close releases Docker client resources.
func (p *Provider) Close() error {
	return p.client.Close()
}
