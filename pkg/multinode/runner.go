// Package multinode provides multi-node distributed training support.
package multinode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	stdsync "sync"

	"connectrpc.com/connect"

	"github.com/NavarchProject/navarch/pkg/config"
	"github.com/NavarchProject/navarch/pkg/sync"
	pb "github.com/NavarchProject/navarch/proto"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

// Runner executes jobs across multiple nodes.
type Runner struct {
	client  protoconnect.ControlPlaneServiceClient
	sshUser string
	sshKey  string
}

// NewRunner creates a new multi-node runner.
func NewRunner(client protoconnect.ControlPlaneServiceClient, sshUser, sshKey string) *Runner {
	return &Runner{
		client:  client,
		sshUser: sshUser,
		sshKey:  sshKey,
	}
}

// NodeInfo holds information about a node in the cluster.
type NodeInfo struct {
	ID   string
	IP   string
	Rank int
}

// Run executes a multi-node job.
func (r *Runner) Run(ctx context.Context, job *config.JobConfig) error {
	nodeCount := job.GetNodeCount()
	fmt.Printf("Multi-node job: requesting %d nodes\n", nodeCount)

	// Select nodes
	nodes, err := r.selectNodes(ctx, job, nodeCount)
	if err != nil {
		return fmt.Errorf("failed to select nodes: %w", err)
	}

	fmt.Printf("Selected %d nodes:\n", len(nodes))
	for _, n := range nodes {
		fmt.Printf("  [%d] %s (%s)\n", n.Rank, n.ID, n.IP)
	}

	// Sync code to all nodes in parallel
	fmt.Println("\nSyncing code to all nodes...")
	if err := r.syncToAll(ctx, nodes, job); err != nil {
		return fmt.Errorf("failed to sync code: %w", err)
	}
	fmt.Println("Code sync complete")

	// Run setup on all nodes in parallel
	if job.Setup != "" {
		fmt.Println("\nRunning setup on all nodes...")
		if err := r.runOnAll(ctx, nodes, job.Setup, nil); err != nil {
			return fmt.Errorf("setup failed: %w", err)
		}
		fmt.Println("Setup complete")
	}

	// Build distributed training environment
	masterIP := nodes[0].IP
	masterPort := 29500 // Default PyTorch distributed port
	worldSize := nodeCount

	fmt.Printf("\nStarting distributed training:\n")
	fmt.Printf("  MASTER_ADDR=%s\n", masterIP)
	fmt.Printf("  MASTER_PORT=%d\n", masterPort)
	fmt.Printf("  WORLD_SIZE=%d\n", worldSize)
	fmt.Println("---")

	// Run the job on all nodes
	return r.runDistributed(ctx, nodes, job, masterIP, masterPort, worldSize)
}

func (r *Runner) selectNodes(ctx context.Context, job *config.JobConfig, count int) ([]NodeInfo, error) {
	req := connect.NewRequest(&pb.ListNodesRequest{})
	resp, err := r.client.ListNodes(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	var candidates []*pb.NodeInfo
	for _, node := range resp.Msg.Nodes {
		// Must be active
		if node.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
			continue
		}

		// Must have IP
		ip := getNodeIP(node)
		if ip == "" {
			continue
		}

		// Filter by pool
		if job.Pool != "" {
			if node.Metadata == nil {
				continue
			}
			poolLabel, ok := node.Metadata.Labels["pool"]
			if !ok || poolLabel != job.Pool {
				continue
			}
		}

		// Filter by GPU type
		if job.Resources.GPU != "" {
			gpuType, _ := job.Resources.ParseGPU()
			if gpuType != "" && gpuType != "any" {
				hasMatch := false
				for _, gpu := range node.Gpus {
					if strings.Contains(strings.ToUpper(gpu.Name), strings.ToUpper(gpuType)) {
						hasMatch = true
						break
					}
				}
				if !hasMatch {
					continue
				}
			}
		}

		candidates = append(candidates, node)
	}

	if len(candidates) < count {
		return nil, fmt.Errorf("not enough nodes: need %d, found %d", count, len(candidates))
	}

	// Take the first N nodes
	nodes := make([]NodeInfo, count)
	for i := 0; i < count; i++ {
		nodes[i] = NodeInfo{
			ID:   candidates[i].NodeId,
			IP:   getNodeIP(candidates[i]),
			Rank: i,
		}
	}

	return nodes, nil
}

func (r *Runner) syncToAll(ctx context.Context, nodes []NodeInfo, job *config.JobConfig) error {
	var wg stdsync.WaitGroup
	errChan := make(chan error, len(nodes))

	for _, node := range nodes {
		wg.Add(1)
		go func(n NodeInfo) {
			defer wg.Done()

			workdir := job.WorkDir
			if workdir == "" {
				workdir = "."
			}
			if !strings.HasPrefix(workdir, "/") {
				cwd, _ := os.Getwd()
				workdir = cwd + "/" + workdir
			}

			err := sync.SyncToNode(ctx, workdir, r.sshUser, n.IP, 22, r.sshKey, "/workspace")
			if err != nil {
				errChan <- fmt.Errorf("sync to node %s failed: %w", n.ID, err)
			}
		}(node)
	}

	wg.Wait()
	close(errChan)

	// Return first error if any
	for err := range errChan {
		return err
	}
	return nil
}

