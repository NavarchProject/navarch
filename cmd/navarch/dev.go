package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	"github.com/NavarchProject/navarch/pkg/config"
	"github.com/NavarchProject/navarch/pkg/sync"
	pb "github.com/NavarchProject/navarch/proto"
)

var (
	devConfigFile string
	devGPU        string
	devPool       string
	devSSHKey     string
	devSSHUser    string
	devNoSync     bool
	devWorkdir    string
	devPorts      []int
)

func devCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Start an interactive development session on a GPU instance",
		Long: `Start an interactive SSH session on a GPU instance.

Code is synced to /workspace on the remote instance. Port forwarding can be
configured for Jupyter, TensorBoard, and other services.

Examples:
  # Start dev session with auto-detected config
  navarch dev

  # Start with port forwarding for Jupyter and TensorBoard
  navarch dev --port 8888 --port 6006

  # Start on specific pool with GPU
  navarch dev --pool training --gpu H100`,
		RunE: runDev,
	}

	cmd.Flags().StringVarP(&devConfigFile, "file", "f", "", "Config file (default: auto-detect navarch.yaml)")
	cmd.Flags().StringVar(&devGPU, "gpu", "", "GPU type requirement (e.g., H100, A100:4)")
	cmd.Flags().StringVar(&devPool, "pool", "", "Pool to use")
	cmd.Flags().StringVar(&devSSHKey, "ssh-key", "", "SSH private key path")
	cmd.Flags().StringVar(&devSSHUser, "ssh-user", "ubuntu", "SSH user")
	cmd.Flags().BoolVar(&devNoSync, "no-sync", false, "Skip code sync")
	cmd.Flags().StringVar(&devWorkdir, "workdir", ".", "Local directory to sync")
	cmd.Flags().IntSliceVarP(&devPorts, "port", "p", nil, "Ports to forward (can be specified multiple times)")

	return cmd
}

func runDev(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nClosing dev session...")
		cancel()
	}()

	// Load job config
	jobCfg, err := loadDevConfig()
	if err != nil {
		return err
	}

	// Get a node to develop on
	node, err := selectDevNode(ctx, jobCfg)
	if err != nil {
		return fmt.Errorf("failed to select node: %w", err)
	}

	ip := getDevNodeIP(node)
	fmt.Printf("Selected node: %s (%s)\n", node.NodeId, ip)

	// Sync code if not disabled
	if !devNoSync {
		fmt.Println("Syncing code...")
		if err := syncDevCode(ctx, node, jobCfg); err != nil {
			return fmt.Errorf("failed to sync code: %w", err)
		}
		fmt.Println("Code sync complete")
	}

	// Run setup commands if any
	if jobCfg.Setup != "" {
		fmt.Println("Running setup...")
		if err := runSetup(ctx, node, jobCfg); err != nil {
			return fmt.Errorf("setup failed: %w", err)
		}
		fmt.Println("Setup complete")
	}

	// Print connection info
	fmt.Println("---")
	fmt.Printf("Working directory: /workspace\n")
	if len(devPorts) > 0 || len(jobCfg.Ports) > 0 {
		fmt.Printf("Port forwarding:\n")
		for _, p := range append(devPorts, jobCfg.Ports...) {
			fmt.Printf("  localhost:%d -> %s:%d\n", p, ip, p)
		}
	}
	fmt.Println("---")
	fmt.Println("Starting interactive session (Ctrl+D or 'exit' to close)")
	fmt.Println()

	// Start interactive SSH session
	return startInteractiveSSH(ctx, node, jobCfg)
}

func loadDevConfig() (*config.JobConfig, error) {
	// Try to load from config file
	if devConfigFile != "" {
		return config.LoadJob(devConfigFile)
	}

	// Try auto-detect
	configPath, err := config.FindJobConfig()
	if err == nil {
		job, err := config.LoadJob(configPath)
		if err != nil {
			return nil, err
		}
		applyDevFlags(job)
		return job, nil
	}

	// Create job from CLI flags
	job := &config.JobConfig{
		WorkDir: devWorkdir,
		Ports:   devPorts,
	}

	if devGPU != "" {
		job.Resources.GPU = devGPU
	}

	if devPool != "" {
		job.Pool = devPool
	}

	return job, nil
}

