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

	"golang.org/x/crypto/ssh"
)

type Config struct {
	SetupCommands     []string
	SSHUser           string
	SSHPrivateKeyPath string
}

// TemplateVars are available in setup command templates as {{.FieldName}}.
type TemplateVars struct {
	ControlPlane string
	Pool         string
	NodeID       string
	Provider     string
	Region       string
	InstanceType string
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

// Bootstrap runs setup commands on the instance via SSH.
func (b *Bootstrapper) Bootstrap(ctx context.Context, ip string, vars TemplateVars) error {
	if len(b.config.SetupCommands) == 0 {
		b.logger.Debug("bootstrap skipped: no commands configured",
			slog.String("node_id", vars.NodeID),
		)
		return nil
	}
	if b.config.SSHPrivateKeyPath == "" {
		return fmt.Errorf("ssh_private_key_path is required for bootstrap")
	}

	bootstrapStart := time.Now()
	b.logger.Info("bootstrap starting",
		slog.String("node_id", vars.NodeID),
		slog.String("ip", ip),
		slog.String("pool", vars.Pool),
		slog.String("provider", vars.Provider),
		slog.String("region", vars.Region),
		slog.String("instance_type", vars.InstanceType),
		slog.String("control_plane", vars.ControlPlane),
		slog.Int("setup_commands", len(b.config.SetupCommands)),
		slog.String("ssh_user", b.config.SSHUser),
	)

	// Load SSH key
	keyPath := b.config.SSHPrivateKeyPath
	if strings.HasPrefix(keyPath, "~/") {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, keyPath[2:])
	}

	b.logger.Debug("loading SSH private key",
		slog.String("path", keyPath),
	)

	key, err := os.ReadFile(keyPath)
	if err != nil {
		b.logger.Error("failed to read SSH private key",
			slog.String("path", keyPath),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("reading SSH private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		b.logger.Error("failed to parse SSH private key",
			slog.String("path", keyPath),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("parsing SSH private key: %w", err)
	}

	b.logger.Debug("SSH key loaded successfully",
		slog.String("key_type", signer.PublicKey().Type()),
	)

	sshConfig := &ssh.ClientConfig{
		User: b.config.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		// TODO(security): use known_hosts or TOFU instead of InsecureIgnoreHostKey
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	// Wait for SSH to become available
	b.logger.Info("waiting for SSH to become available",
		slog.String("ip", ip),
		slog.String("node_id", vars.NodeID),
	)

	sshWaitStart := time.Now()
	client, err := b.waitForSSH(ctx, ip, sshConfig)
	if err != nil {
		b.logger.Error("SSH connection failed",
			slog.String("ip", ip),
			slog.String("node_id", vars.NodeID),
			slog.Duration("wait_duration", time.Since(sshWaitStart)),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("waiting for SSH: %w", err)
	}
	defer client.Close()

	b.logger.Info("SSH connection established",
		slog.String("ip", ip),
		slog.String("node_id", vars.NodeID),
		slog.Duration("wait_duration", time.Since(sshWaitStart)),
	)

	// Run setup commands
	b.logger.Info("starting setup commands",
		slog.String("node_id", vars.NodeID),
		slog.Int("count", len(b.config.SetupCommands)),
	)

	for i, cmd := range b.config.SetupCommands {
		renderedCmd, err := b.renderCommand(cmd, vars)
		if err != nil {
			b.logger.Error("failed to render command template",
				slog.String("node_id", vars.NodeID),
				slog.Int("command_num", i+1),
				slog.String("template", cmd),
				slog.String("error", err.Error()),
			)
			return fmt.Errorf("rendering command %d: %w", i+1, err)
		}

		cmdStart := time.Now()
		b.logger.Info("executing setup command",
			slog.String("node_id", vars.NodeID),
			slog.Int("command_num", i+1),
			slog.Int("command_total", len(b.config.SetupCommands)),
			slog.String("command", renderedCmd),
		)

		stdout, stderr, err := b.runCommand(client, renderedCmd)
		if err != nil {
			b.logger.Error("setup command failed",
				slog.String("node_id", vars.NodeID),
				slog.Int("command_num", i+1),
				slog.String("command", renderedCmd),
				slog.Duration("duration", time.Since(cmdStart)),
				slog.String("stdout", stdout),
				slog.String("stderr", stderr),
				slog.String("error", err.Error()),
			)
			return fmt.Errorf("command %d failed: %w", i+1, err)
		}

		b.logger.Info("setup command completed",
			slog.String("node_id", vars.NodeID),
			slog.Int("command_num", i+1),
			slog.Duration("duration", time.Since(cmdStart)),
		)

		if stdout != "" {
			b.logger.Debug("command stdout",
				slog.String("node_id", vars.NodeID),
				slog.Int("command_num", i+1),
				slog.String("stdout", stdout),
			)
		}
	}

	b.logger.Info("all setup commands completed",
		slog.String("node_id", vars.NodeID),
		slog.Int("count", len(b.config.SetupCommands)),
	)

	b.logger.Info("bootstrap completed successfully",
		slog.String("node_id", vars.NodeID),
		slog.String("ip", ip),
		slog.Duration("total_duration", time.Since(bootstrapStart)),
		slog.Int("commands_executed", len(b.config.SetupCommands)),
	)

	return nil
}

func (b *Bootstrapper) waitForSSH(ctx context.Context, ip string, config *ssh.ClientConfig) (*ssh.Client, error) {
	addr := net.JoinHostPort(ip, "22")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.After(10 * time.Minute)
	attempt := 0

	dial := func() (*ssh.Client, error) {
		attempt++
		dialStart := time.Now()

		b.logger.Debug("SSH connection attempt",
			slog.String("addr", addr),
			slog.Int("attempt", attempt),
		)

		client, err := ssh.Dial("tcp", addr, config)
		if err != nil {
			b.logger.Debug("SSH connection attempt failed",
				slog.String("addr", addr),
				slog.Int("attempt", attempt),
				slog.Duration("duration", time.Since(dialStart)),
				slog.String("error", err.Error()),
			)
			return nil, err
		}

		b.logger.Debug("SSH connection attempt succeeded",
			slog.String("addr", addr),
			slog.Int("attempt", attempt),
			slog.Duration("duration", time.Since(dialStart)),
		)
		return client, nil
	}

	if client, err := dial(); err == nil {
		return client, nil
	}

	for {
		select {
		case <-ctx.Done():
			b.logger.Warn("SSH wait cancelled",
				slog.String("addr", addr),
				slog.Int("attempts", attempt),
				slog.String("reason", "context cancelled"),
			)
			return nil, ctx.Err()
		case <-timeout:
			b.logger.Warn("SSH wait timed out",
				slog.String("addr", addr),
				slog.Int("attempts", attempt),
				slog.String("reason", "10 minute timeout"),
			)
			return nil, fmt.Errorf("timeout waiting for SSH on %s after %d attempts", addr, attempt)
		case <-ticker.C:
			if client, err := dial(); err == nil {
				return client, nil
			}
		}
	}
}

func (b *Bootstrapper) runCommand(client *ssh.Client, cmd string) (string, string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmd)
	stdoutStr := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())

	if err != nil {
		return stdoutStr, stderrStr, fmt.Errorf("%w: stderr: %s", err, stderrStr)
	}

	return stdoutStr, stderrStr, nil
}

func (b *Bootstrapper) renderCommand(cmd string, vars TemplateVars) (string, error) {
	tmpl, err := template.New("cmd").Parse(cmd)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}