func (r *Runner) runOnAll(ctx context.Context, nodes []NodeInfo, command string, extraEnvs map[string]string) error {
	var wg stdsync.WaitGroup
	errChan := make(chan error, len(nodes))

	for _, node := range nodes {
		wg.Add(1)
		go func(n NodeInfo) {
			defer wg.Done()

			cmd := fmt.Sprintf("cd /workspace && %s", command)
			if err := r.runSSH(ctx, n.IP, cmd, extraEnvs); err != nil {
				errChan <- fmt.Errorf("command on node %s failed: %w", n.ID, err)
			}
		}(node)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		return err
	}
	return nil
}

func (r *Runner) runDistributed(ctx context.Context, nodes []NodeInfo, job *config.JobConfig, masterIP string, masterPort, worldSize int) error {
	var wg stdsync.WaitGroup
	errChan := make(chan error, len(nodes))

	for _, node := range nodes {
		wg.Add(1)
		go func(n NodeInfo) {
			defer wg.Done()

			// Build environment for this node
			envs := map[string]string{
				"MASTER_ADDR": masterIP,
				"MASTER_PORT": fmt.Sprintf("%d", masterPort),
				"WORLD_SIZE":  fmt.Sprintf("%d", worldSize),
				"NODE_RANK":   fmt.Sprintf("%d", n.Rank),
				"LOCAL_RANK":  "0", // Assuming single GPU per process for now
			}

			// Add user-specified envs
			for k, v := range job.Envs {
				envs[k] = v
			}

			cmd := fmt.Sprintf("cd /workspace && %s", job.Run)

			fmt.Printf("[Node %d] Starting: %s\n", n.Rank, n.ID)
			if err := r.runSSHStreaming(ctx, n, cmd, envs); err != nil {
				errChan <- fmt.Errorf("node %s (rank %d) failed: %w", n.ID, n.Rank, err)
			}
		}(node)
	}

	wg.Wait()
	close(errChan)

	// Collect all errors
	var errs []string
	for err := range errChan {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return fmt.Errorf("distributed run failed:\n  %s", strings.Join(errs, "\n  "))
	}

	return nil
}

func (r *Runner) runSSH(ctx context.Context, ip, command string, envs map[string]string) error {
	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
	}

	if r.sshKey != "" {
		sshArgs = append(sshArgs, "-i", r.sshKey)
	}

	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", r.sshUser, ip))

	// Prepend environment variables
	if len(envs) > 0 {
		var envParts []string
		for k, v := range envs {
			envParts = append(envParts, fmt.Sprintf("%s=%s", k, v))
		}
		command = strings.Join(envParts, " ") + " " + command
	}

	sshArgs = append(sshArgs, command)

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}

func (r *Runner) runSSHStreaming(ctx context.Context, node NodeInfo, command string, envs map[string]string) error {
	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-t",
	}

	if r.sshKey != "" {
		sshArgs = append(sshArgs, "-i", r.sshKey)
	}

	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", r.sshUser, node.IP))

	// Prepend environment variables
	if len(envs) > 0 {
		var envParts []string
		for k, v := range envs {
			envParts = append(envParts, fmt.Sprintf("export %s=%s;", k, v))
		}
		command = strings.Join(envParts, " ") + " " + command
	}

	sshArgs = append(sshArgs, command)

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	// Prefix output with node rank
	cmd.Stdout = &prefixWriter{prefix: fmt.Sprintf("[%d] ", node.Rank), w: os.Stdout}
	cmd.Stderr = &prefixWriter{prefix: fmt.Sprintf("[%d] ", node.Rank), w: os.Stderr}

	return cmd.Run()
}

func getNodeIP(node *pb.NodeInfo) string {
	if node.Metadata == nil {
		return ""
	}
	if node.Metadata.ExternalIp != "" {
		return node.Metadata.ExternalIp
	}
	return node.Metadata.InternalIp
}

// prefixWriter adds a prefix to each line of output.
type prefixWriter struct {
	prefix string
	w      *os.File
	buf    []byte
}

func (p *prefixWriter) Write(data []byte) (int, error) {
	p.buf = append(p.buf, data...)

	for {
		idx := strings.Index(string(p.buf), "\n")
		if idx < 0 {
			break
		}

		line := p.buf[:idx+1]
		p.buf = p.buf[idx+1:]

		fmt.Fprint(p.w, p.prefix+string(line))
	}

	return len(data), nil
}
