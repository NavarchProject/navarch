// Package sync provides file synchronization between local and remote systems.
package sync

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Options configures the sync operation.
type Options struct {
	// Source is the local directory to sync.
	Source string

	// Dest is the remote destination in format "user@host:path".
	Dest string

	// SSHKey is the path to the SSH private key.
	SSHKey string

	// SSHPort is the SSH port (default: 22).
	SSHPort int

	// Exclude patterns (gitignore-style).
	Exclude []string

	// Delete removes files on remote that don't exist locally.
	Delete bool

	// Verbose enables verbose output.
	Verbose bool

	// Stdout for command output (default: os.Stdout).
	Stdout io.Writer

	// Stderr for command errors (default: os.Stderr).
	Stderr io.Writer
}

// Syncer synchronizes files to remote instances.
type Syncer struct {
	opts Options
}

// New creates a new Syncer with the given options.
func New(opts Options) *Syncer {
	if opts.SSHPort == 0 {
		opts.SSHPort = 22
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	return &Syncer{opts: opts}
}

// Sync synchronizes the source directory to the remote destination.
// Uses rsync if available, falls back to scp.
func (s *Syncer) Sync(ctx context.Context) error {
	// Prefer rsync for efficiency
	if rsyncAvailable() {
		return s.syncWithRsync(ctx)
	}
	return s.syncWithSCP(ctx)
}

func (s *Syncer) syncWithRsync(ctx context.Context) error {
	args := []string{
		"-avz",                   // archive, verbose, compress
		"--progress",             // show progress
		"-e", s.sshCommand(),     // use SSH with key
	}

	if s.opts.Delete {
		args = append(args, "--delete")
	}

	// Add exclude patterns
	for _, pattern := range s.opts.Exclude {
		args = append(args, "--exclude", pattern)
	}

	// Add default excludes for common development artifacts
	defaultExcludes := []string{
		".git",
		"__pycache__",
		"*.pyc",
		".pytest_cache",
		"node_modules",
		".venv",
		"venv",
		".env",
		"*.egg-info",
		".tox",
		".mypy_cache",
		".ruff_cache",
	}
	for _, pattern := range defaultExcludes {
		args = append(args, "--exclude", pattern)
	}

	// Ensure source ends with / to sync contents (not directory itself)
	source := s.opts.Source
	if !strings.HasSuffix(source, "/") {
		source += "/"
	}

	args = append(args, source, s.opts.Dest)

	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmd.Stdout = s.opts.Stdout
	cmd.Stderr = s.opts.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync failed: %w", err)
	}

	return nil
}

func (s *Syncer) syncWithSCP(ctx context.Context) error {
	// SCP fallback for systems without rsync
	args := []string{
		"-r",                     // recursive
		"-i", s.opts.SSHKey,      // identity file
		"-P", fmt.Sprintf("%d", s.opts.SSHPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
	}

	args = append(args, s.opts.Source, s.opts.Dest)

	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Stdout = s.opts.Stdout
	cmd.Stderr = s.opts.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp failed: %w", err)
	}

	return nil
}

func (s *Syncer) sshCommand() string {
	return fmt.Sprintf("ssh -i %s -p %d -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null",
		s.opts.SSHKey, s.opts.SSHPort)
}

func rsyncAvailable() bool {
	_, err := exec.LookPath("rsync")
	return err == nil
}

// SyncToNode is a convenience function to sync files to a Navarch node.
func SyncToNode(ctx context.Context, localDir, user, host string, port int, sshKey, remotePath string) error {
	if remotePath == "" {
		remotePath = "/workspace"
	}

	// Ensure local directory exists
	if _, err := os.Stat(localDir); err != nil {
		return fmt.Errorf("source directory does not exist: %w", err)
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(localDir)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	syncer := New(Options{
		Source:  absPath,
		Dest:    fmt.Sprintf("%s@%s:%s", user, host, remotePath),
		SSHKey:  sshKey,
		SSHPort: port,
		Delete:  true,
		Verbose: true,
	})

	return syncer.Sync(ctx)
}

// WatchAndSync watches the source directory and syncs changes.
// This is useful for development mode where you want live updates.
func (s *Syncer) WatchAndSync(ctx context.Context, onChange func()) error {
	// For initial implementation, we do a simple initial sync
	// TODO: Add file watcher (fsnotify) for live sync in dev mode
	if err := s.Sync(ctx); err != nil {
		return err
	}

	if onChange != nil {
		onChange()
	}

	return nil
}
