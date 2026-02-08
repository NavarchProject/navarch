package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"al.essio.dev/pkg/shellescape"
	"golang.org/x/crypto/ssh"
)

type Config struct {
	SetupCommands     []string
	SSHUser           string
	SSHPrivateKeyPath string

	// Timeouts (zero means use defaults)
	SSHTimeout        time.Duration // Max time to wait for SSH to become available (default: 10m)
	SSHConnectTimeout time.Duration // Timeout per SSH connection attempt (default: 30s)
	CommandTimeout    time.Duration // Max time per command execution (default: 5m)
}

// Default timeouts
const (
	DefaultSSHTimeout        = 10 * time.Minute
	DefaultSSHConnectTimeout = 30 * time.Second
	DefaultCommandTimeout    = 5 * time.Minute
)

// TemplateVars are available in setup command templates as {{.FieldName}}.
type TemplateVars struct {
	ControlPlane string
	Pool         string
	NodeID       string
	Provider     string
	Region       string
	InstanceType string
}

// CommandResult captures the output of a single command execution.
type CommandResult struct {
	Command  string        `json:"command"`
	Stdout   string        `json:"stdout,omitempty"`
	Stderr   string        `json:"stderr,omitempty"`
	Duration time.Duration `json:"duration"`
	ExitCode int           `json:"exit_code"`
}

// Result captures the full bootstrap execution for a node.
type Result struct {
	NodeID      string          `json:"node_id"`
	IP          string          `json:"ip"`
	StartTime   time.Time       `json:"start_time"`
	Duration    time.Duration   `json:"duration"`
	Commands    []CommandResult `json:"commands"`
	SSHWaitTime time.Duration   `json:"ssh_wait_time"`
	Success     bool            `json:"success"`
	Error       string          `json:"error,omitempty"`
}

// Bootstrapper runs setup commands on newly provisioned instances via SSH.
type Bootstrapper struct {
	config Config
	logger *slog.Logger
}

func New(cfg Config, logger *slog.Logger) *Bootstrapper {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bootstrapper{
		config: cfg,
		logger: logger,
	}
}

func (b *Bootstrapper) sshTimeout() time.Duration {
	if b.config.SSHTimeout > 0 {
		return b.config.SSHTimeout
	}
	return DefaultSSHTimeout
}

func (b *Bootstrapper) sshConnectTimeout() time.Duration {
	if b.config.SSHConnectTimeout > 0 {
		return b.config.SSHConnectTimeout
	}
	return DefaultSSHConnectTimeout
}

func (b *Bootstrapper) commandTimeout() time.Duration {
	if b.config.CommandTimeout > 0 {
		return b.config.CommandTimeout
	}
	return DefaultCommandTimeout
}

// Bootstrap runs setup commands on the instance via SSH.
// Returns a Result containing command outputs for logging/debugging.
func (b *Bootstrapper) Bootstrap(ctx context.Context, ip string, vars TemplateVars) (*Result, error) {
	result := &Result{
		NodeID:    vars.NodeID,
		IP:        ip,
		StartTime: time.Now(),
		Commands:  make([]CommandResult, 0, len(b.config.SetupCommands)),
	}

	if len(b.config.SetupCommands) == 0 {
		result.Success = true
		return result, nil
	}
	if b.config.SSHPrivateKeyPath == "" {
		result.Error = "ssh_private_key_path is required for bootstrap"
		return result, fmt.Errorf("ssh_private_key_path is required for bootstrap")
	}

	b.logger.Info("bootstrap starting",
		slog.String("node_id", vars.NodeID),
		slog.String("ip", ip),
		slog.String("pool", vars.Pool),
		slog.Int("commands", len(b.config.SetupCommands)),
	)

	client, sshWait, err := b.connect(ctx, ip, vars.NodeID)
	result.SSHWaitTime = sshWait
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(result.StartTime)
		return result, err
	}
	defer client.Close()

	for i, cmd := range b.config.SetupCommands {
		cmdResult, err := b.executeCommand(ctx, client, cmd, vars, i+1, len(b.config.SetupCommands))
		result.Commands = append(result.Commands, cmdResult)
		if err != nil {
			result.Error = err.Error()
			result.Duration = time.Since(result.StartTime)
			return result, err
		}
	}

	result.Success = true
	result.Duration = time.Since(result.StartTime)
	b.logger.Info("bootstrap completed",
		slog.String("node_id", vars.NodeID),
		slog.String("ip", ip),
		slog.Duration("duration", result.Duration),
		slog.Int("commands", len(b.config.SetupCommands)),
	)
	return result, nil
}

