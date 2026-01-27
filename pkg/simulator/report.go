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
    <title>{{.Name}} | Stress Test Report</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <style>
        @import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600;700&family=Inter:wght@400;500;600&display=swap');
        
        :root {
            --bg-primary: #0a0a0a;
            --bg-secondary: #111111;
            --bg-tertiary: #1a1a1a;
            --bg-hover: #222222;
            --border: #2a2a2a;
            --border-subtle: #1f1f1f;
            --text-primary: #e5e5e5;
            --text-secondary: #888888;
            --text-muted: #555555;
            --accent: #00d9ff;
            --accent-dim: #00d9ff22;
            --success: #00ff88;
            --success-dim: #00ff8815;
            --warning: #ffaa00;
            --warning-dim: #ffaa0015;
            --danger: #ff4444;
            --danger-dim: #ff444415;
            --mono: 'JetBrains Mono', 'SF Mono', 'Fira Code', monospace;
            --sans: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
        }
        
        * { margin: 0; padding: 0; box-sizing: border-box; }
        
        body {
            font-family: var(--sans);
            background: var(--bg-primary);
            color: var(--text-primary);
            line-height: 1.5;
            font-size: 13px;
            -webkit-font-smoothing: antialiased;
        }
        
        .container { max-width: 1600px; margin: 0 auto; padding: 16px; }
        
        /* Header - Compact command bar style */
        .header {
            background: var(--bg-secondary);
            border-bottom: 1px solid var(--border);
            padding: 12px 16px;
            margin: -16px -16px 16px -16px;
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 24px;
            flex-wrap: wrap;
        }
        .header-left { display: flex; align-items: center; gap: 16px; }
        .header h1 {
            font-family: var(--mono);
            font-size: 14px;
            font-weight: 600;
            color: var(--text-primary);
            letter-spacing: -0.02em;
        }
        .header-badge {
            font-family: var(--mono);
            font-size: 10px;
            font-weight: 500;
            padding: 3px 8px;
            border-radius: 3px;
            background: var(--accent-dim);
            color: var(--accent);
            border: 1px solid var(--accent);
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }
        .header-meta {
            display: flex;
            gap: 20px;
            font-family: var(--mono);
            font-size: 11px;
            color: var(--text-secondary);
        }
        .header-meta span { display: flex; align-items: center; gap: 6px; }
        .header-meta .label { color: var(--text-muted); }
        .header-meta .value { color: var(--text-primary); }

        /* Tabs - Terminal style */
        .tabs {
            display: flex;
            gap: 0;
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 4px;
            padding: 3px;
            margin-bottom: 16px;
            width: fit-content;
        }
        .tab {
            padding: 6px 16px;
            cursor: pointer;
            color: var(--text-secondary);
            border: none;
            background: none;
            font-size: 12px;
            font-family: var(--mono);
            font-weight: 500;
            border-radius: 3px;
            transition: all 0.15s;
        }
        .tab:hover { color: var(--text-primary); background: var(--bg-hover); }
        .tab.active {
            color: var(--bg-primary);
            background: var(--accent);
        }
        .tab-content { display: none; }
        .tab-content.active { display: block; }

        /* Stats grid - Dense metric cards */
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
            gap: 8px;
            margin-bottom: 16px;
        }
        .stat-card {
            background: var(--bg-secondary);
            border-radius: 4px;
            padding: 12px;
            border: 1px solid var(--border);
            position: relative;
        }
        .stat-card::before {
            content: '';
            position: absolute;
            left: 0;
            top: 0;
            bottom: 0;
            width: 3px;
            border-radius: 4px 0 0 4px;
        }
        .stat-card.success::before { background: var(--success); }
        .stat-card.warning::before { background: var(--warning); }
        .stat-card.danger::before { background: var(--danger); }
        .stat-card.info::before { background: var(--accent); }
        .stat-label {
            font-size: 10px;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.05em;
            font-weight: 500;
        }
        .stat-value {
            font-family: var(--mono);
            font-size: 24px;
            font-weight: 600;
            margin-top: 2px;
            letter-spacing: -0.02em;
        }
        .stat-card.success .stat-value { color: var(--success); }
        .stat-card.warning .stat-value { color: var(--warning); }
        .stat-card.danger .stat-value { color: var(--danger); }
        .stat-card.info .stat-value { color: var(--accent); }

        /* Charts */
        .charts-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(450px, 1fr));
            gap: 12px;
        }
        .chart-card {
            background: var(--bg-secondary);
            border-radius: 4px;
            padding: 16px;
            border: 1px solid var(--border);
        }
        .chart-card h3 {
            font-family: var(--mono);
            font-size: 11px;
            font-weight: 500;
            color: var(--text-secondary);
            text-transform: uppercase;
            letter-spacing: 0.05em;
            margin-bottom: 12px;
        }
        .chart-container { position: relative; height: 240px; }
        .chart-container.small { height: 180px; }

        /* Tables - Dense data display */
        .data-table {
            width: 100%;
            border-collapse: collapse;
            font-family: var(--mono);
            font-size: 12px;
        }
        .data-table th, .data-table td {
            padding: 8px 12px;
            text-align: left;
            border-bottom: 1px solid var(--border-subtle);
        }
        .data-table th {
            background: var(--bg-tertiary);
            color: var(--text-muted);
            font-weight: 500;
            font-size: 10px;
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }
        .data-table tr:hover { background: var(--bg-hover); }
        .data-table td { color: var(--text-secondary); }

        /* Badges - Minimal */
        .badge {
            display: inline-block;
            padding: 2px 6px;
            border-radius: 2px;
            font-family: var(--mono);
            font-size: 10px;
            font-weight: 500;
            text-transform: uppercase;
            letter-spacing: 0.03em;
        }
        .badge.fatal { background: var(--danger-dim); color: var(--danger); border: 1px solid var(--danger); }
        .badge.recoverable { background: var(--success-dim); color: var(--success); border: 1px solid var(--success); }
        .badge.enabled { background: var(--success-dim); color: var(--success); border: 1px solid var(--success); }
        .badge.disabled { background: var(--bg-tertiary); color: var(--text-muted); border: 1px solid var(--border); }

        /* Config sections */
        .config-section {
            background: var(--bg-secondary);
            border-radius: 4px;
            padding: 16px;
            border: 1px solid var(--border);
            margin-bottom: 12px;
        }
        .config-section h3 {
            font-family: var(--mono);
            font-size: 11px;
            font-weight: 600;
            color: var(--accent);
            text-transform: uppercase;
            letter-spacing: 0.05em;
            margin-bottom: 12px;
            padding-bottom: 8px;
            border-bottom: 1px solid var(--border-subtle);
        }
        .config-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
            gap: 16px;
        }
        .config-item {
            display: flex;
            justify-content: space-between;
            padding: 6px 0;
            border-bottom: 1px solid var(--border-subtle);
        }
        .config-item:last-child { border-bottom: none; }
        .config-label { color: var(--text-muted); font-size: 12px; }
        .config-value {
            color: var(--text-primary);
            font-family: var(--mono);
            font-size: 12px;
            font-weight: 500;
        }

        .config-cards {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 12px;
        }

        /* Section headers */
        .section-header {
            font-family: var(--mono);
            font-size: 11px;
            font-weight: 600;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.08em;
            margin: 20px 0 12px 0;
            padding-bottom: 8px;
            border-bottom: 1px solid var(--border);
        }

        .footer {
            margin-top: 32px;
            padding-top: 16px;
            border-top: 1px solid var(--border);
            text-align: center;
            color: var(--text-muted);
            font-family: var(--mono);
            font-size: 10px;
            letter-spacing: 0.05em;
        }

        /* Timeline Track View - Dense terminal style */
        .timeline-container {
            background: var(--bg-secondary);
            border-radius: 4px;
            border: 1px solid var(--border);
            overflow: hidden;
        }
        .timeline-header {
            display: flex;
            background: var(--bg-tertiary);
            border-bottom: 1px solid var(--border);
            position: sticky;
            top: 0;
            z-index: 10;
        }
        .timeline-labels {
            min-width: 160px;
            max-width: 160px;
            padding: 4px 8px;
            font-family: var(--mono);
            font-weight: 500;
            color: var(--text-muted);
            font-size: 10px;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            border-right: 1px solid var(--border);
        }
        .timeline-ruler {
            flex: 1;
            display: flex;
            align-items: center;
            padding: 4px 0;
            overflow-x: auto;
        }
        .timeline-tick {
            flex: 1;
            text-align: center;
            font-family: var(--mono);
            font-size: 9px;
            color: var(--text-muted);
            border-left: 1px solid var(--border-subtle);
            padding: 0 2px;
            min-width: 50px;
        }
        .timeline-body {
            max-height: 65vh;
            overflow-y: auto;
        }
        .timeline-row {
            display: flex;
            border-bottom: 1px solid var(--border-subtle);
            min-height: 18px;
        }
        .timeline-row:hover { background: var(--bg-hover); }
        .timeline-row.has-failures { background: var(--danger-dim); }
        .timeline-node-label {
            min-width: 160px;
            max-width: 160px;
            padding: 1px 8px;
            font-family: var(--mono);
            font-size: 10px;
            color: var(--text-secondary);
            border-right: 1px solid var(--border-subtle);
            display: flex;
            align-items: center;
            gap: 6px;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            cursor: pointer;
        }
        .timeline-node-label:hover {
            color: var(--text-primary);
            background: var(--bg-tertiary);
        }
        .timeline-track {
            flex: 1;
            position: relative;
            height: 18px;
        }
        .timeline-segment {
            position: absolute;
            top: 2px;
            height: 14px;
            border-radius: 1px;
        }
        .timeline-segment.healthy { background: #166534; }
        .timeline-segment.degraded { background: #854d0e; }
        .timeline-segment.unhealthy { background: #991b1b; }
        .timeline-segment.pending { background: var(--bg-tertiary); }
        .timeline-event {
            position: absolute;
            top: 50%;
            transform: translate(-50%, -50%);
            width: 6px;
            height: 6px;
            border-radius: 50%;
            cursor: pointer;
            z-index: 5;
        }
        .timeline-event.failure { background: var(--danger); box-shadow: 0 0 4px var(--danger); }
        .timeline-event.recovery { background: var(--success); box-shadow: 0 0 4px var(--success); }
        .timeline-event.status_change { background: var(--accent); }
        .timeline-controls {
            display: flex;
            gap: 8px;
            margin-bottom: 12px;
            align-items: center;
            flex-wrap: wrap;
        }
        .timeline-legend {
            display: flex;
            gap: 12px;
            padding: 6px 10px;
            background: var(--bg-secondary);
            border-radius: 3px;
            border: 1px solid var(--border);
        }
        .legend-item {
            display: flex;
            align-items: center;
            gap: 4px;
            font-family: var(--mono);
            font-size: 10px;
            color: var(--text-muted);
        }
        .legend-color {
            width: 10px;
            height: 10px;
            border-radius: 1px;
        }
        .legend-color.healthy { background: #166534; }
        .legend-color.degraded { background: #854d0e; }
        .legend-color.unhealthy { background: #991b1b; }
        .legend-dot {
            width: 6px;
            height: 6px;
            border-radius: 50%;
        }
        .legend-dot.failure { background: var(--danger); }
        .legend-dot.recovery { background: var(--success); }
        .timeline-tooltip {
            position: fixed;
            background: var(--bg-tertiary);
            border: 1px solid var(--border);
            border-radius: 3px;
            padding: 8px 10px;
            font-family: var(--mono);
            font-size: 11px;
            color: var(--text-primary);
            z-index: 1000;
            pointer-events: none;
            max-width: 300px;
            box-shadow: 0 4px 20px rgba(0,0,0,0.5);
        }
        .zoom-controls {
            display: flex;
            gap: 2px;
        }
        .zoom-btn {
            padding: 3px 8px;
            background: var(--bg-tertiary);
            border: 1px solid var(--border);
            border-radius: 2px;
            color: var(--text-secondary);
            cursor: pointer;
            font-family: var(--mono);
            font-size: 11px;
        }
        .zoom-btn:hover { background: var(--bg-hover); color: var(--text-primary); }

        /* Node List - Terminal-style split view */
        .nodes-container {
            display: flex;
            gap: 12px;
            height: 70vh;
        }
        .nodes-sidebar {
            width: 300px;
            min-width: 300px;
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 4px;
            display: flex;
            flex-direction: column;
        }
        .nodes-sidebar-header {
            padding: 8px;
            border-bottom: 1px solid var(--border);
            background: var(--bg-tertiary);
        }
        .nodes-sidebar-header input {
            width: 100%;
            padding: 5px 8px;
            background: var(--bg-primary);
            border: 1px solid var(--border);
            border-radius: 2px;
            color: var(--text-primary);
            font-family: var(--mono);
            font-size: 11px;
        }
        .nodes-sidebar-header input:focus {
            outline: none;
            border-color: var(--accent);
        }
        .nodes-sidebar-filters {
            display: flex;
            gap: 4px;
            margin-top: 6px;
        }
        .nodes-sidebar-filters select {
            flex: 1;
            padding: 4px 6px;
            background: var(--bg-primary);
            border: 1px solid var(--border);
            border-radius: 2px;
            color: var(--text-secondary);
            font-family: var(--mono);
            font-size: 10px;
        }
        .nodes-sidebar-list {
            flex: 1;
            overflow-y: auto;
        }
        .node-list-item {
            display: flex;
            align-items: center;
            padding: 4px 8px;
            cursor: pointer;
            border-bottom: 1px solid var(--border-subtle);
            gap: 6px;
        }
        .node-list-item:hover { background: var(--bg-hover); }
        .node-list-item.selected {
            background: var(--accent-dim);
            border-left: 2px solid var(--accent);
        }
        .node-list-item .status-dot {
            width: 6px;
            height: 6px;
            border-radius: 50%;
            flex-shrink: 0;
        }
        .node-list-item .status-dot.healthy { background: var(--success); }
        .node-list-item .status-dot.degraded { background: var(--warning); }
        .node-list-item .status-dot.unhealthy { background: var(--danger); }
        .node-list-item .node-id {
            flex: 1;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            font-family: var(--mono);
            font-size: 10px;
            color: var(--text-secondary);
        }
        .node-list-item .failure-badge {
            background: var(--danger-dim);
            color: var(--danger);
            padding: 1px 4px;
            border-radius: 2px;
            font-family: var(--mono);
            font-size: 9px;
            font-weight: 600;
        }
        .nodes-detail {
            flex: 1;
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 4px;
            overflow: hidden;
            display: flex;
            flex-direction: column;
        }
        .nodes-detail-header {
            padding: 12px;
            background: var(--bg-tertiary);
            border-bottom: 1px solid var(--border);
        }
        .nodes-detail-content {
            flex: 1;
            overflow-y: auto;
            padding: 12px;
        }
        .nodes-detail-empty {
            display: flex;
            align-items: center;
            justify-content: center;
            height: 100%;
            color: var(--text-muted);
            font-family: var(--mono);
            font-size: 11px;
        }
        .node-info-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
            gap: 6px;
            margin-bottom: 16px;
        }
        .node-info-item {
            background: var(--bg-tertiary);
            padding: 8px;
            border-radius: 3px;
            border: 1px solid var(--border-subtle);
        }
        .node-info-item label {
            display: block;
            font-family: var(--mono);
            font-size: 9px;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.05em;
            margin-bottom: 2px;
        }
        .node-info-item span {
            font-family: var(--mono);
            font-size: 11px;
            color: var(--text-primary);
        }
        .node-events-list {
            background: var(--bg-primary);
            border-radius: 3px;
            border: 1px solid var(--border);
            max-height: 350px;
            overflow-y: auto;
        }
        .node-event-item {
            display: flex;
            align-items: flex-start;
            padding: 6px 8px;
            border-bottom: 1px solid var(--border-subtle);
            font-size: 11px;
            gap: 8px;
        }
        .node-event-item:last-child { border-bottom: none; }
        .node-event-time {
            font-family: var(--mono);
            color: var(--text-muted);
            font-size: 10px;
            white-space: nowrap;
            min-width: 70px;
        }
        .node-event-icon {
            width: 14px;
            text-align: center;
            font-size: 10px;
        }
        .node-event-msg {
            flex: 1;
            font-family: var(--mono);
            color: var(--text-secondary);
            font-size: 11px;
        }
        .nodes-sidebar-stats {
            padding: 6px 8px;
            background: var(--bg-tertiary);
            border-top: 1px solid var(--border);
            font-family: var(--mono);
            font-size: 10px;
            color: var(--text-muted);
        }
        
        /* Utility classes */
        .mono { font-family: var(--mono); }
        .text-muted { color: var(--text-muted); }
        .text-accent { color: var(--accent); }
        .text-success { color: var(--success); }
        .text-danger { color: var(--danger); }
        .text-warning { color: var(--warning); }
        
        /* Scrollbar styling */
        ::-webkit-scrollbar { width: 8px; height: 8px; }
        ::-webkit-scrollbar-track { background: var(--bg-primary); }
        ::-webkit-scrollbar-thumb { background: var(--border); border-radius: 4px; }
        ::-webkit-scrollbar-thumb:hover { background: var(--text-muted); }
        
        /* Input focus states */
        input:focus, select:focus {
            outline: none;
            border-color: var(--accent);
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div class="header-left">
                <h1>{{.Name}}</h1>
                <span class="header-badge">stress test</span>
            </div>
            <div class="header-meta">
                <span><span class="label">start</span> <span class="value">{{.StartTime}}</span></span>
                <span><span class="label">duration</span> <span class="value">{{.Duration}}</span></span>
                <span><span class="label">nodes</span> <span class="value">{{.TotalNodes}}</span></span>
                <span><span class="label">rate</span> <span class="value">{{.FailureRate}}/min/1k</span></span>
                <span><span class="label">seed</span> <span class="value">{{if .Seed}}{{.Seed}}{{else}}random{{end}}</span></span>
            </div>
        </div>

        <div class="tabs">
            <button class="tab active" onclick="showTab('results')">Results</button>
            <button class="tab" onclick="showTab('timeline')">Timeline</button>
            <button class="tab" onclick="showTab('nodes')">Nodes ({{.TotalNodes}})</button>
            <button class="tab" onclick="showTab('config')">Config</button>
        </div>

        <!-- Results Tab -->
        <div id="results" class="tab-content active">
            <div class="section-header">Key Metrics</div>
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

            <div class="section-header">Health Timeline</div>
            <div class="charts-grid">
                <div class="chart-card">
                    <h3>Node Status Distribution</h3>
                    <div class="chart-container">
                        <canvas id="healthChart"></canvas>
                    </div>
                </div>
                <div class="chart-card">
                    <h3>Failures vs Recoveries (cumulative)</h3>
                    <div class="chart-container">
                        <canvas id="failuresChart"></canvas>
                    </div>
                </div>
            </div>

            <div class="section-header">Failure Breakdown</div>
            <div class="charts-grid">
                <div class="chart-card">
                    <h3>By Type</h3>
                    <div class="chart-container">
                        <canvas id="failureTypeChart"></canvas>
                    </div>
                </div>
                <div class="chart-card">
                    <h3>XID Codes</h3>
                    <div class="chart-container">
                        <canvas id="xidChart"></canvas>
                    </div>
                </div>
            </div>

            {{if .TopXIDCodes}}
            <div class="section-header">Top XID Errors</div>
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
                    <div class="legend-item"><div class="legend-color healthy"></div><span>healthy</span></div>
                    <div class="legend-item"><div class="legend-color degraded"></div><span>degraded</span></div>
                    <div class="legend-item"><div class="legend-color unhealthy"></div><span>unhealthy</span></div>
                    <div class="legend-item"><div class="legend-dot failure"></div><span>failure</span></div>
                    <div class="legend-item"><div class="legend-dot recovery"></div><span>recovery</span></div>
                </div>
                <input type="text" id="timelineSearch" placeholder="filter..." 
                       style="padding: 4px 8px; background: var(--bg-primary); border: 1px solid var(--border); border-radius: 2px; color: var(--text-primary); width: 120px; font-family: var(--mono); font-size: 10px;">
                <select id="timelineFilter" style="padding: 4px 6px; background: var(--bg-primary); border: 1px solid var(--border); border-radius: 2px; color: var(--text-secondary); font-family: var(--mono); font-size: 10px;">
                    <option value="">all nodes</option>
                    <option value="has_events">with events</option>
                    <option value="unhealthy">unhealthy</option>
                    <option value="degraded">degraded</option>
                </select>
                <div class="zoom-controls">
                    <button class="zoom-btn" onclick="zoomTimeline(-1)">−</button>
                    <span style="padding: 0 6px; color: var(--text-muted); font-family: var(--mono); font-size: 10px;" id="zoomLevel">100%</span>
                    <button class="zoom-btn" onclick="zoomTimeline(1)">+</button>
                </div>
                <span style="color: var(--text-muted); font-family: var(--mono); font-size: 10px;"><span id="timelineVisibleNodes">0</span> nodes</span>
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
            <div class="section-header">Test Configuration</div>

            <div class="config-section">
                <h3>General</h3>
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
        // Chart.js global config for dark theme
        Chart.defaults.color = '#888888';
        Chart.defaults.borderColor = '#2a2a2a';
        Chart.defaults.font.family = "'JetBrains Mono', monospace";
        Chart.defaults.font.size = 10;

        function showTab(tabId) {
            document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
            document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
            document.getElementById(tabId).classList.add('active');
            event.target.classList.add('active');
        }

        // Color palette
        const colors = {
            success: '#00ff88',
            warning: '#ffaa00',
            danger: '#ff4444',
            accent: '#00d9ff',
            successDim: 'rgba(0, 255, 136, 0.1)',
            warningDim: 'rgba(255, 170, 0, 0.1)',
            dangerDim: 'rgba(255, 68, 68, 0.1)',
            accentDim: 'rgba(0, 217, 255, 0.1)',
        };

        // Node Health Timeline
        new Chart(document.getElementById('healthChart'), {
            type: 'line',
            data: {
                labels: {{.TimelineLabels}},
                datasets: [
                    { label: 'Healthy', data: {{.HealthyData}}, borderColor: colors.success, backgroundColor: colors.successDim, fill: true, tension: 0.4, borderWidth: 1.5, pointRadius: 0 },
                    { label: 'Degraded', data: {{.DegradedData}}, borderColor: colors.warning, backgroundColor: colors.warningDim, fill: true, tension: 0.4, borderWidth: 1.5, pointRadius: 0 },
                    { label: 'Unhealthy', data: {{.UnhealthyData}}, borderColor: colors.danger, backgroundColor: colors.dangerDim, fill: true, tension: 0.4, borderWidth: 1.5, pointRadius: 0 }
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { intersect: false, mode: 'index' },
                scales: {
                    y: { beginAtZero: true, grid: { color: '#1a1a1a' } },
                    x: { grid: { display: false } }
                },
                plugins: { legend: { position: 'bottom', labels: { boxWidth: 12, padding: 15 } } }
            }
        });

        // Failures & Recoveries Timeline
        new Chart(document.getElementById('failuresChart'), {
            type: 'line',
            data: {
                labels: {{.TimelineLabels}},
                datasets: [
                    { label: 'Failures', data: {{.FailuresData}}, borderColor: colors.danger, backgroundColor: colors.dangerDim, fill: true, tension: 0.4, borderWidth: 1.5, pointRadius: 0 },
                    { label: 'Recoveries', data: {{.RecoveriesData}}, borderColor: colors.success, backgroundColor: colors.successDim, fill: true, tension: 0.4, borderWidth: 1.5, pointRadius: 0 }
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { intersect: false, mode: 'index' },
                scales: {
                    y: { beginAtZero: true, grid: { color: '#1a1a1a' } },
                    x: { grid: { display: false } }
                },
                plugins: { legend: { position: 'bottom', labels: { boxWidth: 12, padding: 15 } } }
            }
        });

        // Failure Type Distribution
        new Chart(document.getElementById('failureTypeChart'), {
            type: 'doughnut',
            data: {
                labels: {{.FailureTypeLabels}},
                datasets: [{ data: {{.FailureTypeData}}, backgroundColor: ['#ff4444', '#ffaa00', '#00d9ff', '#00ff88', '#a855f7', '#ec4899', '#60a5fa', '#4ade80', '#f97316', '#ef4444'], borderWidth: 0 }]
            },
            options: { responsive: true, maintainAspectRatio: false, cutout: '60%', plugins: { legend: { position: 'right', labels: { boxWidth: 10, padding: 8 } } } }
        });

        // XID Distribution (Results)
        new Chart(document.getElementById('xidChart'), {
            type: 'bar',
            data: {
                labels: {{.XIDLabels}},
                datasets: [{ label: 'Count', data: {{.XIDData}}, backgroundColor: colors.accent, borderRadius: 2 }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                indexAxis: 'y',
                scales: {
                    x: { beginAtZero: true, grid: { color: '#1a1a1a' } },
                    y: { grid: { display: false } }
                },
                plugins: { legend: { display: false } }
            }
        });

        // XID Distribution Chart (Config)
        {{if .XIDDistribution}}
        new Chart(document.getElementById('xidDistChart'), {
            type: 'doughnut',
            data: {
                labels: {{.XIDDistLabels}},
                datasets: [{ data: {{.XIDDistData}}, backgroundColor: ['#ff4444', '#ffaa00', '#00d9ff', '#00ff88', '#a855f7', '#ec4899', '#60a5fa', '#4ade80', '#f97316', '#ef4444'], borderWidth: 0 }]
            },
            options: { responsive: true, maintainAspectRatio: false, cutout: '60%', plugins: { legend: { position: 'right', labels: { boxWidth: 10, padding: 8 } } } }
        });
        {{end}}
        
        // Timeline management
        const timelineNodes = {{.NodesJSON}};
        const testDuration = "{{.Duration}}";
        let timelineZoom = 1;
        let timelineFilteredNodes = [...timelineNodes];
        
        function parseGoDuration(durationStr) {
            const matches = durationStr.match(/(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?/);
            if (!matches) return 60000;
            const hours = parseInt(matches[1]) || 0;
            const minutes = parseInt(matches[2]) || 0;
            const seconds = parseFloat(matches[3]) || 0;
            return (hours * 3600 + minutes * 60 + seconds) * 1000;
        }
        
        const durationMs = parseGoDuration(testDuration);
        
        function renderTimeline() {
            const ruler = document.getElementById('timelineRuler');
            const body = document.getElementById('timelineBody');
            
            const numTicks = Math.min(20, Math.max(6, Math.floor(durationMs / 10000)));
            const tickInterval = durationMs / numTicks;
            
            let rulerHtml = '';
            for (let i = 0; i <= numTicks; i++) {
                const time = (i * tickInterval) / 1000;
                let label = time >= 60 
                    ? Math.floor(time / 60) + 'm' + (time % 60 > 0 ? Math.floor(time % 60) + 's' : '')
                    : Math.floor(time) + 's';
                rulerHtml += '<div class="timeline-tick">' + label + '</div>';
            }
            ruler.innerHTML = rulerHtml;
            
            let minTime = Infinity;
            timelineFilteredNodes.forEach(node => {
                if (node.events && node.events.length > 0) {
                    const firstEvent = new Date(node.events[0].timestamp).getTime();
                    if (firstEvent < minTime) minTime = firstEvent;
                }
            });
            if (minTime === Infinity) minTime = Date.now() - durationMs;
            
            let bodyHtml = '';
            timelineFilteredNodes.forEach(node => {
                const statusColor = node.status === 'healthy' ? colors.success : 
                                  node.status === 'degraded' ? colors.warning : colors.danger;
                const hasFailures = node.events && node.events.some(e => e.type === 'failure');
                
                bodyHtml += '<div class="timeline-row' + (hasFailures ? ' has-failures' : '') + '">';
                bodyHtml += '<div class="timeline-node-label" title="' + node.node_id + '" onclick="selectNodeFromTimeline(\'' + node.node_id + '\')">';
                bodyHtml += '<span style="width: 5px; height: 5px; border-radius: 50%; background: ' + statusColor + '; flex-shrink: 0;"></span>';
                bodyHtml += '<span style="overflow: hidden; text-overflow: ellipsis;">' + node.node_id + '</span>';
                bodyHtml += '</div>';
                bodyHtml += '<div class="timeline-track" data-node="' + node.node_id + '">';
                
                if (node.events && node.events.length > 0) {
                    let currentStatus = 'pending';
                    let segmentStart = 0;
                    
                    node.events.forEach((event, idx) => {
                        const eventTime = new Date(event.timestamp).getTime();
                        const eventPos = ((eventTime - minTime) / durationMs) * 100 * timelineZoom;
                        
                        if (idx > 0 || event.type === 'started') {
                            const segmentWidth = eventPos - segmentStart;
                            if (segmentWidth > 0.1) {
                                bodyHtml += '<div class="timeline-segment ' + currentStatus + '" style="left: ' + segmentStart + '%; width: ' + segmentWidth + '%;"></div>';
                            }
                        }
                        
                        if (event.type === 'started' || event.type === 'recovery') currentStatus = 'healthy';
                        else if (event.type === 'failure') currentStatus = 'unhealthy';
                        else if (event.type === 'status_change' && event.status) currentStatus = event.status;
                        segmentStart = eventPos;
                        
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
                    
                    const finalWidth = (100 * timelineZoom) - segmentStart;
                    if (finalWidth > 0.1) {
                        bodyHtml += '<div class="timeline-segment ' + currentStatus + '" style="left: ' + segmentStart + '%; width: ' + finalWidth + '%;"></div>';
                    }
                } else {
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
            
            const time = new Date(event.timestamp).toLocaleTimeString();
            let typeLabel = event.type.replace('_', ' ').toUpperCase();
            let color = event.type === 'failure' ? colors.danger : 
                       event.type === 'recovery' ? colors.success : colors.accent;
            
            let html = '<div style="margin-bottom: 6px; font-weight: 600; color: ' + color + '; font-size: 10px; letter-spacing: 0.05em;">' + typeLabel + '</div>';
            html += '<div style="margin-bottom: 3px; color: #888;"><span style="color: #555;">node</span> ' + nodeId + '</div>';
            html += '<div style="margin-bottom: 3px; color: #888;"><span style="color: #555;">time</span> ' + time + '</div>';
            if (event.message) html += '<div style="margin-bottom: 3px; color: #e5e5e5;">' + event.message + '</div>';
            if (event.xid_code) html += '<div style="color: ' + colors.danger + ';"><span style="color: #555;">xid</span> ' + event.xid_code + '</div>';
            
            tooltip.innerHTML = html;
            tooltip.style.display = 'block';
            tooltip.style.left = (e.clientX + 12) + 'px';
            tooltip.style.top = (e.clientY - 10) + 'px';
        }
        
        function hideEventTooltip() {
            document.getElementById('timelineTooltip').style.display = 'none';
        }
        
        function zoomTimeline(direction) {
            if (direction > 0 && timelineZoom < 5) timelineZoom *= 1.5;
            else if (direction < 0 && timelineZoom > 0.5) timelineZoom /= 1.5;
            document.getElementById('zoomLevel').textContent = Math.round(timelineZoom * 100) + '%';
            renderTimeline();
        }
        
        function filterTimeline() {
            const search = document.getElementById('timelineSearch').value.toLowerCase();
            const filter = document.getElementById('timelineFilter').value;
            
            timelineFilteredNodes = timelineNodes.filter(node => {
                const matchesSearch = !search || node.node_id.toLowerCase().includes(search);
                let matchesFilter = true;
                if (filter === 'has_events') matchesFilter = node.events && node.events.length > 1;
                else if (filter === 'unhealthy') matchesFilter = node.status === 'unhealthy';
                else if (filter === 'degraded') matchesFilter = node.status === 'degraded';
                return matchesSearch && matchesFilter;
            });
            renderTimeline();
        }
        
        if (timelineNodes && timelineNodes.length > 0) {
            renderTimeline();
            document.getElementById('timelineSearch').addEventListener('input', filterTimeline);
            document.getElementById('timelineFilter').addEventListener('change', filterTimeline);
        }
        
        // Node management
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
                if (failureCount > 0) html += '<span class="failure-badge">' + failureCount + '</span>';
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
            const statusColor = node.status === 'healthy' ? colors.success : 
                              node.status === 'degraded' ? colors.warning : colors.danger;
            
            let html = '<div class="nodes-detail-header">';
            html += '<div style="display: flex; align-items: center; gap: 8px;">';
            html += '<span style="width: 8px; height: 8px; border-radius: 50%; background: ' + statusColor + ';"></span>';
            html += '<code style="margin: 0; color: #e5e5e5; font-size: 12px;">' + node.node_id + '</code>';
            html += '<span class="badge ' + (node.status === 'healthy' ? 'enabled' : node.status === 'degraded' ? 'recoverable' : 'fatal') + '">' + node.status + '</span>';
            html += '</div></div>';
            
            html += '<div class="nodes-detail-content">';
            html += '<div class="node-info-grid">';
            html += '<div class="node-info-item"><label>Provider</label><span>' + node.provider + '</span></div>';
            html += '<div class="node-info-item"><label>Region</label><span>' + node.region + '</span></div>';
            html += '<div class="node-info-item"><label>Zone</label><span>' + node.zone + '</span></div>';
            html += '<div class="node-info-item"><label>Instance</label><span>' + node.instance_type + '</span></div>';
            html += '<div class="node-info-item"><label>GPUs</label><span>' + node.gpu_count + '× ' + node.gpu_type + '</span></div>';
            html += '<div class="node-info-item"><label>Failures</label><span style="color: ' + colors.danger + ';">' + node.failure_count + '</span></div>';
            html += '</div>';
            
            if (node.log_file) {
                html += '<div style="margin-bottom: 12px;">';
                html += '<label style="display: block; font-family: var(--mono); font-size: 9px; color: #555; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 4px;">Log File</label>';
                html += '<a href="' + node.log_file + '" target="_blank" style="display: block; font-size: 11px; color: ' + colors.accent + '; word-break: break-all; background: #0a0a0a; padding: 6px 8px; border-radius: 2px; text-decoration: none; font-family: var(--mono); border: 1px solid #2a2a2a;">' + node.log_file + ' ↗</a>';
                html += '</div>';
            }
            
            html += '<div style="font-family: var(--mono); font-size: 9px; color: #555; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 6px;">Events (' + (node.events ? node.events.length : 0) + ')</div>';
            html += '<div class="node-events-list">';
            
            if (node.events && node.events.length > 0) {
                node.events.forEach(event => {
                    const time = new Date(event.timestamp).toLocaleTimeString();
                    let icon = '•', color = '#555';
                    
                    if (event.type === 'started') { icon = '▸'; color = colors.success; }
                    else if (event.type === 'failure') { icon = '✕'; color = colors.danger; }
                    else if (event.type === 'recovery') { icon = '↺'; color = colors.success; }
                    else if (event.type === 'status_change') { icon = '◆'; color = colors.accent; }
                    
                    html += '<div class="node-event-item">';
                    html += '<span class="node-event-time">' + time + '</span>';
                    html += '<span class="node-event-icon" style="color: ' + color + ';">' + icon + '</span>';
                    html += '<span class="node-event-msg">' + event.message;
                    if (event.xid_code) html += ' <span style="color: ' + colors.danger + ';">[XID ' + event.xid_code + ']</span>';
                    html += '</span></div>';
                });
            } else {
                html += '<div style="padding: 12px; color: #555; text-align: center; font-family: var(--mono); font-size: 10px;">No events recorded</div>';
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
                return matchesSearch && (!statusFilter || node.status === statusFilter);
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
        
        window.selectNodeFromTimeline = function(nodeId) {
            showTab('nodes');
            setTimeout(() => selectNode(nodeId), 100);
        };
        
        if (allNodes && allNodes.length > 0) {
            filterNodes();
            document.getElementById('nodeSearch').addEventListener('input', filterNodes);
            document.getElementById('statusFilter').addEventListener('change', filterNodes);
            document.getElementById('sortBy').addEventListener('change', filterNodes);
        }
        
        // Keyboard shortcuts
        document.addEventListener('keydown', (e) => {
            if (e.target.tagName === 'INPUT') return;
            if (e.key === '1') showTab('results');
            else if (e.key === '2') showTab('timeline');
            else if (e.key === '3') showTab('nodes');
            else if (e.key === '4') showTab('config');
        });
    </script>
</body>
</html>`
