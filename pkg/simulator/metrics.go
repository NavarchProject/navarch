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

	"github.com/NavarchProject/navarch/pkg/clock"
)

// StressMetrics collects metrics during stress testing.
type StressMetrics struct {
	mu        sync.RWMutex
	logger    *slog.Logger
	clock     clock.Clock
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
	samples      []MetricSample
	latencySum   int64
	latencyCount int64
	maxLatency   int64

	// Per-node tracking
	nodeStatus          map[string]string
	nodeEvents          map[string][]NodeEvent
	nodeSpecs           map[string]NodeSpec
	nodeColdStartDelays map[string]time.Duration
}

// NodeEvent represents an event that occurred on a specific node.
type NodeEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"`
	Status      string    `json:"status,omitempty"`
	Message     string    `json:"message,omitempty"`
	FailureType string    `json:"failure_type,omitempty"`
	XIDCode     int       `json:"xid_code,omitempty"`
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
	Name          string         `json:"name"`
	StartTime     time.Time      `json:"start_time"`
	EndTime       time.Time      `json:"end_time"`
	Duration      time.Duration  `json:"duration"`
	Configuration ReportConfig   `json:"configuration"`
	Summary       ReportSummary  `json:"summary"`
	Failures      FailureReport  `json:"failures"`
	Timeline      []MetricSample `json:"timeline"`
	Nodes         []NodeReport   `json:"nodes"`
	LogsDirectory string         `json:"logs_directory,omitempty"`
}

// NodeReport contains per-node statistics and event history.
type NodeReport struct {
	NodeID         string        `json:"node_id"`
	Provider       string        `json:"provider"`
	Region         string        `json:"region"`
	Zone           string        `json:"zone"`
	InstanceType   string        `json:"instance_type"`
	GPUCount       int           `json:"gpu_count"`
	GPUType        string        `json:"gpu_type"`
	Status         string        `json:"status"`
	FailureCount   int           `json:"failure_count"`
	RecoveryCount  int           `json:"recovery_count"`
	ColdStartDelay time.Duration `json:"cold_start_delay,omitempty"`
	Events         []NodeEvent   `json:"events"`
	LogFile        string        `json:"log_file,omitempty"`
}

// ReportConfig summarizes the stress test configuration.
type ReportConfig struct {
	TotalNodes       int     `json:"total_nodes"`
	FailureRate      float64 `json:"failure_rate_per_min"`
	CascadingEnabled bool    `json:"cascading_enabled"`
	RecoveryEnabled  bool    `json:"recovery_enabled"`
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
	ByType      map[string]int64 `json:"by_type"`
	ByXID       map[int]int64    `json:"by_xid"`
	Cascading   int64            `json:"cascading_failures"`
	TopXIDCodes []XIDCount       `json:"top_xid_codes"`
}

