package simulator

import (
	"os"
	"strings"
	"testing"
)

func TestRunDir_Create(t *testing.T) {
	tmpDir := t.TempDir()
	rd, err := NewRunDir(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create run dir: %v", err)
	}
	defer rd.Close()

	if rd.Dir() == "" {
		t.Error("expected non-empty dir")
	}

	if _, err := os.Stat(rd.Dir()); os.IsNotExist(err) {
		t.Error("run directory should exist")
	}

	if _, err := os.Stat(rd.LogsDir()); os.IsNotExist(err) {
		t.Error("logs directory should exist")
	}
}

func TestRunDir_NodeLogger(t *testing.T) {
	tmpDir := t.TempDir()
	rd, err := NewRunDir(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create run dir: %v", err)
	}
	defer rd.Close()

	logger, err := rd.CreateNodeLogger("test-node-001")
	if err != nil {
		t.Fatalf("failed to create node logger: %v", err)
	}

	logger.Info("test message")
	rd.Close()

	content, err := os.ReadFile(rd.NodeLogPath("test-node-001"))
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "test message") {
		t.Errorf("log file should contain 'test message', got: %s", string(content))
	}
}

func TestRunDir_ControlPlaneLogger(t *testing.T) {
	tmpDir := t.TempDir()
	rd, err := NewRunDir(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create run dir: %v", err)
	}
	defer rd.Close()

	logger, err := rd.CreateControlPlaneLogger()
	if err != nil {
		t.Fatalf("failed to create control plane logger: %v", err)
	}

	logger.Info("control plane message")
	rd.Close()

	content, err := os.ReadFile(rd.ControlPlaneLogPath())
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "control plane message") {
		t.Errorf("log file should contain 'control plane message', got: %s", string(content))
	}
}

func TestRunDir_GetAllLogPaths(t *testing.T) {
	tmpDir := t.TempDir()
	rd, err := NewRunDir(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create run dir: %v", err)
	}
	defer rd.Close()

	if _, err := rd.CreateNodeLogger("node-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rd.CreateNodeLogger("node-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := rd.CreateControlPlaneLogger(); err != nil {
		t.Fatal(err)
	}

	paths := rd.GetAllLogPaths()
	if len(paths) != 3 {
		t.Errorf("expected 3 paths, got %d", len(paths))
	}

	if _, ok := paths["node-1"]; !ok {
		t.Error("expected node-1 path")
	}
	if _, ok := paths["node-2"]; !ok {
		t.Error("expected node-2 path")
	}
	if _, ok := paths["control-plane"]; !ok {
		t.Error("expected control-plane path")
	}
}

func TestRunDir_Paths(t *testing.T) {
	tmpDir := t.TempDir()
	rd, err := NewRunDir(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create run dir: %v", err)
	}
	defer rd.Close()

	if !strings.HasPrefix(rd.Dir(), tmpDir) {
		t.Errorf("expected dir to start with %s, got %s", tmpDir, rd.Dir())
	}

	if !strings.HasSuffix(rd.ReportPath(), "report.json") {
		t.Errorf("expected report path to end with report.json, got %s", rd.ReportPath())
	}

	if !strings.HasSuffix(rd.HTMLReportPath(), "report.html") {
		t.Errorf("expected HTML report path to end with report.html, got %s", rd.HTMLReportPath())
	}

	if !strings.HasSuffix(rd.ScenarioPath(), "scenario.yaml") {
		t.Errorf("expected scenario path to end with scenario.yaml, got %s", rd.ScenarioPath())
	}
}

func TestRunDir_SavesScenario(t *testing.T) {
	tmpDir := t.TempDir()
	scenario := &Scenario{
		Name: "test-scenario",
		Fleet: []NodeSpec{
			{ID: "node-1"},
		},
	}

	rd, err := NewRunDir(tmpDir, scenario)
	if err != nil {
		t.Fatalf("failed to create run dir: %v", err)
	}
	defer rd.Close()

	content, err := os.ReadFile(rd.ScenarioPath())
	if err != nil {
		t.Fatalf("failed to read scenario file: %v", err)
	}

	if !strings.Contains(string(content), "test-scenario") {
		t.Errorf("scenario file should contain scenario name, got: %s", string(content))
	}
}

func TestRunDir_SanitizesNodeID(t *testing.T) {
	tmpDir := t.TempDir()
	rd, err := NewRunDir(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create run dir: %v", err)
	}
	defer rd.Close()

	// Test that path traversal attempts are sanitized
	path := rd.NodeLogPath("../../../etc/passwd")
	if strings.Contains(path, "..") {
		t.Errorf("path should not contain '..': %s", path)
	}
	if !strings.HasPrefix(path, rd.LogsDir()) {
		t.Errorf("path should be within logs dir: %s", path)
	}
}

