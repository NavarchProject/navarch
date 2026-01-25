package simulator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// StressMetrics collects metrics during stress testing.
type StressMetrics struct {
	mu        sync.RWMutex
	logger    *slog.Logger
	startTime time.Time

	// Node metrics
	nodesStarted   int64
	nodesFailed    int64
	nodesHealthy   int64
	nodesUnhealthy int64
	nodesDegraded  int64

	// Failure metrics
	totalFailures     int64
	cascadingFailures int64
	recoveries        int64
	outages           int64

	// Failure breakdown by type
	failuresByType map[string]int64

	// XID breakdown
	failuresByXID map[int]int64

	// Timing metrics
	samples         []MetricSample
	latencySum      int64
	latencyCount    int64
	maxLatency      int64

	// Per-node tracking
	nodeStatus map[string]string
}

// MetricSample represents a point-in-time metric snapshot.
type MetricSample struct {
	Timestamp       time.Time `json:"timestamp"`
	ElapsedSeconds  float64   `json:"elapsed_seconds"`
	TotalNodes      int       `json:"total_nodes"`
	HealthyNodes    int       `json:"healthy_nodes"`
	UnhealthyNodes  int       `json:"unhealthy_nodes"`
	DegradedNodes   int       `json:"degraded_nodes"`
	FailuresTotal   int64     `json:"failures_total"`
	RecoveriesTotal int64     `json:"recoveries_total"`
	ActiveOutages   int       `json:"active_outages"`
}

// StressReport is the final stress test report.
type StressReport struct {
	Name          string        `json:"name"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	Duration      time.Duration `json:"duration"`
	Configuration ReportConfig  `json:"configuration"`
	Summary       ReportSummary `json:"summary"`
	Failures      FailureReport `json:"failures"`
	Timeline      []MetricSample `json:"timeline"`
}

// ReportConfig summarizes the stress test configuration.
type ReportConfig struct {
	TotalNodes      int     `json:"total_nodes"`
	FailureRate     float64 `json:"failure_rate_per_min"`
	CascadingEnabled bool   `json:"cascading_enabled"`
	RecoveryEnabled  bool   `json:"recovery_enabled"`
}

// ReportSummary provides high-level statistics.
type ReportSummary struct {
	NodesStarted     int64   `json:"nodes_started"`
	NodesFailed      int64   `json:"nodes_failed_to_start"`
	PeakHealthyNodes int     `json:"peak_healthy_nodes"`
	MinHealthyNodes  int     `json:"min_healthy_nodes"`
	AvgHealthyNodes  float64 `json:"avg_healthy_nodes"`
	TotalFailures    int64   `json:"total_failures"`
	TotalRecoveries  int64   `json:"total_recoveries"`
	TotalOutages     int     `json:"total_outages"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	MaxLatencyMs     float64 `json:"max_latency_ms"`
}

// FailureReport breaks down failures by type.
type FailureReport struct {
	ByType       map[string]int64 `json:"by_type"`
	ByXID        map[int]int64    `json:"by_xid"`
	Cascading    int64            `json:"cascading_failures"`
	TopXIDCodes  []XIDCount       `json:"top_xid_codes"`
}

// XIDCount pairs an XID code with its occurrence count.
type XIDCount struct {
	Code  int   `json:"code"`
	Name  string `json:"name"`
	Count int64 `json:"count"`
	Fatal bool  `json:"fatal"`
}

// NewStressMetrics creates a new metrics collector.
func NewStressMetrics(logger *slog.Logger) *StressMetrics {
	return &StressMetrics{
		logger:         logger.With(slog.String("component", "stress-metrics")),
		startTime:      time.Now(),
		failuresByType: make(map[string]int64),
		failuresByXID:  make(map[int]int64),
		nodeStatus:     make(map[string]string),
		samples:        make([]MetricSample, 0, 1000),
	}
}

// RecordNodeStart records a node starting.
func (m *StressMetrics) RecordNodeStart(nodeID string) {
	atomic.AddInt64(&m.nodesStarted, 1)
	m.mu.Lock()
	m.nodeStatus[nodeID] = "healthy"
	m.mu.Unlock()
	atomic.AddInt64(&m.nodesHealthy, 1)
}

// RecordNodeFailedStart records a node that failed to start.
func (m *StressMetrics) RecordNodeFailedStart(nodeID string) {
	atomic.AddInt64(&m.nodesFailed, 1)
}

