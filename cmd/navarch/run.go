package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	"github.com/NavarchProject/navarch/pkg/config"
	"github.com/NavarchProject/navarch/pkg/sync"
	pb "github.com/NavarchProject/navarch/proto"
)

var (
	runConfigFile   string
	runGPU          string
	runPool         string
	runCommand      string
	runSSHKey       string
	runSSHUser      string
	runNoSync       bool
	runWorkdir      string
	runDetach       bool
)

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [command]",
		Short: "Run a command on a GPU instance",
		Long: `Run a command on a GPU instance from a pool.

The command can be specified directly as arguments, or via a navarch.yaml config file.

Examples:
  # Run a command directly
  navarch run python train.py --epochs 100

  # Run from config file
  navarch run -f navarch.yaml

  # Run with specific GPU and pool
  navarch run --gpu H100 --pool training python train.py`,
		RunE: runTask,
	}

	cmd.Flags().StringVarP(&runConfigFile, "file", "f", "", "Config file (default: auto-detect navarch.yaml)")
	cmd.Flags().StringVar(&runGPU, "gpu", "", "GPU type requirement (e.g., H100, A100:4)")
	cmd.Flags().StringVar(&runPool, "pool", "", "Pool to use")
	cmd.Flags().StringVar(&runCommand, "command", "", "Command to run (alternative to positional args)")
	cmd.Flags().StringVar(&runSSHKey, "ssh-key", "", "SSH private key path")
	cmd.Flags().StringVar(&runSSHUser, "ssh-user", "ubuntu", "SSH user")
	cmd.Flags().BoolVar(&runNoSync, "no-sync", false, "Skip code sync")
	cmd.Flags().StringVar(&runWorkdir, "workdir", ".", "Local directory to sync")
	cmd.Flags().BoolVarP(&runDetach, "detach", "d", false, "Run in background (detached mode)")

	return cmd
}

func runTask(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Load job config
	jobCfg, err := loadJobConfig(args)
	if err != nil {
		return err
	}

	// Get a node to run on
	node, err := selectNode(ctx, jobCfg)
	if err != nil {
		return fmt.Errorf("failed to select node: %w", err)
	}

	fmt.Printf("Selected node: %s (%s)\n", node.NodeId, getNodeIP(node))

	// Sync code if not disabled
	if !runNoSync {
		fmt.Println("Syncing code...")
		if err := syncCode(ctx, node, jobCfg); err != nil {
			return fmt.Errorf("failed to sync code: %w", err)
		}
		fmt.Println("Code sync complete")
	}

	// Build and run the command
	runCommand := buildCommand(jobCfg)
	if runCommand == "" {
		return fmt.Errorf("no command specified")
	}

	fmt.Printf("Running: %s\n", runCommand)
	fmt.Println("---")

	if runDetach {
		return runDetached(ctx, node, jobCfg, runCommand)
	}

	return runAttached(ctx, node, jobCfg, runCommand)
}

func loadJobConfig(args []string) (*config.JobConfig, error) {
	// Try to load from config file
	if runConfigFile != "" {
		return config.LoadJob(runConfigFile)
	}

	// Try auto-detect
	configPath, err := config.FindJobConfig()
	if err == nil {
		job, err := config.LoadJob(configPath)
		if err != nil {
			return nil, err
		}
		// Override with CLI flags
		applyFlags(job, args)
		return job, nil
	}

	// Create job from CLI flags
	job := &config.JobConfig{
		WorkDir: runWorkdir,
	}

	if len(args) > 0 {
		job.Run = strings.Join(args, " ")
	} else if runCommand != "" {
		job.Run = runCommand
	}

	if runGPU != "" {
		job.Resources.GPU = runGPU
	}

	if runPool != "" {
		job.Pool = runPool
	}

	return job, nil
}

