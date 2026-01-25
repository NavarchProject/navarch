package simulator

import (
	"log/slog"
	"os"
	"testing"
)

func TestStressMetrics_RecordNodeHealth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewStressMetrics(logger)

	// Record some node starts
	m.RecordNodeStart("node-1")
	m.RecordNodeStart("node-2")
	m.RecordNodeStart("node-3")

	stats := m.GetCurrentStats()
	if stats["nodes_healthy"].(int64) != 3 {
		t.Errorf("initial healthy nodes = %v, want 3", stats["nodes_healthy"])
	}
	if stats["nodes_unhealthy"].(int64) != 0 {
		t.Errorf("initial unhealthy nodes = %v, want 0", stats["nodes_unhealthy"])
	}

	// Change node-1 to unhealthy
	m.RecordNodeHealth("node-1", "unhealthy")

	stats = m.GetCurrentStats()
	if stats["nodes_healthy"].(int64) != 2 {
		t.Errorf("after unhealthy: healthy nodes = %v, want 2", stats["nodes_healthy"])
	}
	if stats["nodes_unhealthy"].(int64) != 1 {
		t.Errorf("after unhealthy: unhealthy nodes = %v, want 1", stats["nodes_unhealthy"])
	}

	// Change node-2 to degraded
	m.RecordNodeHealth("node-2", "degraded")

	stats = m.GetCurrentStats()
	if stats["nodes_healthy"].(int64) != 1 {
		t.Errorf("after degraded: healthy nodes = %v, want 1", stats["nodes_healthy"])
	}
	if stats["nodes_degraded"].(int64) != 1 {
		t.Errorf("after degraded: degraded nodes = %v, want 1", stats["nodes_degraded"])
	}

	// Recover node-1 to healthy
	m.RecordNodeHealth("node-1", "healthy")

	stats = m.GetCurrentStats()
	if stats["nodes_healthy"].(int64) != 2 {
		t.Errorf("after recovery: healthy nodes = %v, want 2", stats["nodes_healthy"])
	}
	if stats["nodes_unhealthy"].(int64) != 0 {
		t.Errorf("after recovery: unhealthy nodes = %v, want 0", stats["nodes_unhealthy"])
	}
}

func TestStressMetrics_RecordNodeHealth_NoChange(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewStressMetrics(logger)

	m.RecordNodeStart("node-1")

	// Record same status - should not change counters
	m.RecordNodeHealth("node-1", "healthy")
	m.RecordNodeHealth("node-1", "healthy")
	m.RecordNodeHealth("node-1", "healthy")

	stats := m.GetCurrentStats()
	if stats["nodes_healthy"].(int64) != 1 {
		t.Errorf("healthy nodes = %v, want 1 (should not increment on same status)", stats["nodes_healthy"])
	}
}

func TestStressMetrics_ComputeSummary_SkipsEmptySamples(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewStressMetrics(logger)

	// Simulate startup phase - sample taken before nodes started
	m.samples = append(m.samples, MetricSample{
		TotalNodes:   0, // No nodes yet
		HealthyNodes: 0,
	})

	// Simulate after fleet started
	m.samples = append(m.samples, MetricSample{
		TotalNodes:   10,
		HealthyNodes: 10,
	})
	m.samples = append(m.samples, MetricSample{
		TotalNodes:   10,
		HealthyNodes: 8,
	})
	m.samples = append(m.samples, MetricSample{
		TotalNodes:   10,
		HealthyNodes: 6,
	})

	summary := m.computeSummary()

	// Bug fix: min_healthy_nodes should be 6, not 0
	// The sample with TotalNodes=0 should be skipped
	if summary.MinHealthyNodes != 6 {
		t.Errorf("MinHealthyNodes = %v, want 6 (should skip startup samples)", summary.MinHealthyNodes)
	}
	if summary.PeakHealthyNodes != 10 {
		t.Errorf("PeakHealthyNodes = %v, want 10", summary.PeakHealthyNodes)
	}
	// Average should be (10+8+6)/3 = 8
	if summary.AvgHealthyNodes != 8.0 {
		t.Errorf("AvgHealthyNodes = %v, want 8.0", summary.AvgHealthyNodes)
	}
}

