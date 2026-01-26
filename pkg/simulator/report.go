package simulator

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HTMLReportGenerator generates visual HTML reports from stress test results.
type HTMLReportGenerator struct {
	report *StressReport
	config *StressConfig
}

// NewHTMLReportGenerator creates a new HTML report generator.
func NewHTMLReportGenerator(report *StressReport, config *StressConfig) *HTMLReportGenerator {
	return &HTMLReportGenerator{report: report, config: config}
}

// Generate creates an HTML report file.
func (g *HTMLReportGenerator) Generate(outputPath string) error {
	// Ensure .html extension
	if !strings.HasSuffix(outputPath, ".html") {
		outputPath = strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".html"
	}

	funcMap := template.FuncMap{
		"mul": func(a, b float64) float64 { return a * b },
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(htmlReportTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	data := g.prepareTemplateData()
	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}

type templateData struct {
	Name              string
	StartTime         string
	EndTime           string
	Duration          string
	TotalNodes        int
	FailureRate       float64
	CascadingEnabled  bool
	RecoveryEnabled   bool
	NodesStarted      int64
	NodesFailed       int64
	PeakHealthy       int
	MinHealthy        int
	AvgHealthy        float64
	TotalFailures     int64
	TotalRecoveries   int64
	CascadingFailures int64
	TotalOutages      int
	AvgLatencyMs      float64
	MaxLatencyMs      float64

	// Chart data (template.JS for safe JavaScript embedding)
	TimelineLabels    template.JS
	HealthyData       template.JS
	UnhealthyData     template.JS
	DegradedData      template.JS
	FailuresData      template.JS
	RecoveriesData    template.JS
	FailureTypeLabels template.JS
	FailureTypeData   template.JS
	XIDLabels         template.JS
	XIDData           template.JS
	TopXIDCodes       []XIDCount

	// Full configuration details
	HasConfig       bool
	TestDuration    string
	MetricsInterval string
	Seed            int64

	// Fleet generation config
	HasFleetGen      bool
	FleetTemplates   []fleetTemplateData
	FleetProviders   []kvPair
	FleetRegions     []kvPair
	StartupPattern   string
	StartupDuration  string
	StartupBatchSize int
	StartupJitter    int

	// Chaos config
	HasChaos           bool
	ChaosEnabled       bool
	XIDDistribution    []xidDistData
	FailureTypeWeights []kvPair

	// Cascading config
	HasCascading       bool
	CascadeProbability float64
	CascadeMaxDepth    int
	CascadeMinDelay    string
	CascadeMaxDelay    string
	CascadeScope       string
	CascadeMaxAffected float64

	// Recovery config
	HasRecovery         bool
	RecoveryProbability float64
	RecoveryMeanTime    string
	RecoveryStdDev      string

	// Scheduled outages
	ScheduledOutages []outageData

	// Chart data for config visualization
	XIDDistLabels  template.JS
	XIDDistData    template.JS
	ProviderLabels template.JS
	ProviderData   template.JS

	// Node data
	NodeReports   []NodeReport
	NodesJSON     template.JS
	LogsDirectory string
}

type fleetTemplateData struct {
	Name         string
	Weight       int
	GPUCount     int
	GPUType      string
	InstanceType string
}

type kvPair struct {
	Key   string
	Value int
}

type xidDistData struct {
	Code   int
	Name   string
	Weight int
	Fatal  bool
}

type outageData struct {
	Name        string
	StartTime   string
	Duration    string
	Scope       string
	Target      string
	FailureType string
}

func (g *HTMLReportGenerator) prepareTemplateData() templateData {
	r := g.report
	c := g.config

	data := templateData{
		Name:              r.Name,
		StartTime:         r.StartTime.Format(time.RFC3339),
		EndTime:           r.EndTime.Format(time.RFC3339),
		Duration:          r.Duration.Round(time.Second).String(),
		TotalNodes:        r.Configuration.TotalNodes,
		FailureRate:       r.Configuration.FailureRate,
		CascadingEnabled:  r.Configuration.CascadingEnabled,
		RecoveryEnabled:   r.Configuration.RecoveryEnabled,
		NodesStarted:      r.Summary.NodesStarted,
		NodesFailed:       r.Summary.NodesFailed,
		PeakHealthy:       r.Summary.PeakHealthyNodes,
		MinHealthy:        r.Summary.MinHealthyNodes,
		AvgHealthy:        r.Summary.AvgHealthyNodes,
		TotalFailures:     r.Summary.TotalFailures,
		TotalRecoveries:   r.Summary.TotalRecoveries,
		CascadingFailures: r.Failures.Cascading,
		TotalOutages:      r.Summary.TotalOutages,
		AvgLatencyMs:      r.Summary.AvgLatencyMs,
		MaxLatencyMs:      r.Summary.MaxLatencyMs,
		TopXIDCodes:       r.Failures.TopXIDCodes,
	}

	// Prepare timeline data
	var labels []string
	var healthy, unhealthy, degraded, failures, recoveries []int

	for _, sample := range r.Timeline {
		labels = append(labels, fmt.Sprintf("%.0fs", sample.ElapsedSeconds))
		healthy = append(healthy, sample.HealthyNodes)
		unhealthy = append(unhealthy, sample.UnhealthyNodes)
		degraded = append(degraded, sample.DegradedNodes)
		failures = append(failures, int(sample.FailuresTotal))
		recoveries = append(recoveries, int(sample.RecoveriesTotal))
	}

	data.TimelineLabels = toJSArray(labels)
	data.HealthyData = toJSArray(healthy)
	data.UnhealthyData = toJSArray(unhealthy)
	data.DegradedData = toJSArray(degraded)
	data.FailuresData = toJSArray(failures)
	data.RecoveriesData = toJSArray(recoveries)

	// Prepare failure type breakdown
	var ftLabels []string
	var ftData []int64
	for ftype, count := range r.Failures.ByType {
		ftLabels = append(ftLabels, ftype)
		ftData = append(ftData, count)
	}
	data.FailureTypeLabels = toJSArray(ftLabels)
	data.FailureTypeData = toJSArray(ftData)

	// Prepare XID breakdown
	var xidLabels []string
	var xidData []int64
	for code, count := range r.Failures.ByXID {
		info, known := XIDCodes[code]
		label := fmt.Sprintf("XID %d", code)
		if known {
			label = fmt.Sprintf("%d: %s", code, truncate(info.Name, 20))
		}
		xidLabels = append(xidLabels, label)
		xidData = append(xidData, count)
	}
	data.XIDLabels = toJSArray(xidLabels)
	data.XIDData = toJSArray(xidData)

	// Populate full configuration details
	if c != nil {
		data.HasConfig = true
		data.TestDuration = c.Duration.Duration().String()
		data.MetricsInterval = c.MetricsInterval.Duration().String()
		data.Seed = c.Seed

		// Fleet generation config
		if c.FleetGen != nil {
			data.HasFleetGen = true
			for _, t := range c.FleetGen.Templates {
				data.FleetTemplates = append(data.FleetTemplates, fleetTemplateData{
					Name:         t.Name,
					Weight:       t.Weight,
					GPUCount:     t.GPUCount,
					GPUType:      t.GPUType,
					InstanceType: t.InstanceType,
				})
			}
			for p, pct := range c.FleetGen.Providers {
				data.FleetProviders = append(data.FleetProviders, kvPair{Key: p, Value: pct})
			}
			for r, pct := range c.FleetGen.Regions {
				data.FleetRegions = append(data.FleetRegions, kvPair{Key: r, Value: pct})
			}
			data.StartupPattern = c.FleetGen.Startup.Pattern
			if data.StartupPattern == "" {
				data.StartupPattern = "linear"
			}
			data.StartupDuration = c.FleetGen.Startup.Duration.Duration().String()
			data.StartupBatchSize = c.FleetGen.Startup.BatchSize
			data.StartupJitter = c.FleetGen.Startup.JitterPercent

			// Provider chart data
			var provLabels []string
			var provData []int
			for p, pct := range c.FleetGen.Providers {
				provLabels = append(provLabels, p)
				provData = append(provData, pct)
			}
			data.ProviderLabels = toJSArray(provLabels)
			data.ProviderData = toJSArray(provData)
		}

		// Chaos config
		if c.Chaos != nil {
			data.HasChaos = true
			data.ChaosEnabled = c.Chaos.Enabled

			// XID distribution
			var xidDistLabels []string
			var xidDistWeights []int
			for code, weight := range c.Chaos.XIDDistribution {
				info, known := XIDCodes[code]
				name := fmt.Sprintf("XID %d", code)
				fatal := false
				if known {
					name = info.Name
					fatal = info.Fatal
				}
				data.XIDDistribution = append(data.XIDDistribution, xidDistData{
					Code:   code,
					Name:   name,
					Weight: weight,
					Fatal:  fatal,
				})
				xidDistLabels = append(xidDistLabels, fmt.Sprintf("%d: %s", code, truncate(name, 15)))
				xidDistWeights = append(xidDistWeights, weight)
			}
			data.XIDDistLabels = toJSArray(xidDistLabels)
			data.XIDDistData = toJSArray(xidDistWeights)

			// Failure type weights
			for _, ft := range c.Chaos.FailureTypes {
				data.FailureTypeWeights = append(data.FailureTypeWeights, kvPair{Key: ft.Type, Value: ft.Weight})
			}

			// Cascading config
			if c.Chaos.Cascading != nil {
				data.HasCascading = true
				data.CascadeProbability = c.Chaos.Cascading.Probability
				data.CascadeMaxDepth = c.Chaos.Cascading.MaxDepth
				data.CascadeMinDelay = c.Chaos.Cascading.MinDelay.Duration().String()
				data.CascadeMaxDelay = c.Chaos.Cascading.MaxDelay.Duration().String()
				data.CascadeScope = c.Chaos.Cascading.Scope
				data.CascadeMaxAffected = c.Chaos.Cascading.MaxAffectedPercent
			}

			// Recovery config
			if c.Chaos.Recovery != nil {
				data.HasRecovery = true
				data.RecoveryProbability = c.Chaos.Recovery.Probability
				data.RecoveryMeanTime = c.Chaos.Recovery.MeanTime.Duration().String()
				data.RecoveryStdDev = c.Chaos.Recovery.StdDev.Duration().String()
			}

			// Scheduled outages
			for _, o := range c.Chaos.ScheduledOutages {
				data.ScheduledOutages = append(data.ScheduledOutages, outageData{
					Name:        o.Name,
					StartTime:   o.StartTime.Duration().String(),
					Duration:    o.Duration.Duration().String(),
					Scope:       o.Scope,
					Target:      o.Target,
					FailureType: o.FailureType,
				})
			}
		}
	}

	// Populate node reports
	data.NodeReports = r.Nodes
	data.NodesJSON = toJSArray(r.Nodes)
	data.LogsDirectory = r.LogsDirectory

	return data
}

func toJSArray(v interface{}) template.JS {
	b, _ := json.Marshal(v)
	return template.JS(b)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

const htmlReportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Stress Test Report: {{.Name}}</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            background: #0d1117;
            color: #c9d1d9;
            line-height: 1.6;
            padding: 20px;
        }
        .container { max-width: 1400px; margin: 0 auto; }
        h1 { color: #58a6ff; margin-bottom: 10px; font-size: 2em; }
        h2 { color: #8b949e; font-size: 1.3em; margin: 30px 0 15px 0; padding-bottom: 10px; border-bottom: 1px solid #21262d; }
        h3 { color: #c9d1d9; font-size: 1.1em; margin: 20px 0 10px 0; }
        .header {
            background: #161b22;
            border-radius: 8px;
            padding: 20px;
            margin-bottom: 20px;
            border: 1px solid #30363d;
        }
        .header-meta {
            display: flex;
            gap: 30px;
            flex-wrap: wrap;
            margin-top: 10px;
            font-size: 0.9em;
            color: #8b949e;
        }
        .header-meta span { display: flex; align-items: center; gap: 5px; }

        /* Tabs */
        .tabs {
            display: flex;
            gap: 0;
            margin-bottom: 20px;
            border-bottom: 1px solid #30363d;
        }
        .tab {
            padding: 12px 24px;
            cursor: pointer;
            color: #8b949e;
            border: none;
            background: none;
            font-size: 1em;
            font-family: inherit;
            border-bottom: 2px solid transparent;
            transition: all 0.2s;
        }
        .tab:hover { color: #c9d1d9; }
        .tab.active {
            color: #58a6ff;
            border-bottom-color: #58a6ff;
        }
        .tab-content { display: none; }
        .tab-content.active { display: block; }

        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 15px;
            margin-bottom: 20px;
        }
        .stat-card {
            background: #161b22;
            border-radius: 8px;
            padding: 20px;
            border: 1px solid #30363d;
        }
        .stat-card.success { border-left: 4px solid #3fb950; }
        .stat-card.warning { border-left: 4px solid #d29922; }
        .stat-card.danger { border-left: 4px solid #f85149; }
        .stat-card.info { border-left: 4px solid #58a6ff; }
        .stat-label { font-size: 0.85em; color: #8b949e; text-transform: uppercase; letter-spacing: 0.5px; }
        .stat-value { font-size: 2em; font-weight: 600; margin-top: 5px; }
        .stat-card.success .stat-value { color: #3fb950; }
        .stat-card.warning .stat-value { color: #d29922; }
        .stat-card.danger .stat-value { color: #f85149; }
        .stat-card.info .stat-value { color: #58a6ff; }

        .charts-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(500px, 1fr));
            gap: 20px;
        }
        .chart-card {
            background: #161b22;
            border-radius: 8px;
            padding: 20px;
            border: 1px solid #30363d;
        }
        .chart-card h3 { color: #c9d1d9; font-size: 1em; margin-bottom: 15px; }
        .chart-container { position: relative; height: 300px; }
        .chart-container.small { height: 200px; }

        .data-table {
            width: 100%;
            border-collapse: collapse;
            margin-top: 15px;
        }
        .data-table th, .data-table td {
            padding: 12px;
            text-align: left;
            border-bottom: 1px solid #21262d;
        }
        .data-table th {
            background: #21262d;
            color: #8b949e;
            font-weight: 500;
            font-size: 0.85em;
            text-transform: uppercase;
        }
        .data-table tr:hover { background: #21262d; }

        .badge {
            display: inline-block;
            padding: 2px 8px;
            border-radius: 12px;
            font-size: 0.75em;
            font-weight: 500;
        }
        .badge.fatal { background: #f8514922; color: #f85149; }
        .badge.recoverable { background: #3fb95022; color: #3fb950; }
        .badge.enabled { background: #3fb95022; color: #3fb950; }
        .badge.disabled { background: #48505822; color: #484f58; }

        .config-section {
            background: #161b22;
            border-radius: 8px;
            padding: 20px;
            border: 1px solid #30363d;
            margin-bottom: 20px;
        }
        .config-section h3 {
            color: #58a6ff;
            margin-bottom: 15px;
            padding-bottom: 10px;
            border-bottom: 1px solid #21262d;
        }
        .config-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 20px;
        }
        .config-item {
            display: flex;
            justify-content: space-between;
            padding: 8px 0;
            border-bottom: 1px solid #21262d;
        }
        .config-item:last-child { border-bottom: none; }
        .config-label { color: #8b949e; }
        .config-value { color: #c9d1d9; font-weight: 500; font-family: monospace; }

        .config-cards {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(350px, 1fr));
            gap: 20px;
        }

        .footer {
            margin-top: 40px;
            padding-top: 20px;
            border-top: 1px solid #21262d;
            text-align: center;
            color: #484f58;
            font-size: 0.85em;
        }

        /* Timeline Track View - Compact */
        .timeline-container {
            background: #161b22;
            border-radius: 8px;
            border: 1px solid #30363d;
            overflow: hidden;
        }
        .timeline-header {
            display: flex;
            background: #21262d;
            border-bottom: 1px solid #30363d;
            position: sticky;
            top: 0;
            z-index: 10;
        }
        .timeline-labels {
            min-width: 140px;
            max-width: 140px;
            padding: 6px 10px;
            font-weight: 500;
            color: #8b949e;
            font-size: 0.75em;
            border-right: 1px solid #30363d;
        }
        .timeline-ruler {
            flex: 1;
            display: flex;
            align-items: center;
            padding: 6px 0;
            overflow-x: auto;
        }
        .timeline-tick {
            flex: 1;
            text-align: center;
            font-size: 0.7em;
            color: #8b949e;
            border-left: 1px solid #30363d;
            padding: 0 2px;
            min-width: 50px;
        }
        .timeline-body {
            max-height: 70vh;
            overflow-y: auto;
        }
        .timeline-row {
            display: flex;
            border-bottom: 1px solid #21262d;
            min-height: 20px;
        }
        .timeline-row:hover {
            background: #21262d66;
        }
        .timeline-row.has-failures {
            background: #f8514908;
        }
        .timeline-node-label {
            min-width: 140px;
            max-width: 140px;
            padding: 2px 8px;
            font-size: 0.7em;
            font-family: monospace;
            color: #8b949e;
            border-right: 1px solid #30363d;
            display: flex;
            align-items: center;
            gap: 5px;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            cursor: pointer;
        }
        .timeline-node-label:hover {
            color: #c9d1d9;
            background: #21262d;
        }
        .timeline-track {
            flex: 1;
            position: relative;
            height: 20px;
        }
        .timeline-segment {
            position: absolute;
            top: 3px;
            height: 14px;
            border-radius: 2px;
        }
        .timeline-segment.healthy { background: #238636; }
        .timeline-segment.degraded { background: #9e6a03; }
        .timeline-segment.unhealthy { background: #da3633; }
        .timeline-segment.pending { background: #484f58; }
        .timeline-event {
            position: absolute;
            top: 50%;
            transform: translate(-50%, -50%);
            width: 8px;
            height: 8px;
            border-radius: 50%;
            border: 1px solid #0d1117;
            cursor: pointer;
            z-index: 5;
        }
        .timeline-event.failure { background: #f85149; }
        .timeline-event.recovery { background: #3fb950; }
        .timeline-event.status_change { background: #58a6ff; }
        .timeline-controls {
            display: flex;
            gap: 10px;
            margin-bottom: 15px;
            align-items: center;
            flex-wrap: wrap;
            font-size: 0.85em;
        }
        .timeline-legend {
            display: flex;
            gap: 15px;
            padding: 8px 12px;
            background: #161b22;
            border-radius: 6px;
            border: 1px solid #30363d;
            margin-bottom: 15px;
            flex-wrap: wrap;
        }
        .legend-item {
            display: flex;
            align-items: center;
            gap: 5px;
            font-size: 0.75em;
            color: #8b949e;
        }
        .legend-color {
            width: 12px;
            height: 12px;
            border-radius: 2px;
        }
        .legend-color.healthy { background: #238636; }
        .legend-color.degraded { background: #9e6a03; }
        .legend-color.unhealthy { background: #da3633; }
        .legend-dot {
            width: 8px;
            height: 8px;
            border-radius: 50%;
        }
        .legend-dot.failure { background: #f85149; }
        .legend-dot.recovery { background: #3fb950; }
        .timeline-tooltip {
            position: absolute;
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 6px;
            padding: 8px 10px;
            font-size: 0.8em;
            color: #c9d1d9;
            z-index: 100;
            pointer-events: none;
            max-width: 280px;
            box-shadow: 0 4px 12px rgba(0,0,0,0.4);
        }
        .zoom-controls {
            display: flex;
            gap: 3px;
        }
        .zoom-btn {
            padding: 4px 10px;
            background: #21262d;
            border: 1px solid #30363d;
            border-radius: 4px;
            color: #c9d1d9;
            cursor: pointer;
            font-size: 0.8em;
        }
        .zoom-btn:hover { background: #30363d; }

        /* Node List - Compact Split View */
        .nodes-container {
            display: flex;
            gap: 15px;
            height: 75vh;
        }
        .nodes-sidebar {
            width: 280px;
            min-width: 280px;
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 8px;
            display: flex;
            flex-direction: column;
        }
        .nodes-sidebar-header {
            padding: 10px;
            border-bottom: 1px solid #30363d;
            background: #21262d;
        }
        .nodes-sidebar-header input {
            width: 100%;
            padding: 6px 10px;
            background: #0d1117;
            border: 1px solid #30363d;
            border-radius: 4px;
            color: #c9d1d9;
            font-size: 0.8em;
        }
        .nodes-sidebar-filters {
            display: flex;
            gap: 5px;
            margin-top: 8px;
        }
        .nodes-sidebar-filters select {
            flex: 1;
            padding: 4px 6px;
            background: #0d1117;
            border: 1px solid #30363d;
            border-radius: 4px;
            color: #c9d1d9;
            font-size: 0.75em;
        }
        .nodes-sidebar-list {
            flex: 1;
            overflow-y: auto;
        }
        .node-list-item {
            display: flex;
            align-items: center;
            padding: 6px 10px;
            cursor: pointer;
            border-bottom: 1px solid #21262d;
            font-size: 0.8em;
            gap: 8px;
        }
        .node-list-item:hover {
            background: #21262d;
        }
        .node-list-item.selected {
            background: #388bfd22;
            border-left: 2px solid #58a6ff;
        }
        .node-list-item .status-dot {
            width: 8px;
            height: 8px;
            border-radius: 50%;
            flex-shrink: 0;
        }
        .node-list-item .status-dot.healthy { background: #3fb950; }
        .node-list-item .status-dot.degraded { background: #d29922; }
        .node-list-item .status-dot.unhealthy { background: #f85149; }
        .node-list-item .node-id {
            flex: 1;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            font-family: monospace;
            color: #8b949e;
        }
        .node-list-item .failure-badge {
            background: #f8514922;
            color: #f85149;
            padding: 1px 5px;
            border-radius: 3px;
            font-size: 0.75em;
            font-weight: 500;
        }
        .nodes-detail {
            flex: 1;
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 8px;
            overflow: hidden;
            display: flex;
            flex-direction: column;
        }
        .nodes-detail-header {
            padding: 15px;
            background: #21262d;
            border-bottom: 1px solid #30363d;
        }
        .nodes-detail-content {
            flex: 1;
            overflow-y: auto;
            padding: 15px;
        }
        .nodes-detail-empty {
            display: flex;
            align-items: center;
            justify-content: center;
            height: 100%;
            color: #8b949e;
            font-size: 0.9em;
        }
        .node-info-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
            gap: 10px;
            margin-bottom: 20px;
        }
        .node-info-item {
            background: #21262d;
            padding: 10px;
            border-radius: 6px;
        }
        .node-info-item label {
            display: block;
            font-size: 0.7em;
            color: #8b949e;
            text-transform: uppercase;
            margin-bottom: 3px;
        }
        .node-info-item span {
            font-family: monospace;
            font-size: 0.85em;
            color: #c9d1d9;
        }
        .node-events-list {
            background: #0d1117;
            border-radius: 6px;
            max-height: 400px;
            overflow-y: auto;
        }
        .node-event-item {
            display: flex;
            align-items: flex-start;
            padding: 8px 10px;
            border-bottom: 1px solid #21262d;
            font-size: 0.8em;
            gap: 10px;
        }
        .node-event-item:last-child { border-bottom: none; }
        .node-event-time {
            color: #8b949e;
            font-size: 0.75em;
            white-space: nowrap;
            min-width: 75px;
        }
        .node-event-icon {
            width: 16px;
            text-align: center;
        }
        .node-event-msg {
            flex: 1;
            color: #c9d1d9;
        }
        .nodes-sidebar-stats {
            padding: 8px 10px;
            background: #21262d;
            border-top: 1px solid #30363d;
            font-size: 0.75em;
            color: #8b949e;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>{{.Name}}</h1>
            <div class="header-meta">
                <span>Start: {{.StartTime}}</span>
                <span>Duration: {{.Duration}}</span>
                <span>{{.TotalNodes}} nodes</span>
                <span>{{.FailureRate}}/min/1000 failure rate</span>
            </div>
        </div>

        <div class="tabs">
            <button class="tab active" onclick="showTab('results')">Results</button>
            <button class="tab" onclick="showTab('timeline')">Timeline</button>
            <button class="tab" onclick="showTab('nodes')">Nodes</button>
            <button class="tab" onclick="showTab('config')">Configuration</button>
        </div>

        <!-- Results Tab -->
        <div id="results" class="tab-content active">
            <h2>Summary Statistics</h2>
            <div class="stats-grid">
                <div class="stat-card success">
                    <div class="stat-label">Nodes Started</div>
                    <div class="stat-value">{{.NodesStarted}}</div>
                </div>
                <div class="stat-card success">
                    <div class="stat-label">Peak Healthy</div>
                    <div class="stat-value">{{.PeakHealthy}}</div>
                </div>
                <div class="stat-card warning">
                    <div class="stat-label">Min Healthy</div>
                    <div class="stat-value">{{.MinHealthy}}</div>
                </div>
                <div class="stat-card info">
                    <div class="stat-label">Avg Healthy</div>
                    <div class="stat-value">{{printf "%.1f" .AvgHealthy}}</div>
                </div>
                <div class="stat-card danger">
                    <div class="stat-label">Total Failures</div>
                    <div class="stat-value">{{.TotalFailures}}</div>
                </div>
                <div class="stat-card danger">
                    <div class="stat-label">Cascading Failures</div>
                    <div class="stat-value">{{.CascadingFailures}}</div>
                </div>
                <div class="stat-card success">
                    <div class="stat-label">Total Recoveries</div>
                    <div class="stat-value">{{.TotalRecoveries}}</div>
                </div>
                <div class="stat-card warning">
                    <div class="stat-label">Outages</div>
                    <div class="stat-value">{{.TotalOutages}}</div>
                </div>
            </div>

            <h2>Timeline</h2>
            <div class="charts-grid">
                <div class="chart-card">
                    <h3>Node Health Over Time</h3>
                    <div class="chart-container">
                        <canvas id="healthChart"></canvas>
                    </div>
                </div>
                <div class="chart-card">
                    <h3>Cumulative Failures & Recoveries</h3>
                    <div class="chart-container">
                        <canvas id="failuresChart"></canvas>
                    </div>
                </div>
            </div>

            <h2>Failure Analysis</h2>
            <div class="charts-grid">
                <div class="chart-card">
                    <h3>Failures by Type</h3>
                    <div class="chart-container">
                        <canvas id="failureTypeChart"></canvas>
                    </div>
                </div>
                <div class="chart-card">
                    <h3>XID Error Distribution</h3>
                    <div class="chart-container">
                        <canvas id="xidChart"></canvas>
                    </div>
                </div>
            </div>

            {{if .TopXIDCodes}}
            <h2>Top XID Errors</h2>
            <div class="chart-card">
                <table class="data-table">
                    <thead>
                        <tr>
                            <th>XID Code</th>
                            <th>Name</th>
                            <th>Count</th>
                            <th>Severity</th>
                        </tr>
                    </thead>
                    <tbody>
                        {{range .TopXIDCodes}}
                        <tr>
                            <td><strong>{{.Code}}</strong></td>
                            <td>{{.Name}}</td>
                            <td>{{.Count}}</td>
                            <td>
                                {{if .Fatal}}<span class="badge fatal">Fatal</span>
                                {{else}}<span class="badge recoverable">Recoverable</span>{{end}}
                            </td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
            </div>
            {{end}}
        </div>

        <!-- Timeline Tab -->
        <div id="timeline" class="tab-content">
            <div class="timeline-controls">
                <div class="timeline-legend">
                    <div class="legend-item"><div class="legend-color healthy"></div><span>Healthy</span></div>
                    <div class="legend-item"><div class="legend-color degraded"></div><span>Degraded</span></div>
                    <div class="legend-item"><div class="legend-color unhealthy"></div><span>Unhealthy</span></div>
                    <div class="legend-item"><div class="legend-dot failure"></div><span>Failure</span></div>
                    <div class="legend-item"><div class="legend-dot recovery"></div><span>Recovery</span></div>
                </div>
                <input type="text" id="timelineSearch" placeholder="Filter nodes..." 
                       style="padding: 5px 10px; background: #0d1117; border: 1px solid #30363d; border-radius: 4px; color: #c9d1d9; width: 150px; font-size: 0.8em;">
                <select id="timelineFilter" style="padding: 5px 8px; background: #0d1117; border: 1px solid #30363d; border-radius: 4px; color: #c9d1d9; font-size: 0.8em;">
                    <option value="">All</option>
                    <option value="has_events">With Events</option>
                    <option value="unhealthy">Unhealthy</option>
                    <option value="degraded">Degraded</option>
                </select>
                <div class="zoom-controls">
                    <button class="zoom-btn" onclick="zoomTimeline(-1)">−</button>
                    <span style="padding: 0 8px; color: #8b949e; font-size: 0.8em;" id="zoomLevel">100%</span>
                    <button class="zoom-btn" onclick="zoomTimeline(1)">+</button>
                </div>
                <span style="color: #8b949e; font-size: 0.75em;"><span id="timelineVisibleNodes">0</span> nodes</span>
            </div>
            
            <div class="timeline-container">
                <div class="timeline-header">
                    <div class="timeline-labels">Node ID</div>
                    <div class="timeline-ruler" id="timelineRuler"></div>
                </div>
                <div class="timeline-body" id="timelineBody"></div>
            </div>
            
            <div id="timelineTooltip" class="timeline-tooltip" style="display: none;"></div>
        </div>

        <!-- Nodes Tab -->
        <div id="nodes" class="tab-content">
            <div class="nodes-container">
                <div class="nodes-sidebar">
                    <div class="nodes-sidebar-header">
                        <input type="text" id="nodeSearch" placeholder="Search nodes...">
                        <div class="nodes-sidebar-filters">
                            <select id="statusFilter">
                                <option value="">All</option>
                                <option value="healthy">Healthy</option>
                                <option value="degraded">Degraded</option>
                                <option value="unhealthy">Unhealthy</option>
                            </select>
                            <select id="sortBy">
                                <option value="node_id">By ID</option>
                                <option value="failures">By Failures</option>
                                <option value="status">By Status</option>
                                <option value="provider">By Provider</option>
                            </select>
                        </div>
                    </div>
                    <div class="nodes-sidebar-list" id="nodeList"></div>
                    <div class="nodes-sidebar-stats">
                        <span id="visibleNodes">0</span> / <span id="totalNodes">0</span> nodes
                    </div>
                </div>
                <div class="nodes-detail">
                    <div id="nodeDetailContent" class="nodes-detail-empty">
                        Select a node to view details
                    </div>
                </div>
            </div>
        </div>

        <!-- Configuration Tab -->
        <div id="config" class="tab-content">
            <h2>Test Configuration</h2>

            <div class="config-section">
                <h3>General Settings</h3>
                <div class="config-grid">
                    <div>
                        <div class="config-item">
                            <span class="config-label">Test Duration</span>
                            <span class="config-value">{{.TestDuration}}</span>
                        </div>
                        <div class="config-item">
                            <span class="config-label">Metrics Interval</span>
                            <span class="config-value">{{.MetricsInterval}}</span>
                        </div>
                        <div class="config-item">
                            <span class="config-label">Random Seed</span>
                            <span class="config-value">{{if .Seed}}{{.Seed}}{{else}}random{{end}}</span>
                        </div>
                    </div>
                    <div>
                        <div class="config-item">
                            <span class="config-label">Start Time</span>
                            <span class="config-value">{{.StartTime}}</span>
                        </div>
                        <div class="config-item">
                            <span class="config-label">End Time</span>
                            <span class="config-value">{{.EndTime}}</span>
                        </div>
                        <div class="config-item">
                            <span class="config-label">Actual Duration</span>
                            <span class="config-value">{{.Duration}}</span>
                        </div>
                        {{if .LogsDirectory}}
                        <div class="config-item">
                            <span class="config-label">Logs Directory</span>
                            <a href="{{.LogsDirectory}}" target="_blank" class="config-value" style="font-family: monospace; font-size: 0.85em; color: #58a6ff; text-decoration: none;">{{.LogsDirectory}} ↗</a>
                        </div>
                        {{end}}
                    </div>
                </div>
            </div>

            {{if .HasFleetGen}}
            <div class="config-section">
                <h3>Fleet Generation</h3>
                <div class="config-grid">
                    <div>
                        <div class="config-item">
                            <span class="config-label">Total Nodes</span>
                            <span class="config-value">{{.TotalNodes}}</span>
                        </div>
                        <div class="config-item">
                            <span class="config-label">Startup Pattern</span>
                            <span class="config-value">{{.StartupPattern}}</span>
                        </div>
                        <div class="config-item">
                            <span class="config-label">Startup Duration</span>
                            <span class="config-value">{{.StartupDuration}}</span>
                        </div>
                        {{if .StartupBatchSize}}
                        <div class="config-item">
                            <span class="config-label">Batch Size</span>
                            <span class="config-value">{{.StartupBatchSize}}</span>
                        </div>
                        {{end}}
                        {{if .StartupJitter}}
                        <div class="config-item">
                            <span class="config-label">Jitter</span>
                            <span class="config-value">{{.StartupJitter}}%</span>
                        </div>
                        {{end}}
                    </div>
                    <div>
                        {{if .FleetProviders}}
                        <h4 style="color: #8b949e; font-size: 0.9em; margin-bottom: 10px;">Provider Distribution</h4>
                        {{range .FleetProviders}}
                        <div class="config-item">
                            <span class="config-label">{{.Key}}</span>
                            <span class="config-value">{{.Value}}%</span>
                        </div>
                        {{end}}
                        {{end}}
                        {{if .FleetRegions}}
                        <h4 style="color: #8b949e; font-size: 0.9em; margin: 15px 0 10px 0;">Region Distribution</h4>
                        {{range .FleetRegions}}
                        <div class="config-item">
                            <span class="config-label">{{.Key}}</span>
                            <span class="config-value">{{.Value}}%</span>
                        </div>
                        {{end}}
                        {{end}}
                    </div>
                </div>

                {{if .FleetTemplates}}
                <h4 style="color: #8b949e; font-size: 0.9em; margin: 20px 0 10px 0;">Node Templates</h4>
                <table class="data-table">
                    <thead>
                        <tr>
                            <th>Name</th>
                            <th>Weight</th>
                            <th>GPU Count</th>
                            <th>GPU Type</th>
                            <th>Instance Type</th>
                        </tr>
                    </thead>
                    <tbody>
                        {{range .FleetTemplates}}
                        <tr>
                            <td><strong>{{.Name}}</strong></td>
                            <td>{{.Weight}}</td>
                            <td>{{.GPUCount}}</td>
                            <td>{{.GPUType}}</td>
                            <td>{{.InstanceType}}</td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
                {{end}}
            </div>
            {{end}}

            {{if .HasChaos}}
            <div class="config-section">
                <h3>Chaos Engineering</h3>
                <div class="config-grid">
                    <div>
                        <div class="config-item">
                            <span class="config-label">Chaos Enabled</span>
                            <span class="config-value">
                                {{if .ChaosEnabled}}<span class="badge enabled">Enabled</span>
                                {{else}}<span class="badge disabled">Disabled</span>{{end}}
                            </span>
                        </div>
                        <div class="config-item">
                            <span class="config-label">Failure Rate</span>
                            <span class="config-value">{{.FailureRate}} / min / 1000 nodes</span>
                        </div>
                    </div>
                    <div>
                        {{if .FailureTypeWeights}}
                        <h4 style="color: #8b949e; font-size: 0.9em; margin-bottom: 10px;">Failure Type Weights</h4>
                        {{range .FailureTypeWeights}}
                        <div class="config-item">
                            <span class="config-label">{{.Key}}</span>
                            <span class="config-value">{{.Value}}</span>
                        </div>
                        {{end}}
                        {{end}}
                    </div>
                </div>

                {{if .XIDDistribution}}
                <div class="charts-grid" style="margin-top: 20px;">
                    <div>
                        <h4 style="color: #8b949e; font-size: 0.9em; margin-bottom: 10px;">XID Error Distribution (Configured Weights)</h4>
                        <table class="data-table">
                            <thead>
                                <tr>
                                    <th>XID Code</th>
                                    <th>Name</th>
                                    <th>Weight</th>
                                    <th>Severity</th>
                                </tr>
                            </thead>
                            <tbody>
                                {{range .XIDDistribution}}
                                <tr>
                                    <td><strong>{{.Code}}</strong></td>
                                    <td>{{.Name}}</td>
                                    <td>{{.Weight}}</td>
                                    <td>
                                        {{if .Fatal}}<span class="badge fatal">Fatal</span>
                                        {{else}}<span class="badge recoverable">Recoverable</span>{{end}}
                                    </td>
                                </tr>
                                {{end}}
                            </tbody>
                        </table>
                    </div>
                    <div class="chart-card">
                        <h3>XID Weight Distribution</h3>
                        <div class="chart-container small">
                            <canvas id="xidDistChart"></canvas>
                        </div>
                    </div>
                </div>
                {{end}}
            </div>
            {{end}}

            <div class="config-cards">
                {{if .HasCascading}}
                <div class="config-section">
                    <h3>Cascading Failures</h3>
                    <div class="config-item">
                        <span class="config-label">Status</span>
                        <span class="config-value"><span class="badge enabled">Enabled</span></span>
                    </div>
                    <div class="config-item">
                        <span class="config-label">Probability</span>
                        <span class="config-value">{{printf "%.0f" (mul .CascadeProbability 100)}}%</span>
                    </div>
                    <div class="config-item">
                        <span class="config-label">Max Depth</span>
                        <span class="config-value">{{.CascadeMaxDepth}}</span>
                    </div>
                    <div class="config-item">
                        <span class="config-label">Min Delay</span>
                        <span class="config-value">{{.CascadeMinDelay}}</span>
                    </div>
                    <div class="config-item">
                        <span class="config-label">Max Delay</span>
                        <span class="config-value">{{.CascadeMaxDelay}}</span>
                    </div>
                    <div class="config-item">
                        <span class="config-label">Scope</span>
                        <span class="config-value">{{.CascadeScope}}</span>
                    </div>
                    <div class="config-item">
                        <span class="config-label">Max Affected</span>
                        <span class="config-value">{{printf "%.0f" (mul .CascadeMaxAffected 100)}}%</span>
                    </div>
                </div>
                {{end}}

                {{if .HasRecovery}}
                <div class="config-section">
                    <h3>Recovery Settings</h3>
                    <div class="config-item">
                        <span class="config-label">Status</span>
                        <span class="config-value"><span class="badge enabled">Enabled</span></span>
                    </div>
                    <div class="config-item">
                        <span class="config-label">Probability</span>
                        <span class="config-value">{{printf "%.0f" (mul .RecoveryProbability 100)}}%</span>
                    </div>
                    <div class="config-item">
                        <span class="config-label">Mean Time</span>
                        <span class="config-value">{{.RecoveryMeanTime}}</span>
                    </div>
                    <div class="config-item">
                        <span class="config-label">Std Deviation</span>
                        <span class="config-value">{{.RecoveryStdDev}}</span>
                    </div>
                </div>
                {{end}}
            </div>

            {{if .ScheduledOutages}}
            <div class="config-section">
                <h3>Scheduled Outages</h3>
                <table class="data-table">
                    <thead>
                        <tr>
                            <th>Name</th>
                            <th>Start Time</th>
                            <th>Duration</th>
                            <th>Scope</th>
                            <th>Target</th>
                            <th>Failure Type</th>
                        </tr>
                    </thead>
                    <tbody>
                        {{range .ScheduledOutages}}
                        <tr>
                            <td><strong>{{.Name}}</strong></td>
                            <td>{{.StartTime}}</td>
                            <td>{{.Duration}}</td>
                            <td>{{.Scope}}</td>
                            <td>{{.Target}}</td>
                            <td>{{.FailureType}}</td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
            </div>
            {{end}}
        </div>

        <div class="footer">
            Generated by Navarch Stress Test Simulator
        </div>
    </div>

    <script>
        Chart.defaults.color = '#8b949e';
        Chart.defaults.borderColor = '#30363d';

        function showTab(tabId) {
            document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
            document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
            document.getElementById(tabId).classList.add('active');
            event.target.classList.add('active');
        }

        // Node Health Timeline
        new Chart(document.getElementById('healthChart'), {
            type: 'line',
            data: {
                labels: {{.TimelineLabels}},
                datasets: [
                    { label: 'Healthy', data: {{.HealthyData}}, borderColor: '#3fb950', backgroundColor: '#3fb95022', fill: true, tension: 0.3 },
                    { label: 'Degraded', data: {{.DegradedData}}, borderColor: '#d29922', backgroundColor: '#d2992222', fill: true, tension: 0.3 },
                    { label: 'Unhealthy', data: {{.UnhealthyData}}, borderColor: '#f85149', backgroundColor: '#f8514922', fill: true, tension: 0.3 }
                ]
            },
            options: { responsive: true, maintainAspectRatio: false, scales: { y: { beginAtZero: true } }, plugins: { legend: { position: 'bottom' } } }
        });

        // Failures & Recoveries Timeline
        new Chart(document.getElementById('failuresChart'), {
            type: 'line',
            data: {
                labels: {{.TimelineLabels}},
                datasets: [
                    { label: 'Cumulative Failures', data: {{.FailuresData}}, borderColor: '#f85149', backgroundColor: '#f8514922', fill: true, tension: 0.3 },
                    { label: 'Cumulative Recoveries', data: {{.RecoveriesData}}, borderColor: '#3fb950', backgroundColor: '#3fb95022', fill: true, tension: 0.3 }
                ]
            },
            options: { responsive: true, maintainAspectRatio: false, scales: { y: { beginAtZero: true } }, plugins: { legend: { position: 'bottom' } } }
        });

        // Failure Type Distribution
        new Chart(document.getElementById('failureTypeChart'), {
            type: 'doughnut',
            data: {
                labels: {{.FailureTypeLabels}},
                datasets: [{ data: {{.FailureTypeData}}, backgroundColor: ['#f85149', '#d29922', '#58a6ff', '#3fb950', '#a371f7', '#f778ba', '#79c0ff', '#7ee787', '#ffa657', '#ff7b72'] }]
            },
            options: { responsive: true, maintainAspectRatio: false, plugins: { legend: { position: 'right' } } }
        });

        // XID Distribution (Results)
        new Chart(document.getElementById('xidChart'), {
            type: 'bar',
            data: {
                labels: {{.XIDLabels}},
                datasets: [{ label: 'Count', data: {{.XIDData}}, backgroundColor: '#58a6ff', borderRadius: 4 }]
            },
            options: { responsive: true, maintainAspectRatio: false, indexAxis: 'y', scales: { x: { beginAtZero: true } }, plugins: { legend: { display: false } } }
        });

        // XID Distribution Chart (Config)
        {{if .XIDDistribution}}
        new Chart(document.getElementById('xidDistChart'), {
            type: 'doughnut',
            data: {
                labels: {{.XIDDistLabels}},
                datasets: [{ data: {{.XIDDistData}}, backgroundColor: ['#f85149', '#d29922', '#58a6ff', '#3fb950', '#a371f7', '#f778ba', '#79c0ff', '#7ee787', '#ffa657', '#ff7b72'] }]
            },
            options: { responsive: true, maintainAspectRatio: false, plugins: { legend: { position: 'right' } } }
        });
        {{end}}
        
        // Timeline management
        const timelineNodes = {{.NodesJSON}};
        const testDuration = "{{.Duration}}";
        let timelineZoom = 1;
        let timelineFilteredNodes = [...timelineNodes];
        
        function parseGoDuration(durationStr) {
            // Parse Go duration string like "1m0s" or "5m30s" to milliseconds
            const matches = durationStr.match(/(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?/);
            if (!matches) return 60000; // Default 1 minute
            const hours = parseInt(matches[1]) || 0;
            const minutes = parseInt(matches[2]) || 0;
            const seconds = parseFloat(matches[3]) || 0;
            return (hours * 3600 + minutes * 60 + seconds) * 1000;
        }
        
        const durationMs = parseGoDuration(testDuration);
        
        function renderTimeline() {
            const ruler = document.getElementById('timelineRuler');
            const body = document.getElementById('timelineBody');
            
            // Calculate tick intervals based on duration
            const numTicks = Math.min(20, Math.max(6, Math.floor(durationMs / 10000)));
            const tickInterval = durationMs / numTicks;
            
            // Render ruler
            let rulerHtml = '';
            for (let i = 0; i <= numTicks; i++) {
                const time = (i * tickInterval) / 1000;
                let label;
                if (time >= 60) {
                    label = Math.floor(time / 60) + 'm' + (time % 60 > 0 ? Math.floor(time % 60) + 's' : '');
                } else {
                    label = Math.floor(time) + 's';
                }
                rulerHtml += '<div class="timeline-tick">' + label + '</div>';
            }
            ruler.innerHTML = rulerHtml;
            
            // Find the earliest timestamp across all nodes
            let minTime = Infinity;
            timelineFilteredNodes.forEach(node => {
                if (node.events && node.events.length > 0) {
                    const firstEvent = new Date(node.events[0].timestamp).getTime();
                    if (firstEvent < minTime) minTime = firstEvent;
                }
            });
            if (minTime === Infinity) minTime = Date.now() - durationMs;
            
            // Render node tracks
            let bodyHtml = '';
            timelineFilteredNodes.forEach(node => {
                const statusColor = node.status === 'healthy' ? '#238636' : 
                                  node.status === 'degraded' ? '#9e6a03' : '#da3633';
                const hasFailures = node.events && node.events.some(e => e.type === 'failure');
                
                bodyHtml += '<div class="timeline-row' + (hasFailures ? ' has-failures' : '') + '">';
                bodyHtml += '<div class="timeline-node-label" title="' + node.node_id + '" onclick="selectNodeFromTimeline(\'' + node.node_id + '\')">';
                bodyHtml += '<span style="width: 6px; height: 6px; border-radius: 50%; background: ' + statusColor + '; flex-shrink: 0;"></span>';
                bodyHtml += '<span style="overflow: hidden; text-overflow: ellipsis;">' + node.node_id + '</span>';
                bodyHtml += '</div>';
                bodyHtml += '<div class="timeline-track" data-node="' + node.node_id + '">';
                
                // Build status segments from events
                if (node.events && node.events.length > 0) {
                    let currentStatus = 'pending';
                    let segmentStart = 0;
                    
                    node.events.forEach((event, idx) => {
                        const eventTime = new Date(event.timestamp).getTime();
                        const eventPos = ((eventTime - minTime) / durationMs) * 100 * timelineZoom;
                        
                        // Close previous segment
                        if (idx > 0 || event.type === 'started') {
                            const segmentEnd = eventPos;
                            const segmentWidth = segmentEnd - segmentStart;
                            if (segmentWidth > 0.1) {
                                bodyHtml += '<div class="timeline-segment ' + currentStatus + '" style="left: ' + segmentStart + '%; width: ' + segmentWidth + '%;"></div>';
                            }
                        }
                        
                        // Update status based on event
                        if (event.type === 'started' || event.type === 'recovery') {
                            currentStatus = 'healthy';
                        } else if (event.type === 'failure') {
                            currentStatus = 'unhealthy';
                        } else if (event.type === 'status_change' && event.status) {
                            currentStatus = event.status;
                        }
                        segmentStart = eventPos;
                        
                        // Render event marker (except for 'started')
                        if (event.type !== 'started') {
                            const eventClass = event.type === 'failure' ? 'failure' : 
                                             event.type === 'recovery' ? 'recovery' : 'status_change';
                            bodyHtml += '<div class="timeline-event ' + eventClass + '" style="left: ' + eventPos + '%;" ';
                            bodyHtml += 'data-event="' + encodeURIComponent(JSON.stringify(event)) + '" ';
                            bodyHtml += 'data-node="' + node.node_id + '" ';
                            bodyHtml += 'onmouseenter="showEventTooltip(event, this)" ';
                            bodyHtml += 'onmouseleave="hideEventTooltip()"></div>';
                        }
                    });
                    
                    // Final segment to end
                    const finalWidth = (100 * timelineZoom) - segmentStart;
                    if (finalWidth > 0.1) {
                        bodyHtml += '<div class="timeline-segment ' + currentStatus + '" style="left: ' + segmentStart + '%; width: ' + finalWidth + '%;"></div>';
                    }
                } else {
                    // No events - show as healthy for full duration
                    bodyHtml += '<div class="timeline-segment healthy" style="left: 0; width: ' + (100 * timelineZoom) + '%;"></div>';
                }
                
                bodyHtml += '</div></div>';
            });
            
            body.innerHTML = bodyHtml;
            document.getElementById('timelineVisibleNodes').textContent = timelineFilteredNodes.length;
        }
        
        function showEventTooltip(e, element) {
            const event = JSON.parse(decodeURIComponent(element.dataset.event));
            const nodeId = element.dataset.node;
            const tooltip = document.getElementById('timelineTooltip');
            
            const time = new Date(event.timestamp).toLocaleString();
            let typeLabel = event.type.replace('_', ' ').replace(/\b\w/g, l => l.toUpperCase());
            let color = event.type === 'failure' ? '#f85149' : 
                       event.type === 'recovery' ? '#3fb950' : '#58a6ff';
            
            tooltip.innerHTML = '<div style="margin-bottom: 8px; font-weight: 500; color: ' + color + ';">' + typeLabel + '</div>';
            tooltip.innerHTML += '<div style="margin-bottom: 5px;"><strong>Node:</strong> ' + nodeId + '</div>';
            tooltip.innerHTML += '<div style="margin-bottom: 5px;"><strong>Time:</strong> ' + time + '</div>';
            if (event.message) {
                tooltip.innerHTML += '<div style="margin-bottom: 5px;"><strong>Details:</strong> ' + event.message + '</div>';
            }
            if (event.xid_code) {
                tooltip.innerHTML += '<div><strong>XID Code:</strong> ' + event.xid_code + '</div>';
            }
            
            tooltip.style.display = 'block';
            tooltip.style.left = (e.pageX + 15) + 'px';
            tooltip.style.top = (e.pageY - 10) + 'px';
        }
        
        function hideEventTooltip() {
            document.getElementById('timelineTooltip').style.display = 'none';
        }
        
        function zoomTimeline(direction) {
            if (direction > 0 && timelineZoom < 5) {
                timelineZoom *= 1.5;
            } else if (direction < 0 && timelineZoom > 0.5) {
                timelineZoom /= 1.5;
            }
            document.getElementById('zoomLevel').textContent = Math.round(timelineZoom * 100) + '%';
            renderTimeline();
        }
        
        function resetZoom() {
            timelineZoom = 1;
            document.getElementById('zoomLevel').textContent = '100%';
            renderTimeline();
        }
        
        function filterTimeline() {
            const search = document.getElementById('timelineSearch').value.toLowerCase();
            const filter = document.getElementById('timelineFilter').value;
            
            timelineFilteredNodes = timelineNodes.filter(node => {
                const matchesSearch = !search || node.node_id.toLowerCase().includes(search);
                
                let matchesFilter = true;
                if (filter === 'has_events') {
                    matchesFilter = node.events && node.events.length > 1;
                } else if (filter === 'unhealthy') {
                    matchesFilter = node.status === 'unhealthy';
                } else if (filter === 'degraded') {
                    matchesFilter = node.status === 'degraded';
                }
                
                return matchesSearch && matchesFilter;
            });
            
            renderTimeline();
        }
        
        // Initialize timeline
        if (timelineNodes && timelineNodes.length > 0) {
            renderTimeline();
            document.getElementById('timelineSearch').addEventListener('input', filterTimeline);
            document.getElementById('timelineFilter').addEventListener('change', filterTimeline);
        }
        
        // Node management - Compact split-panel view
        const allNodes = {{.NodesJSON}};
        let filteredNodes = [...allNodes];
        let selectedNodeId = null;
        
        function renderNodes() {
            const container = document.getElementById('nodeList');
            let html = '';
            
            filteredNodes.forEach(node => {
                const failureCount = node.events ? node.events.filter(e => e.type === 'failure').length : 0;
                const isSelected = node.node_id === selectedNodeId;
                
                html += '<div class="node-list-item' + (isSelected ? ' selected' : '') + '" onclick="selectNode(\'' + node.node_id + '\')">';
                html += '<div class="status-dot ' + node.status + '"></div>';
                html += '<span class="node-id">' + node.node_id + '</span>';
                if (failureCount > 0) {
                    html += '<span class="failure-badge">' + failureCount + '</span>';
                }
                html += '</div>';
            });
            
            container.innerHTML = html;
            document.getElementById('visibleNodes').textContent = filteredNodes.length;
            document.getElementById('totalNodes').textContent = allNodes.length;
        }
        
        function selectNode(nodeId) {
            selectedNodeId = nodeId;
            renderNodes();
            showNodeDetail(nodeId);
        }
        
        function showNodeDetail(nodeId) {
            const node = allNodes.find(n => n.node_id === nodeId);
            if (!node) return;
            
            const detailContainer = document.getElementById('nodeDetailContent');
            const statusColor = node.status === 'healthy' ? '#3fb950' : 
                              node.status === 'degraded' ? '#d29922' : '#f85149';
            
            let html = '<div class="nodes-detail-header">';
            html += '<div style="display: flex; align-items: center; gap: 10px;">';
            html += '<span style="width: 12px; height: 12px; border-radius: 50%; background: ' + statusColor + ';"></span>';
            html += '<h3 style="margin: 0; color: #c9d1d9; font-size: 1.1em; font-family: monospace;">' + node.node_id + '</h3>';
            html += '<span class="badge ' + (node.status === 'healthy' ? 'enabled' : node.status === 'degraded' ? 'recoverable' : 'fatal') + '">' + node.status + '</span>';
            html += '</div></div>';
            
            html += '<div class="nodes-detail-content">';
            html += '<div class="node-info-grid">';
            html += '<div class="node-info-item"><label>Provider</label><span>' + node.provider + '</span></div>';
            html += '<div class="node-info-item"><label>Region</label><span>' + node.region + '</span></div>';
            html += '<div class="node-info-item"><label>Zone</label><span>' + node.zone + '</span></div>';
            html += '<div class="node-info-item"><label>Instance</label><span>' + node.instance_type + '</span></div>';
            html += '<div class="node-info-item"><label>GPUs</label><span>' + node.gpu_count + 'x ' + node.gpu_type + '</span></div>';
            html += '<div class="node-info-item"><label>Failures</label><span style="color: #f85149;">' + node.failure_count + '</span></div>';
            html += '</div>';
            
            // Log file path
            if (node.log_file) {
                html += '<div style="margin-bottom: 12px;"><label style="color: #8b949e; font-size: 0.75em; text-transform: uppercase;">Log File</label>';
                html += '<a href="' + node.log_file + '" target="_blank" style="display: block; font-size: 0.85em; color: #58a6ff; word-break: break-all; background: #161b22; padding: 6px; border-radius: 4px; margin-top: 4px; text-decoration: none; font-family: monospace;">' + node.log_file + ' ↗</a></div>';
            }
            
            // Events list
            html += '<h4 style="color: #8b949e; font-size: 0.8em; margin-bottom: 10px; text-transform: uppercase;">Event Log (' + (node.events ? node.events.length : 0) + ')</h4>';
            html += '<div class="node-events-list">';
            
            if (node.events && node.events.length > 0) {
                node.events.forEach(event => {
                    const time = new Date(event.timestamp).toLocaleTimeString();
                    let icon = '•';
                    let color = '#8b949e';
                    
                    if (event.type === 'started') { icon = '▶'; color = '#3fb950'; }
                    else if (event.type === 'failure') { icon = '✗'; color = '#f85149'; }
                    else if (event.type === 'recovery') { icon = '↻'; color = '#3fb950'; }
                    else if (event.type === 'status_change') { icon = '◆'; color = '#58a6ff'; }
                    
                    html += '<div class="node-event-item">';
                    html += '<span class="node-event-time">' + time + '</span>';
                    html += '<span class="node-event-icon" style="color: ' + color + ';">' + icon + '</span>';
                    html += '<span class="node-event-msg">' + event.message;
                    if (event.xid_code) html += ' <span style="color: #f85149; font-family: monospace; font-size: 0.9em;">[XID ' + event.xid_code + ']</span>';
                    html += '</span></div>';
                });
            } else {
                html += '<div style="padding: 15px; color: #8b949e; text-align: center;">No events</div>';
            }
            
            html += '</div></div>';
            
            detailContainer.innerHTML = html;
            detailContainer.className = '';
        }
        
        function filterNodes() {
            const search = document.getElementById('nodeSearch').value.toLowerCase();
            const statusFilter = document.getElementById('statusFilter').value;
            const sortBy = document.getElementById('sortBy').value;
            
            filteredNodes = allNodes.filter(node => {
                const matchesSearch = !search || 
                    node.node_id.toLowerCase().includes(search) ||
                    node.provider.toLowerCase().includes(search) ||
                    node.region.toLowerCase().includes(search);
                const matchesStatus = !statusFilter || node.status === statusFilter;
                return matchesSearch && matchesStatus;
            });
            
            filteredNodes.sort((a, b) => {
                switch(sortBy) {
                    case 'failures': return b.failure_count - a.failure_count;
                    case 'status': return a.status.localeCompare(b.status);
                    case 'provider': return a.provider.localeCompare(b.provider);
                    default: return a.node_id.localeCompare(b.node_id);
                }
            });
            
            renderNodes();
        }
        
        // Public function to select node from timeline
        window.selectNodeFromTimeline = function(nodeId) {
            showTab('nodes');
            setTimeout(() => selectNode(nodeId), 100);
        };
        
        // Initialize
        if (allNodes && allNodes.length > 0) {
            filterNodes();
            document.getElementById('nodeSearch').addEventListener('input', filterNodes);
            document.getElementById('statusFilter').addEventListener('change', filterNodes);
            document.getElementById('sortBy').addEventListener('change', filterNodes);
        }
    </script>
</body>
</html>`
