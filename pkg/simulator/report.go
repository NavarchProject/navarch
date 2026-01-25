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
	HasConfig           bool
	TestDuration        string
	MetricsInterval     string
	Seed                int64

	// Fleet generation config
	HasFleetGen         bool
	FleetTemplates      []fleetTemplateData
	FleetProviders      []kvPair
	FleetRegions        []kvPair
	StartupPattern      string
	StartupDuration     string
	StartupBatchSize    int
	StartupJitter       int

	// Chaos config
	HasChaos            bool
	ChaosEnabled        bool
	XIDDistribution     []xidDistData
	FailureTypeWeights  []kvPair

	// Cascading config
	HasCascading        bool
	CascadeProbability  float64
	CascadeMaxDepth     int
	CascadeMinDelay     string
	CascadeMaxDelay     string
	CascadeScope        string
	CascadeMaxAffected  float64

	// Recovery config
	HasRecovery         bool
	RecoveryProbability float64
	RecoveryMeanTime    string
	RecoveryStdDev      string

	// Scheduled outages
	ScheduledOutages    []outageData

	// Chart data for config visualization
	XIDDistLabels       template.JS
	XIDDistData         template.JS
	ProviderLabels      template.JS
	ProviderData        template.JS
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

	return data
}

func toJSArray(v interface{}) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		// Return empty array on error to prevent broken charts
		return template.JS("[]")
	}
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
    </script>
</body>
</html>`
