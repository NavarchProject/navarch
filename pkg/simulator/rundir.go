package simulator

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// RunDir manages the directory structure for a simulation run.
// All artifacts (logs, reports, config) are stored in a single timestamped directory.
type RunDir struct {
	mu      sync.Mutex
	baseDir string
	logsDir string
	files   map[string]*os.File
}

// NewRunDir creates a new run directory with a timestamped subdirectory.
// The directory structure is:
//
//	{baseDir}/{timestamp}/
//	├── logs/           # Per-node log files
//	├── scenario.yaml   # Copy of input scenario
//	├── report.json     # JSON report
//	└── report.html     # HTML report
func NewRunDir(baseDir string, scenario *Scenario) (*RunDir, error) {
	if baseDir == "" {
		baseDir = "./sim-runs"
	}

	// Include nanoseconds to avoid collisions when starting multiple runs quickly
	timestamp := time.Now().Format("2006-01-02_15-04-05.000000000")
	runDir := filepath.Join(baseDir, timestamp)
	logsDir := filepath.Join(runDir, "logs")

	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create run directory: %w", err)
	}

	rd := &RunDir{
		baseDir: runDir,
		logsDir: logsDir,
		files:   make(map[string]*os.File),
	}

	if scenario != nil {
		if err := rd.saveScenario(scenario); err != nil {
			return nil, fmt.Errorf("failed to save scenario: %w", err)
		}
	}

	return rd, nil
}

// Dir returns the run directory path.
func (rd *RunDir) Dir() string {
	return rd.baseDir
}

// LogsDir returns the logs subdirectory path.
func (rd *RunDir) LogsDir() string {
	return rd.logsDir
}

// ReportPath returns the path for the JSON report.
func (rd *RunDir) ReportPath() string {
	return filepath.Join(rd.baseDir, "report.json")
}

// HTMLReportPath returns the path for the HTML report.
func (rd *RunDir) HTMLReportPath() string {
	return filepath.Join(rd.baseDir, "report.html")
}

// ScenarioPath returns the path to the saved scenario.
func (rd *RunDir) ScenarioPath() string {
	return filepath.Join(rd.baseDir, "scenario.yaml")
}

// NodeLogPath returns the path for a specific node's log file.
func (rd *RunDir) NodeLogPath(nodeID string) string {
	return filepath.Join(rd.logsDir, fmt.Sprintf("%s.log", sanitizeFilename(nodeID)))
}

// ControlPlaneLogPath returns the path for the control plane log.
func (rd *RunDir) ControlPlaneLogPath() string {
	return filepath.Join(rd.logsDir, "control-plane.log")
}

func (rd *RunDir) saveScenario(scenario *Scenario) error {
	data, err := yaml.Marshal(scenario)
	if err != nil {
		return err
	}
	return os.WriteFile(rd.ScenarioPath(), data, 0644)
}

// sanitizeFilename removes path separators and other dangerous characters.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "..", "_")
	return name
}

// CreateNodeLogger creates a logger for a specific node that writes to a file.
func (rd *RunDir) CreateNodeLogger(nodeID string) (*slog.Logger, error) {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	filename := rd.NodeLogPath(nodeID)
	f, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create node log file: %w", err)
	}
	rd.files[nodeID] = f

	handler := slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	return slog.New(handler).With(slog.String("node_id", nodeID)), nil
}

// CreateControlPlaneLogger creates a logger for the control plane.
func (rd *RunDir) CreateControlPlaneLogger() (*slog.Logger, error) {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	filename := rd.ControlPlaneLogPath()
	f, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create control plane log file: %w", err)
	}
	rd.files["control-plane"] = f

	handler := slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	return slog.New(handler).With(slog.String("component", "control-plane")), nil
}

// Close closes all open log files.
func (rd *RunDir) Close() error {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	var errs []error
	for name, f := range rd.files {
		if err := f.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("sync %s: %w", name, err))
		}
		if err := f.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", name, err))
		}
	}
	rd.files = make(map[string]*os.File)
	return errors.Join(errs...)
}

// GetAllLogPaths returns paths to all log files.
func (rd *RunDir) GetAllLogPaths() map[string]string {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	result := make(map[string]string, len(rd.files))
	for name := range rd.files {
		if name == "control-plane" {
			result[name] = rd.ControlPlaneLogPath()
		} else {
			result[name] = rd.NodeLogPath(name)
		}
	}
	return result
}

