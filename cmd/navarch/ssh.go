package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	pb "github.com/NavarchProject/navarch/proto"
)

var (
	sshUser    string
	sshKey     string
	sshCommand string
	sshPorts   []int
)

func sshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssh <node-id>",
		Short: "SSH into a GPU node",
		Long: `Open an SSH session to a GPU node.

The node ID can be partial - it will match the first node that starts with the
given prefix.

Examples:
  # SSH to a node
  navarch ssh node-abc123

  # SSH with partial ID
  navarch ssh node-abc

  # Run a command
  navarch ssh node-abc -- nvidia-smi

  # Forward ports
  navarch ssh node-abc -p 8888 -p 6006`,
		Args: cobra.MinimumNArgs(1),
		RunE: runSSH,
	}

	cmd.Flags().StringVarP(&sshUser, "user", "u", "ubuntu", "SSH user")
	cmd.Flags().StringVarP(&sshKey, "identity", "i", "", "SSH private key path")
	cmd.Flags().StringVarP(&sshCommand, "command", "c", "", "Command to run instead of shell")
	cmd.Flags().IntSliceVarP(&sshPorts, "port", "p", nil, "Ports to forward (can be specified multiple times)")

	return cmd
}

func runSSH(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	nodeIDPrefix := args[0]

	// Find the node
	node, err := findNode(ctx, nodeIDPrefix)
	if err != nil {
		return err
	}

	ip := getSSHNodeIP(node)
	if ip == "" {
		return fmt.Errorf("node %s has no IP address", node.NodeId)
	}

	fmt.Printf("Connecting to %s (%s)...\n", node.NodeId, ip)

	// Find SSH key if not specified
	keyPath := sshKey
	if keyPath == "" {
		homeDir, _ := os.UserHomeDir()
		candidates := []string{
			homeDir + "/.ssh/id_ed25519",
			homeDir + "/.ssh/id_rsa",
		}
		for _, path := range candidates {
			if _, err := os.Stat(path); err == nil {
				keyPath = path
				break
			}
		}
	}

	// Build SSH args
	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
	}

	if keyPath != "" {
		sshArgs = append(sshArgs, "-i", keyPath)
	}

	// Add port forwarding
	for _, p := range sshPorts {
		sshArgs = append(sshArgs, "-L", fmt.Sprintf("%d:localhost:%d", p, p))
	}

	// Add user@host
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", sshUser, ip))

	// Add command if specified (from flag or remaining args)
	remoteCmd := sshCommand
	if remoteCmd == "" && len(args) > 1 {
		// Check if there's a -- separator
		for i, arg := range args {
			if arg == "--" {
				remoteCmd = strings.Join(args[i+1:], " ")
				break
			}
		}
	}

	if remoteCmd != "" {
		sshArgs = append(sshArgs, remoteCmd)
	} else {
		// Interactive session needs -t
		sshArgs = append([]string{"-t"}, sshArgs...)
	}

	sshExec := exec.Command("ssh", sshArgs...)
	sshExec.Stdin = os.Stdin
	sshExec.Stdout = os.Stdout
	sshExec.Stderr = os.Stderr

	return sshExec.Run()
}

func findNode(ctx context.Context, idPrefix string) (*pb.NodeInfo, error) {
	client := newClient()

	req := connect.NewRequest(&pb.ListNodesRequest{})
	resp, err := client.ListNodes(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Find matching nodes
	var matches []*pb.NodeInfo
	for _, node := range resp.Msg.Nodes {
		if strings.HasPrefix(node.NodeId, idPrefix) {
			matches = append(matches, node)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no node found matching %q", idPrefix)
	}

	if len(matches) > 1 {
		var nodeIDs []string
		for _, n := range matches {
			nodeIDs = append(nodeIDs, n.NodeId)
		}
		return nil, fmt.Errorf("multiple nodes match %q: %s", idPrefix, strings.Join(nodeIDs, ", "))
	}

	return matches[0], nil
}

func getSSHNodeIP(node *pb.NodeInfo) string {
	if node.Metadata == nil {
		return ""
	}
	if node.Metadata.ExternalIp != "" {
		return node.Metadata.ExternalIp
	}
	return node.Metadata.InternalIp
}