// RecordNodeHealth updates node health status.
func (m *StressMetrics) RecordNodeHealth(nodeID, status string) {
	m.mu.Lock()
	oldStatus := m.nodeStatus[nodeID]
	m.nodeStatus[nodeID] = status
	m.mu.Unlock()

	// Update counters
	if oldStatus != status {
		switch oldStatus {
		case "healthy":
			atomic.AddInt64(&m.nodesHealthy, -1)
		case "unhealthy":
			atomic.AddInt64(&m.nodesUnhealthy, -1)
		case "degraded":
			atomic.AddInt64(&m.nodesDegraded, -1)
		}

		switch status {
		case "healthy":
			atomic.AddInt64(&m.nodesHealthy, 1)
		case "unhealthy":
			atomic.AddInt64(&m.nodesUnhealthy, 1)
		case "degraded":
			atomic.AddInt64(&m.nodesDegraded, 1)
		}
	}
}

// RecordFailure records a failure event.
func (m *StressMetrics) RecordFailure(event FailureEvent) {
	atomic.AddInt64(&m.totalFailures, 1)

	if event.IsCascade {
		atomic.AddInt64(&m.cascadingFailures, 1)
	}

	m.mu.Lock()
	m.failuresByType[event.Type]++
	if event.Type == "xid_error" {
		m.failuresByXID[event.XIDCode]++
	}
	m.mu.Unlock()
}

// RecordRecovery records a node recovery.
func (m *StressMetrics) RecordRecovery(nodeID, failureType string) {
	atomic.AddInt64(&m.recoveries, 1)
}

// RecordOutage records an outage event.
func (m *StressMetrics) RecordOutage(name string, affectedNodes int) {
	atomic.AddInt64(&m.outages, 1)
	m.logger.Info("outage recorded",
		slog.String("name", name),
		slog.Int("affected_nodes", affectedNodes),
	)
}

// RecordLatency records an operation latency.
func (m *StressMetrics) RecordLatency(d time.Duration) {
	ms := d.Milliseconds()
	atomic.AddInt64(&m.latencySum, ms)
	atomic.AddInt64(&m.latencyCount, 1)

	// Update max (simple CAS loop)
	for {
		current := atomic.LoadInt64(&m.maxLatency)
		if ms <= current {
			break
		}
		if atomic.CompareAndSwapInt64(&m.maxLatency, current, ms) {
			break
		}
	}
}

// TakeSample captures current metrics state.
func (m *StressMetrics) TakeSample() {
	sample := MetricSample{
		Timestamp:       time.Now(),
		ElapsedSeconds:  time.Since(m.startTime).Seconds(),
		TotalNodes:      int(atomic.LoadInt64(&m.nodesStarted) - atomic.LoadInt64(&m.nodesFailed)),
		HealthyNodes:    int(atomic.LoadInt64(&m.nodesHealthy)),
		UnhealthyNodes:  int(atomic.LoadInt64(&m.nodesUnhealthy)),
		DegradedNodes:   int(atomic.LoadInt64(&m.nodesDegraded)),
		FailuresTotal:   atomic.LoadInt64(&m.totalFailures),
		RecoveriesTotal: atomic.LoadInt64(&m.recoveries),
		ActiveOutages:   int(atomic.LoadInt64(&m.outages)),
	}

	m.mu.Lock()
	m.samples = append(m.samples, sample)
	m.mu.Unlock()
}

// StartSampling begins periodic metric sampling.
func (m *StressMetrics) StartSampling(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.TakeSample()
		}
	}
}

// GenerateReport creates the final stress test report.
func (m *StressMetrics) GenerateReport(name string, config *StressConfig) *StressReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	report := &StressReport{
		Name:      name,
		StartTime: m.startTime,
		EndTime:   time.Now(),
		Duration:  time.Since(m.startTime),
		Timeline:  m.samples,
	}

	// Configuration summary
	if config != nil && config.FleetGen != nil {
		report.Configuration = ReportConfig{
			TotalNodes:  config.FleetGen.TotalNodes,
			FailureRate: 0,
		}
		if config.Chaos != nil {
			report.Configuration.FailureRate = config.Chaos.FailureRate
			report.Configuration.CascadingEnabled = config.Chaos.Cascading != nil && config.Chaos.Cascading.Enabled
			report.Configuration.RecoveryEnabled = config.Chaos.Recovery != nil && config.Chaos.Recovery.Enabled
		}
	}

	// Summary statistics
	report.Summary = m.computeSummary()

	// Failure breakdown
	report.Failures = m.computeFailureReport()

	return report
}

