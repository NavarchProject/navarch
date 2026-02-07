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

// FileUpload specifies a local file to copy to the remote instance before
// running setup commands.
type FileUpload struct {
	LocalPath  string // Path to local file
	RemotePath string // Destination path on remote instance
	Mode       string // File permissions (e.g., "0755"). Default: "0644"
}

type Config struct {
	SetupCommands     []string
	FileUploads       []FileUpload // Files to SCP before running commands
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

// Bootstrap uploads files and runs setup commands on the instance via SSH.
func (b *Bootstrapper) Bootstrap(ctx context.Context, ip string, vars TemplateVars) error {
	if len(b.config.SetupCommands) == 0 && len(b.config.FileUploads) == 0 {
		return nil
	}
	if b.config.SSHPrivateKeyPath == "" {
		return fmt.Errorf("ssh_private_key_path is required for bootstrap")
	}

	b.logger.Info("starting bootstrap",
		slog.String("ip", ip),
		slog.String("node_id", vars.NodeID),
		slog.Int("commands", len(b.config.SetupCommands)),
	)

	keyPath := b.config.SSHPrivateKeyPath
	if strings.HasPrefix(keyPath, "~/") {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, keyPath[2:])
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("reading SSH private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("parsing SSH private key: %w", err)
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

	client, err := b.waitForSSH(ctx, ip, sshConfig)
	if err != nil {
		return fmt.Errorf("waiting for SSH: %w", err)
	}
	defer client.Close()

	for _, upload := range b.config.FileUploads {
		if err := b.uploadFile(client, upload); err != nil {
			return fmt.Errorf("uploading %s: %w", upload.LocalPath, err)
		}
	}

	for i, cmd := range b.config.SetupCommands {
		renderedCmd, err := b.renderCommand(cmd, vars)
		if err != nil {
			return fmt.Errorf("rendering command %d: %w", i+1, err)
		}

		b.logger.Debug("executing setup command",
			slog.Int("step", i+1),
			slog.String("command", renderedCmd),
		)

		if err := b.runCommand(client, renderedCmd); err != nil {
			return fmt.Errorf("command %d failed: %w", i+1, err)
		}
	}

	b.logger.Info("bootstrap completed",
		slog.String("ip", ip),
		slog.String("node_id", vars.NodeID),
	)

	return nil
}

func (b *Bootstrapper) waitForSSH(ctx context.Context, ip string, config *ssh.ClientConfig) (*ssh.Client, error) {
	addr := net.JoinHostPort(ip, "22")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.After(10 * time.Minute)

	dial := func() (*ssh.Client, error) {
		client, err := ssh.Dial("tcp", addr, config)
		if err != nil {
			b.logger.Debug("SSH not ready", slog.String("addr", addr), slog.String("error", err.Error()))
			return nil, err
		}
		return client, nil
	}

	if client, err := dial(); err == nil {
		return client, nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for SSH on %s", addr)
		case <-ticker.C:
			if client, err := dial(); err == nil {
				return client, nil
			}
		}
	}
}

func (b *Bootstrapper) uploadFile(client *ssh.Client, upload FileUpload) error {
	data, err := os.ReadFile(upload.LocalPath)
	if err != nil {
		return fmt.Errorf("reading local file: %w", err)
	}

	mode := upload.Mode
	if mode == "" {
		mode = "0644"
	}

	b.logger.Info("uploading file",
		slog.String("local", upload.LocalPath),
		slog.String("remote", upload.RemotePath),
		slog.String("size", fmt.Sprintf("%d bytes", len(data))),
	)

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	session.Stdin = bytes.NewReader(data)

	var stderr bytes.Buffer
	session.Stderr = &stderr

	cmd := fmt.Sprintf("sudo sh -c 'cat > %s && chmod %s %s'", upload.RemotePath, mode, upload.RemotePath)
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("%w: stderr: %s", err, stderr.String())
	}

	return nil
}

func (b *Bootstrapper) runCommand(client *ssh.Client, cmd string) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmd)
	if err != nil {
		return fmt.Errorf("%w: stderr: %s", err, stderr.String())
	}

	if stdout.Len() > 0 {
		b.logger.Debug("command output", slog.String("stdout", strings.TrimSpace(stdout.String())))
	}

	return nil
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
