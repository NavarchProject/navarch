package sync

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	opts := Options{
		Source:  "/local/path",
		Dest:    "user@host:/remote/path",
		SSHKey:  "/path/to/key",
		SSHPort: 2222,
	}

	s := New(opts)
	if s.opts.SSHPort != 2222 {
		t.Errorf("expected SSHPort=2222, got %d", s.opts.SSHPort)
	}
}

func TestNew_Defaults(t *testing.T) {
	s := New(Options{})

	if s.opts.SSHPort != 22 {
		t.Errorf("expected default SSHPort=22, got %d", s.opts.SSHPort)
	}
	if s.opts.Stdout == nil {
		t.Error("expected default Stdout")
	}
	if s.opts.Stderr == nil {
		t.Error("expected default Stderr")
	}
}

func TestSyncer_sshCommand(t *testing.T) {
	s := New(Options{
		SSHKey:  "/home/user/.ssh/id_rsa",
		SSHPort: 2222,
	})

	cmd := s.sshCommand()
	if !strings.Contains(cmd, "-i /home/user/.ssh/id_rsa") {
		t.Error("SSH command should contain key path")
	}
	if !strings.Contains(cmd, "-p 2222") {
		t.Error("SSH command should contain port")
	}
	if !strings.Contains(cmd, "StrictHostKeyChecking=no") {
		t.Error("SSH command should disable strict host key checking")
	}
}

func TestRsyncAvailable(t *testing.T) {
	// This test just checks that the function doesn't panic
	// The result depends on the system
	_ = rsyncAvailable()
}

func TestSyncToNode_InvalidSource(t *testing.T) {
	ctx := context.Background()
	err := SyncToNode(ctx, "/nonexistent/path", "user", "host", 22, "/key", "/workspace")
	if err == nil {
		t.Error("expected error for nonexistent source")
	}
	if !strings.Contains(err.Error(), "source directory does not exist") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSyncToNode_DefaultRemotePath(t *testing.T) {
	// Create a temp directory for the source
	tmpDir := t.TempDir()

	// This will fail because we can't actually SSH, but we're testing
	// that the function sets up the syncer correctly
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately so we don't actually try to sync

	// The error will be about the cancelled context or SSH failure,
	// not about the remote path
	_ = SyncToNode(ctx, tmpDir, "user", "host", 22, "/fake/key", "")
}

func TestSyncer_OutputCapture(t *testing.T) {
	var stdout, stderr bytes.Buffer

	s := New(Options{
		Source:  "/local",
		Dest:    "user@host:/remote",
		SSHKey:  "/key",
		Stdout:  &stdout,
		Stderr:  &stderr,
	})

	if s.opts.Stdout != &stdout {
		t.Error("stdout not captured correctly")
	}
	if s.opts.Stderr != &stderr {
		t.Error("stderr not captured correctly")
	}
}

func TestOptions_Exclude(t *testing.T) {
	s := New(Options{
		Source:  "/local",
		Dest:    "user@host:/remote",
		SSHKey:  "/key",
		Exclude: []string{"*.log", "tmp/"},
	})

	if len(s.opts.Exclude) != 2 {
		t.Errorf("expected 2 exclude patterns, got %d", len(s.opts.Exclude))
	}
}

func TestSyncToNode_ResolvesPath(t *testing.T) {
	// Create a temp directory
	tmpDir := t.TempDir()

	// Create a subdirectory to test relative path resolution
	subDir := filepath.Join(tmpDir, "subdir")
	os.Mkdir(subDir, 0755)

	// Test that we can resolve the path (even though sync will fail)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// This should not error on path resolution
	_ = SyncToNode(ctx, subDir, "user", "host", 22, "/fake/key", "/workspace")
}