func (m *StressMetrics) computeSummary() ReportSummary {
	summary := ReportSummary{
		NodesStarted:    atomic.LoadInt64(&m.nodesStarted),
		NodesFailed:     atomic.LoadInt64(&m.nodesFailed),
		TotalFailures:   atomic.LoadInt64(&m.totalFailures),
		TotalRecoveries: atomic.LoadInt64(&m.recoveries),
		TotalOutages:    int(atomic.LoadInt64(&m.outages)),
	}

	// Compute min/max/avg healthy nodes from samples
	if len(m.samples) > 0 {
		summary.PeakHealthyNodes = m.samples[0].HealthyNodes
		summary.MinHealthyNodes = m.samples[0].HealthyNodes
		totalHealthy := 0

		for _, s := range m.samples {
			if s.HealthyNodes > summary.PeakHealthyNodes {
				summary.PeakHealthyNodes = s.HealthyNodes
			}
			if s.HealthyNodes < summary.MinHealthyNodes {
				summary.MinHealthyNodes = s.HealthyNodes
			}
			totalHealthy += s.HealthyNodes
		}
		summary.AvgHealthyNodes = float64(totalHealthy) / float64(len(m.samples))
	}

	// Latency stats
	count := atomic.LoadInt64(&m.latencyCount)
	if count > 0 {
		summary.AvgLatencyMs = float64(atomic.LoadInt64(&m.latencySum)) / float64(count)
		summary.MaxLatencyMs = float64(atomic.LoadInt64(&m.maxLatency))
	}

	return summary
}

func (m *StressMetrics) computeFailureReport() FailureReport {
	report := FailureReport{
		ByType:    make(map[string]int64),
		ByXID:     make(map[int]int64),
		Cascading: atomic.LoadInt64(&m.cascadingFailures),
	}

	for k, v := range m.failuresByType {
		report.ByType[k] = v
	}

	for k, v := range m.failuresByXID {
		report.ByXID[k] = v
	}

	// Top XID codes
	type xidEntry struct {
		code  int
		count int64
	}
	var entries []xidEntry
	for code, count := range m.failuresByXID {
		entries = append(entries, xidEntry{code, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	for i := 0; i < len(entries) && i < 10; i++ {
		info, known := XIDCodes[entries[i].code]
		name := "Unknown"
		fatal := false
		if known {
			name = info.Name
			fatal = info.Fatal
		}
		report.TopXIDCodes = append(report.TopXIDCodes, XIDCount{
			Code:  entries[i].code,
			Name:  name,
			Count: entries[i].count,
			Fatal: fatal,
		})
	}

	return report
}

// WriteReport writes the report to a file.
func (m *StressMetrics) WriteReport(report *StressReport, filename string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write report: %w", err)
	}

	m.logger.Info("report written", slog.String("file", filename))
	return nil
}

// PrintSummary prints a summary to the logger.
func (m *StressMetrics) PrintSummary() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.logger.Info("=== STRESS TEST SUMMARY ===")
	m.logger.Info("Duration", slog.Duration("duration", time.Since(m.startTime)))
	m.logger.Info("Nodes",
		slog.Int64("started", atomic.LoadInt64(&m.nodesStarted)),
		slog.Int64("failed_start", atomic.LoadInt64(&m.nodesFailed)),
		slog.Int64("healthy", atomic.LoadInt64(&m.nodesHealthy)),
		slog.Int64("unhealthy", atomic.LoadInt64(&m.nodesUnhealthy)),
		slog.Int64("degraded", atomic.LoadInt64(&m.nodesDegraded)),
	)
	m.logger.Info("Failures",
		slog.Int64("total", atomic.LoadInt64(&m.totalFailures)),
		slog.Int64("cascading", atomic.LoadInt64(&m.cascadingFailures)),
		slog.Int64("recoveries", atomic.LoadInt64(&m.recoveries)),
	)

	// Top failure types
	m.logger.Info("Failures by type:")
	for ftype, count := range m.failuresByType {
		m.logger.Info("  "+ftype, slog.Int64("count", count))
	}

	// Top XID codes
	if len(m.failuresByXID) > 0 {
		m.logger.Info("Top XID codes:")
		type xidEntry struct {
			code  int
			count int64
		}
		var entries []xidEntry
		for code, count := range m.failuresByXID {
			entries = append(entries, xidEntry{code, count})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].count > entries[j].count
		})
		for i := 0; i < len(entries) && i < 5; i++ {
			info, known := XIDCodes[entries[i].code]
			name := "Unknown"
			if known {
				name = info.Name
			}
			m.logger.Info(fmt.Sprintf("  XID %d: %s", entries[i].code, name),
				slog.Int64("count", entries[i].count),
			)
		}
	}
}

// GetCurrentStats returns current statistics as a map.
func (m *StressMetrics) GetCurrentStats() map[string]interface{} {
	return map[string]interface{}{
		"elapsed":          time.Since(m.startTime).String(),
		"nodes_started":    atomic.LoadInt64(&m.nodesStarted),
		"nodes_healthy":    atomic.LoadInt64(&m.nodesHealthy),
		"nodes_unhealthy":  atomic.LoadInt64(&m.nodesUnhealthy),
		"nodes_degraded":   atomic.LoadInt64(&m.nodesDegraded),
		"total_failures":   atomic.LoadInt64(&m.totalFailures),
		"cascading":        atomic.LoadInt64(&m.cascadingFailures),
		"recoveries":       atomic.LoadInt64(&m.recoveries),
	}
}