func applyFlags(job *config.JobConfig, args []string) {
	if runGPU != "" {
		job.Resources.GPU = runGPU
	}
	if runPool != "" {
		job.Pool = runPool
	}
	if runWorkdir != "." {
		job.WorkDir = runWorkdir
	}
	if len(args) > 0 {
		job.Run = strings.Join(args, " ")
	}
}

func selectNode(ctx context.Context, job *config.JobConfig) (*pb.NodeInfo, error) {
	client := newClient()

	req := connect.NewRequest(&pb.ListNodesRequest{})
	resp, err := client.ListNodes(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(resp.Msg.Nodes) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	// Filter by pool if specified
	var candidates []*pb.NodeInfo
	for _, node := range resp.Msg.Nodes {
		// Must be active
		if node.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
			continue
		}

		// Must have IP address
		ip := getNodeIP(node)
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
				// Check if node has matching GPU
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

	// Return first available node
	return candidates[0], nil
}

// getNodeIP returns the node's IP address (external preferred, internal fallback).
func getNodeIP(node *pb.NodeInfo) string {
	if node.Metadata == nil {
		return ""
	}
	if node.Metadata.ExternalIp != "" {
		return node.Metadata.ExternalIp
	}
	return node.Metadata.InternalIp
}

func syncCode(ctx context.Context, node *pb.NodeInfo, job *config.JobConfig) error {
	sshKey := runSSHKey
	if sshKey == "" {
		// Try default SSH key locations
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

	// Resolve workdir to absolute path
	if !strings.HasPrefix(workdir, "/") {
		cwd, _ := os.Getwd()
		workdir = cwd + "/" + workdir
	}

	port := 22
	ip := getNodeIP(node)

	return sync.SyncToNode(ctx, workdir, runSSHUser, ip, port, sshKey, "/workspace")
}

func buildCommand(job *config.JobConfig) string {
	var parts []string

	// Change to workspace directory
	parts = append(parts, "cd /workspace")

	// Setup commands
	if job.Setup != "" {
		parts = append(parts, job.Setup)
	}

	// Main run command
	if job.Run != "" {
		parts = append(parts, job.Run)
	}

	return strings.Join(parts, " && ")
}

func runAttached(ctx context.Context, node *pb.NodeInfo, job *config.JobConfig, command string) error {
	sshKey := runSSHKey
	if sshKey == "" {
		homeDir, _ := os.UserHomeDir()
		sshKey = homeDir + "/.ssh/id_ed25519"
	}

	port := 22
	ip := getNodeIP(node)

	// Build SSH command
	sshArgs := []string{
		"-i", sshKey,
		"-p", fmt.Sprintf("%d", port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-t", // Force pseudo-terminal for interactive output
		fmt.Sprintf("%s@%s", runSSHUser, ip),
		command,
	}

	// Set environment variables
	if len(job.Envs) > 0 {
		var envPrefix []string
		for k, v := range job.Envs {
			envPrefix = append(envPrefix, fmt.Sprintf("%s=%s", k, v))
		}
		// Prepend env vars to command
		sshArgs[len(sshArgs)-1] = strings.Join(envPrefix, " ") + " " + command
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func runDetached(ctx context.Context, node *pb.NodeInfo, job *config.JobConfig, command string) error {
	sshKey := runSSHKey
	if sshKey == "" {
		homeDir, _ := os.UserHomeDir()
		sshKey = homeDir + "/.ssh/id_ed25519"
	}

	port := 22
	ip := getNodeIP(node)

	// Wrap command in nohup and redirect output
	detachCmd := fmt.Sprintf("nohup bash -c '%s' > /workspace/output.log 2>&1 &", command)

	sshArgs := []string{
		"-i", sshKey,
		"-p", fmt.Sprintf("%d", port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", runSSHUser, ip),
		detachCmd,
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	fmt.Printf("\nTask running in background on %s\n", node.NodeId)
	fmt.Println("Output: /workspace/output.log")
	fmt.Printf("SSH: ssh -i %s %s@%s\n", sshKey, runSSHUser, ip)

	return nil
}