func TestStressMetrics_ComputeSummary_AllEmptySamples(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewStressMetrics(logger)

	// Only empty samples
	m.samples = append(m.samples, MetricSample{TotalNodes: 0, HealthyNodes: 0})
	m.samples = append(m.samples, MetricSample{TotalNodes: 0, HealthyNodes: 0})

	summary := m.computeSummary()

	// Should not panic and return defaults
	if summary.MinHealthyNodes != 0 {
		t.Errorf("MinHealthyNodes = %v, want 0", summary.MinHealthyNodes)
	}
	if summary.PeakHealthyNodes != 0 {
		t.Errorf("PeakHealthyNodes = %v, want 0", summary.PeakHealthyNodes)
	}
	if summary.AvgHealthyNodes != 0.0 {
		t.Errorf("AvgHealthyNodes = %v, want 0.0", summary.AvgHealthyNodes)
	}
}

func TestStressMetrics_ComputeSummary_NoSamples(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewStressMetrics(logger)

	// No samples at all
	summary := m.computeSummary()

	// Should not panic and return defaults
	if summary.MinHealthyNodes != 0 {
		t.Errorf("MinHealthyNodes = %v, want 0", summary.MinHealthyNodes)
	}
}

func TestStressMetrics_RecordFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewStressMetrics(logger)

	// Record a non-cascading failure
	m.RecordFailure(FailureEvent{
		NodeID:    "node-1",
		Type:      "xid_error",
		XIDCode:   79,
		IsCascade: false,
	})

	stats := m.GetCurrentStats()
	if stats["total_failures"].(int64) != 1 {
		t.Errorf("total_failures = %v, want 1", stats["total_failures"])
	}
	if stats["cascading"].(int64) != 0 {
		t.Errorf("cascading = %v, want 0", stats["cascading"])
	}

	// Record a cascading failure
	m.RecordFailure(FailureEvent{
		NodeID:    "node-2",
		Type:      "xid_error",
		XIDCode:   31,
		IsCascade: true,
	})

	stats = m.GetCurrentStats()
	if stats["total_failures"].(int64) != 2 {
		t.Errorf("total_failures = %v, want 2", stats["total_failures"])
	}
	if stats["cascading"].(int64) != 1 {
		t.Errorf("cascading = %v, want 1", stats["cascading"])
	}
}

func TestStressMetrics_RecordRecovery(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewStressMetrics(logger)

	m.RecordRecovery("node-1", "xid_error")
	m.RecordRecovery("node-2", "temperature")

	stats := m.GetCurrentStats()
	if stats["recoveries"].(int64) != 2 {
		t.Errorf("recoveries = %v, want 2", stats["recoveries"])
	}
}

func TestStressMetrics_FailureReport(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewStressMetrics(logger)

	// Record various failures
	m.RecordFailure(FailureEvent{Type: "xid_error", XIDCode: 79})
	m.RecordFailure(FailureEvent{Type: "xid_error", XIDCode: 79})
	m.RecordFailure(FailureEvent{Type: "xid_error", XIDCode: 31})
	m.RecordFailure(FailureEvent{Type: "temperature"})
	m.RecordFailure(FailureEvent{Type: "xid_error", XIDCode: 79, IsCascade: true})

	report := m.computeFailureReport()

	if report.ByType["xid_error"] != 4 {
		t.Errorf("ByType[xid_error] = %v, want 4", report.ByType["xid_error"])
	}
	if report.ByType["temperature"] != 1 {
		t.Errorf("ByType[temperature] = %v, want 1", report.ByType["temperature"])
	}
	if report.ByXID[79] != 3 {
		t.Errorf("ByXID[79] = %v, want 3", report.ByXID[79])
	}
	if report.ByXID[31] != 1 {
		t.Errorf("ByXID[31] = %v, want 1", report.ByXID[31])
	}
	if report.Cascading != 1 {
		t.Errorf("Cascading = %v, want 1", report.Cascading)
	}
}
