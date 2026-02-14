package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var (
	logsUser   string
	logsKey    string
	logsFollow bool
	logsTail   int
	logsPath   string
)

func logsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <node-id>",
		Short: "View logs from a GPU node",
		Long: `Fetch and display logs from a GPU node.

By default, shows the task output log at /workspace/output.log (created by
detached runs). Use --path to specify a different log file.

Examples:
  # View task output
  navarch logs node-abc123

  # Follow logs (like tail -f)
  navarch logs -f node-abc123

  # View last 100 lines
  navarch logs --tail 100 node-abc123

  # View a specific log file
  navarch logs node-abc123 --path /var/log/syslog`,
		Args: cobra.ExactArgs(1),
		RunE: runLogs,
	}

	cmd.Flags().StringVarP(&logsUser, "user", "u", "ubuntu", "SSH user")
	cmd.Flags().StringVarP(&logsKey, "identity", "i", "", "SSH private key path")
	cmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVar(&logsTail, "tail", 0, "Number of lines to show from end (0 = all)")
	cmd.Flags().StringVar(&logsPath, "path", "/workspace/output.log", "Log file path on remote")

	return cmd
}

func runLogs(cmd *cobra.Command, args []string) error {
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

	// Find SSH key if not specified
	keyPath := logsKey
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

	// Build remote command
	var remoteCmd string
	if logsFollow {
		remoteCmd = fmt.Sprintf("tail -f %s", logsPath)
	} else if logsTail > 0 {
		remoteCmd = fmt.Sprintf("tail -n %d %s", logsTail, logsPath)
	} else {
		remoteCmd = fmt.Sprintf("cat %s", logsPath)
	}

	// Build SSH args
	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
	}

	if keyPath != "" {
		sshArgs = append(sshArgs, "-i", keyPath)
	}

	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", logsUser, ip), remoteCmd)

	sshExec := exec.Command("ssh", sshArgs...)
	sshExec.Stdout = os.Stdout
	sshExec.Stderr = os.Stderr

	// For follow mode, handle interrupt
	if logsFollow {
		sshExec.Stdin = os.Stdin
	}

	return sshExec.Run()
}
