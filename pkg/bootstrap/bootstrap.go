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
		return nil
	}
	if b.config.SSHPrivateKeyPath == "" {
		return fmt.Errorf("ssh_private_key_path is required for bootstrap")
	}

	start := time.Now()
	b.logger.Info("bootstrap starting",
		slog.String("node_id", vars.NodeID),
		slog.String("ip", ip),
		slog.String("pool", vars.Pool),
		slog.Int("commands", len(b.config.SetupCommands)),
	)

	client, err := b.connect(ctx, ip, vars.NodeID)
	if err != nil {
		return err
	}
	defer client.Close()

	for i, cmd := range b.config.SetupCommands {
		if err := b.executeCommand(client, cmd, vars, i+1, len(b.config.SetupCommands)); err != nil {
			return err
		}
	}

	b.logger.Info("bootstrap completed",
		slog.String("node_id", vars.NodeID),
		slog.String("ip", ip),
		slog.Duration("duration", time.Since(start)),
		slog.Int("commands", len(b.config.SetupCommands)),
	)
	return nil
}

func (b *Bootstrapper) connect(ctx context.Context, ip, nodeID string) (*ssh.Client, error) {
	keyPath := b.config.SSHPrivateKeyPath
	if strings.HasPrefix(keyPath, "~/") {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, keyPath[2:])
	}

	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading SSH private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parsing SSH private key: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User: b.config.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		// TODO(security): use known_hosts or TOFU instead of InsecureIgnoreHostKey
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	b.logger.Info("waiting for SSH",
		slog.String("ip", ip),
		slog.String("node_id", nodeID),
	)

	start := time.Now()
	client, err := b.waitForSSH(ctx, ip, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("SSH connection failed after %v: %w", time.Since(start), err)
	}

	b.logger.Info("SSH connected",
		slog.String("ip", ip),
		slog.String("node_id", nodeID),
		slog.Duration("wait", time.Since(start)),
	)
	return client, nil
}

func (b *Bootstrapper) executeCommand(client *ssh.Client, cmd string, vars TemplateVars, num, total int) error {
	rendered, err := b.renderCommand(cmd, vars)
	if err != nil {
		return fmt.Errorf("rendering command %d: %w", num, err)
	}

	start := time.Now()
	b.logger.Info("executing command",
		slog.String("node_id", vars.NodeID),
		slog.Int("command", num),
		slog.Int("total", total),
	)

	stdout, stderr, err := b.runCommand(client, rendered)
	if err != nil {
		b.logger.Error("command failed",
			slog.String("node_id", vars.NodeID),
			slog.Int("command", num),
			slog.Duration("duration", time.Since(start)),
			slog.String("stdout", stdout),
			slog.String("stderr", stderr),
		)
		return fmt.Errorf("command %d failed: %w", num, err)
	}

	b.logger.Info("command succeeded",
		slog.String("node_id", vars.NodeID),
		slog.Int("command", num),
		slog.Duration("duration", time.Since(start)),
	)
	return nil
}

func (b *Bootstrapper) waitForSSH(ctx context.Context, ip string, config *ssh.ClientConfig) (*ssh.Client, error) {
	addr := net.JoinHostPort(ip, "22")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	timeout := time.After(10 * time.Minute)

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