func (b *Bootstrapper) connect(ctx context.Context, ip, nodeID string) (*ssh.Client, time.Duration, error) {
	keyPath := b.config.SSHPrivateKeyPath
	if strings.HasPrefix(keyPath, "~/") {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, keyPath[2:])
	}

	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, 0, fmt.Errorf("reading SSH private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, 0, fmt.Errorf("parsing SSH private key: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User: b.config.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		// TODO(#27): use known_hosts or TOFU instead of InsecureIgnoreHostKey
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         b.sshConnectTimeout(),
	}

	b.logger.Info("waiting for SSH",
		slog.String("ip", ip),
		slog.String("node_id", nodeID),
	)

	start := time.Now()
	client, err := b.waitForSSH(ctx, ip, sshConfig)
	wait := time.Since(start)
	if err != nil {
		return nil, wait, fmt.Errorf("SSH connection failed after %v: %w", wait, err)
	}

	b.logger.Info("SSH connected",
		slog.String("ip", ip),
		slog.String("node_id", nodeID),
		slog.Duration("wait", wait),
	)
	return client, wait, nil
}

func (b *Bootstrapper) executeCommand(ctx context.Context, client *ssh.Client, cmd string, vars TemplateVars, num, total int) (CommandResult, error) {
	result := CommandResult{Command: cmd}

	rendered, err := b.renderCommand(cmd, vars)
	if err != nil {
		return result, fmt.Errorf("rendering command %d: %w", num, err)
	}

	start := time.Now()
	b.logger.Info("executing command",
		slog.String("node_id", vars.NodeID),
		slog.Int("command", num),
		slog.Int("total", total),
	)

	stdout, stderr, exitCode, err := b.runCommand(ctx, client, rendered)
	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode
	result.Duration = time.Since(start)

	if err != nil {
		b.logger.Error("command failed",
			slog.String("node_id", vars.NodeID),
			slog.Int("command", num),
			slog.Duration("duration", result.Duration),
			slog.String("stdout", stdout),
			slog.String("stderr", stderr),
		)
		return result, fmt.Errorf("command %d failed: %w", num, err)
	}

	b.logger.Info("command succeeded",
		slog.String("node_id", vars.NodeID),
		slog.Int("command", num),
		slog.Duration("duration", result.Duration),
	)
	return result, nil
}

func (b *Bootstrapper) waitForSSH(ctx context.Context, ip string, config *ssh.ClientConfig) (*ssh.Client, error) {
	addr := net.JoinHostPort(ip, "22")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	timeout := time.After(b.sshTimeout())

	for attempts := 1; ; attempts++ {
		client, err := ssh.Dial("tcp", addr, config)
		if err == nil {
			return client, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for SSH on %s after %d attempts", addr, attempts)
		case <-ticker.C:
			// retry
		}
	}
}

func (b *Bootstrapper) runCommand(ctx context.Context, client *ssh.Client, cmd string) (stdout, stderr string, exitCode int, err error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", -1, fmt.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Create timeout context for the command
	cmdCtx, cancel := context.WithTimeout(ctx, b.commandTimeout())
	defer cancel()

	// Run command in goroutine so we can enforce timeout
	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()

	select {
	case <-cmdCtx.Done():
		// Timeout or cancellation - try to signal the remote process
		session.Signal(ssh.SIGKILL)
		return strings.TrimSpace(stdoutBuf.String()), strings.TrimSpace(stderrBuf.String()), -1,
			fmt.Errorf("command timed out after %v", b.commandTimeout())
	case runErr := <-done:
		stdout = strings.TrimSpace(stdoutBuf.String())
		stderr = strings.TrimSpace(stderrBuf.String())

		if runErr != nil {
			// Extract exit code from ssh.ExitError if available
			if exitErr, ok := runErr.(*ssh.ExitError); ok {
				exitCode = exitErr.ExitStatus()
			} else {
				exitCode = -1
			}
			return stdout, stderr, exitCode, fmt.Errorf("%w: stderr: %s", runErr, stderr)
		}

		return stdout, stderr, 0, nil
	}
}

func (b *Bootstrapper) renderCommand(cmd string, vars TemplateVars) (string, error) {
	// Shell-escape template variables to prevent injection attacks.
	// Uses shellescape.Quote which properly quotes strings for POSIX shells.
	escapedVars := TemplateVars{
		ControlPlane: shellescape.Quote(vars.ControlPlane),
		Pool:         shellescape.Quote(vars.Pool),
		NodeID:       shellescape.Quote(vars.NodeID),
		Provider:     shellescape.Quote(vars.Provider),
		Region:       shellescape.Quote(vars.Region),
		InstanceType: shellescape.Quote(vars.InstanceType),
	}

	tmpl, err := template.New("cmd").Parse(cmd)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, escapedVars); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}