// XIDCount pairs an XID code with its occurrence count.
type XIDCount struct {
	Code  int    `json:"code"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
	Fatal bool   `json:"fatal"`
}

// NewStressMetrics creates a new metrics collector.
func NewStressMetrics(clk clock.Clock, logger *slog.Logger) *StressMetrics {
	if clk == nil {
		clk = clock.Real()
	}
	return &StressMetrics{
		logger:              logger.With(slog.String("component", "stress-metrics")),
		clock:               clk,
		startTime:           clk.Now(),
		failuresByType:      make(map[string]int64),
		failuresByXID:       make(map[int]int64),
		nodeStatus:          make(map[string]string),
		nodeEvents:          make(map[string][]NodeEvent),
		nodeSpecs:           make(map[string]NodeSpec),
		nodeColdStartDelays: make(map[string]time.Duration),
		samples:             make([]MetricSample, 0, 1000),
	}
}

// RegisterNode records node specification for later reporting.
func (m *StressMetrics) RegisterNode(spec NodeSpec) {
	m.mu.Lock()
	m.nodeSpecs[spec.ID] = spec
	m.mu.Unlock()
}

// RecordColdStartDelay records the cold start delay for a node.
func (m *StressMetrics) RecordColdStartDelay(nodeID string, delay time.Duration) {
	m.mu.Lock()
	m.nodeColdStartDelays[nodeID] = delay
	m.mu.Unlock()
}

// RecordNodeStart records a node starting.
func (m *StressMetrics) RecordNodeStart(nodeID string) {
	atomic.AddInt64(&m.nodesStarted, 1)
	m.mu.Lock()
	m.nodeStatus[nodeID] = "healthy"
	m.nodeEvents[nodeID] = append(m.nodeEvents[nodeID], NodeEvent{
		Timestamp: m.clock.Now(),
		Type:      "started",
		Status:    "healthy",
		Message:   "Node started successfully",
	})
	m.mu.Unlock()
	atomic.AddInt64(&m.nodesHealthy, 1)
}

// RecordNodeFailedStart records a node that failed to start.
func (m *StressMetrics) RecordNodeFailedStart(nodeID string) {
	atomic.AddInt64(&m.nodesFailed, 1)
	m.mu.Lock()
	m.nodeEvents[nodeID] = append(m.nodeEvents[nodeID], NodeEvent{
		Timestamp: m.clock.Now(),
		Type:      "start_failed",
		Message:   "Node failed to start",
	})
	m.mu.Unlock()
}

// RecordNodeHealth updates node health status.
func (m *StressMetrics) RecordNodeHealth(nodeID, status string) {
	m.mu.Lock()
	oldStatus := m.nodeStatus[nodeID]
	m.nodeStatus[nodeID] = status
	if oldStatus != status {
		m.nodeEvents[nodeID] = append(m.nodeEvents[nodeID], NodeEvent{
			Timestamp: m.clock.Now(),
			Type:      "status_change",
			Status:    status,
			Message:   fmt.Sprintf("Status changed from %s to %s", oldStatus, status),
		})
	}
	m.mu.Unlock()

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

	message := fmt.Sprintf("Failure: %s", event.Type)
	if event.IsCascade {
		message += " (cascading)"
	}
	if event.Type == "xid_error" && event.XIDCode > 0 {
		if info, known := XIDCodes[event.XIDCode]; known {
			message = fmt.Sprintf("XID %d: %s", event.XIDCode, info.Name)
		} else {
			message = fmt.Sprintf("XID %d", event.XIDCode)
		}
	}

	m.nodeEvents[event.NodeID] = append(m.nodeEvents[event.NodeID], NodeEvent{
		Timestamp:   m.clock.Now(),
		Type:        "failure",
		FailureType: event.Type,
		XIDCode:     event.XIDCode,
		Message:     message,
	})
	m.mu.Unlock()
}

// RecordRecovery records a node recovery.
func (m *StressMetrics) RecordRecovery(nodeID, failureType string) {
	atomic.AddInt64(&m.recoveries, 1)

	m.mu.Lock()
	m.nodeEvents[nodeID] = append(m.nodeEvents[nodeID], NodeEvent{
		Timestamp:   m.clock.Now(),
		Type:        "recovery",
		FailureType: failureType,
		Message:     fmt.Sprintf("Recovered from %s", failureType),
	})
	m.mu.Unlock()
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
		Timestamp:       m.clock.Now(),
		ElapsedSeconds:  m.clock.Since(m.startTime).Seconds(),
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

	ticker := m.clock.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
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
		EndTime:   m.clock.Now(),
		Duration:  m.clock.Since(m.startTime),
		Timeline:  m.samples,
	}

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

	report.Summary = m.computeSummary()
	report.Failures = m.computeFailureReport()
	report.Nodes = m.computeNodeReports()

	return report
}

// computeNodeReports generates per-node reports.
func (m *StressMetrics) computeNodeReports() []NodeReport {
	var reports []NodeReport

	for nodeID, spec := range m.nodeSpecs {
		events := m.nodeEvents[nodeID]
		status := m.nodeStatus[nodeID]
		failureCount := 0
		recoveryCount := 0
		for _, event := range events {
			if event.Type == "failure" {
				failureCount++
			} else if event.Type == "recovery" {
				recoveryCount++
			}
		}

		reports = append(reports, NodeReport{
			NodeID:         nodeID,
			Provider:       spec.Provider,
			Region:         spec.Region,
			Zone:           spec.Zone,
			InstanceType:   spec.InstanceType,
			GPUCount:       spec.GPUCount,
			GPUType:        spec.GPUType,
			Status:         status,
			FailureCount:   failureCount,
			RecoveryCount:  recoveryCount,
			ColdStartDelay: m.nodeColdStartDelays[nodeID],
			Events:         events,
		})
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].NodeID < reports[j].NodeID
	})

	return reports
}

func (m *StressMetrics) computeSummary() ReportSummary {
	summary := ReportSummary{
		NodesStarted:    atomic.LoadInt64(&m.nodesStarted),
		NodesFailed:     atomic.LoadInt64(&m.nodesFailed),
		TotalFailures:   atomic.LoadInt64(&m.totalFailures),
		TotalRecoveries: atomic.LoadInt64(&m.recoveries),
		TotalOutages:    int(atomic.LoadInt64(&m.outages)),
	}

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

// WriteReport writes the report to a file (JSON format).
func (m *StressMetrics) WriteReport(report *StressReport, filename string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write report: %w", err)
	}

	m.logger.Info("JSON report written", slog.String("file", filename))
	return nil
}

// WriteHTMLReport writes an HTML visual report.
func (m *StressMetrics) WriteHTMLReport(report *StressReport, config *StressConfig, filename string) error {
	generator := NewHTMLReportGenerator(report, config)
	if err := generator.Generate(filename); err != nil {
		return err
	}

	m.logger.Info("HTML report written", slog.String("file", filename))
	return nil
}

// GetCurrentStats returns current statistics as a map.
func (m *StressMetrics) GetCurrentStats() map[string]interface{} {
	return map[string]interface{}{
		"elapsed":         m.clock.Since(m.startTime).String(),
		"nodes_started":   atomic.LoadInt64(&m.nodesStarted),
		"nodes_healthy":   atomic.LoadInt64(&m.nodesHealthy),
		"nodes_unhealthy": atomic.LoadInt64(&m.nodesUnhealthy),
		"nodes_degraded":  atomic.LoadInt64(&m.nodesDegraded),
		"total_failures":  atomic.LoadInt64(&m.totalFailures),
		"cascading":       atomic.LoadInt64(&m.cascadingFailures),
		"recoveries":      atomic.LoadInt64(&m.recoveries),
	}
}

// GetStressResults returns current statistics in the StressResults format for console output.
func (m *StressMetrics) GetStressResults() *StressResults {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Copy maps to avoid race conditions
	failuresByType := make(map[string]int64)
	for k, v := range m.failuresByType {
		failuresByType[k] = v
	}
	failuresByXID := make(map[int]int64)
	for k, v := range m.failuresByXID {
		failuresByXID[k] = v
	}

	return &StressResults{
		Duration:          m.clock.Since(m.startTime),
		NodesStarted:      atomic.LoadInt64(&m.nodesStarted),
		NodesFailed:       atomic.LoadInt64(&m.nodesFailed),
		NodesHealthy:      atomic.LoadInt64(&m.nodesHealthy),
		NodesUnhealthy:    atomic.LoadInt64(&m.nodesUnhealthy),
		NodesDegraded:     atomic.LoadInt64(&m.nodesDegraded),
		TotalFailures:     atomic.LoadInt64(&m.totalFailures),
		CascadingFailures: atomic.LoadInt64(&m.cascadingFailures),
		Recoveries:        atomic.LoadInt64(&m.recoveries),
		FailuresByType:    failuresByType,
		FailuresByXID:     failuresByXID,
	}
}