func applyDevFlags(job *config.JobConfig) {
	if devGPU != "" {
		job.Resources.GPU = devGPU
	}
	if devPool != "" {
		job.Pool = devPool
	}
	if devWorkdir != "." {
		job.WorkDir = devWorkdir
	}
	if len(devPorts) > 0 {
		job.Ports = devPorts
	}
}

func selectDevNode(ctx context.Context, job *config.JobConfig) (*pb.NodeInfo, error) {
	client := newClient()

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req := connect.NewRequest(&pb.ListNodesRequest{})
	resp, err := client.ListNodes(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(resp.Msg.Nodes) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	// Filter nodes
	var candidates []*pb.NodeInfo
	for _, node := range resp.Msg.Nodes {
		// Must be active
		if node.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
			continue
		}

		// Must have IP address
		ip := getDevNodeIP(node)
		if ip == "" {
			continue
		}

		// Filter by pool if specified
		if job.Pool != "" {
			if node.Metadata == nil {
				continue
			}
			poolLabel, ok := node.Metadata.Labels["pool"]
			if !ok || poolLabel != job.Pool {
				continue
			}
		}

		// Filter by GPU type if specified
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

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no suitable nodes found (pool=%q, gpu=%q)", job.Pool, job.Resources.GPU)
	}

	return candidates[0], nil
}

func getDevNodeIP(node *pb.NodeInfo) string {
	if node.Metadata == nil {
		return ""
	}
	if node.Metadata.ExternalIp != "" {
		return node.Metadata.ExternalIp
	}
	return node.Metadata.InternalIp
}

func syncDevCode(ctx context.Context, node *pb.NodeInfo, job *config.JobConfig) error {
	sshKey := devSSHKey
	if sshKey == "" {
		homeDir, _ := os.UserHomeDir()
		candidates := []string{
			homeDir + "/.ssh/id_ed25519",
			homeDir + "/.ssh/id_rsa",
		}
		for _, path := range candidates {
			if _, err := os.Stat(path); err == nil {
				sshKey = path
				break
			}
		}
	}

	if sshKey == "" {
		return fmt.Errorf("no SSH key found, specify with --ssh-key")
	}

	workdir := job.WorkDir
	if workdir == "" {
		workdir = "."
	}

	if !strings.HasPrefix(workdir, "/") {
		cwd, _ := os.Getwd()
		workdir = cwd + "/" + workdir
	}

	ip := getDevNodeIP(node)

	return sync.SyncToNode(ctx, workdir, devSSHUser, ip, 22, sshKey, "/workspace")
}

func runSetup(ctx context.Context, node *pb.NodeInfo, job *config.JobConfig) error {
	sshKey := devSSHKey
	if sshKey == "" {
		homeDir, _ := os.UserHomeDir()
		sshKey = homeDir + "/.ssh/id_ed25519"
	}

	ip := getDevNodeIP(node)
	setupCmd := fmt.Sprintf("cd /workspace && %s", job.Setup)

	sshArgs := []string{
		"-i", sshKey,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", devSSHUser, ip),
		setupCmd,
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func startInteractiveSSH(ctx context.Context, node *pb.NodeInfo, job *config.JobConfig) error {
	sshKey := devSSHKey
	if sshKey == "" {
		homeDir, _ := os.UserHomeDir()
		sshKey = homeDir + "/.ssh/id_ed25519"
	}

	ip := getDevNodeIP(node)

	sshArgs := []string{
		"-i", sshKey,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-t", // Force pseudo-terminal
	}

	// Add port forwarding
	allPorts := append(devPorts, job.Ports...)
	for _, p := range allPorts {
		sshArgs = append(sshArgs, "-L", fmt.Sprintf("%d:localhost:%d", p, p))
	}

	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", devSSHUser, ip))

	// Start in workspace directory
	sshArgs = append(sshArgs, "cd /workspace && exec $SHELL -l")

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Wait a moment for context cancellation to not immediately kill SSH
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Give SSH a moment to clean up
		time.Sleep(100 * time.Millisecond)
		return nil
	}
}
