package simulator

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed templates/report.html
var htmlReportTemplate string

type HTMLReportGenerator struct {
	report *StressReport
	config *StressConfig
}

func NewHTMLReportGenerator(report *StressReport, config *StressConfig) *HTMLReportGenerator {
	return &HTMLReportGenerator{report: report, config: config}
}

func (g *HTMLReportGenerator) Generate(outputPath string) error {
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

	if err := tmpl.Execute(f, g.prepareTemplateData()); err != nil {
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

	HasConfig       bool
	TestDuration    string
	MetricsInterval string
	Seed            int64

	HasFleetGen      bool
	FleetTemplates   []fleetTemplateData
	FleetProviders   []kvPair
	FleetRegions     []kvPair
	StartupPattern   string
	StartupDuration  string
	StartupBatchSize int
	StartupJitter    int
	HasColdStart     bool
	ColdStartMin     string
	ColdStartMax     string
	ColdStartMean    string
	ColdStartStdDev  string

	HasChaos           bool
	ChaosEnabled       bool
	XIDDistribution    []xidDistData
	FailureTypeWeights []kvPair

	HasCascading       bool
	CascadeProbability float64
	CascadeMaxDepth    int
	CascadeMinDelay    string
	CascadeMaxDelay    string
	CascadeScope       string
	CascadeMaxAffected float64

	HasRecovery         bool
	RecoveryProbability float64
	RecoveryMeanTime    string
	RecoveryStdDev      string

	ScheduledOutages []outageData

	XIDDistLabels  template.JS
	XIDDistData    template.JS
	ProviderLabels template.JS
	ProviderData   template.JS
	RegionLabels   template.JS
	RegionData     template.JS
	TemplateLabels template.JS
	TemplateData   template.JS

	// Policy rule evaluation
	PolicyRuleHits   []policyRuleHitData
	PolicyRuleLabels template.JS
	PolicyRuleData   template.JS
	PolicyRuleColors template.JS

	NodeReports   []NodeReport
	NodesJSON     template.JS
	LogsDirectory string
}

type policyRuleHitData struct {
	Name       string
	Hits       int64
	Result     string
	Priority   int
	Expression string
}

// policyRuleExpressions maps rule names to their CEL expressions.
// From pkg/health/defaults.go.
var policyRuleExpressions = map[string]string{
	"fatal-xid":        `event.event_type == "xid" && event.metrics.xid_code in [13, 31, 32, 43, 45, 48, 61, 62, 63, 64, 68, 69, 74, 79, 92, 94, 95, 100, 119, 120]`,
	"recoverable-xid":  `event.event_type == "xid" && !(event.metrics.xid_code in [...])`,
	"ecc-dbe":          `event.event_type == "ecc_dbe" || (event.system == "DCGM_HEALTH_WATCH_MEM" && event.metrics.ecc_dbe_count > 0)`,
	"ecc-sbe-high":     `event.event_type == "ecc_sbe" && event.metrics.ecc_sbe_count > 100`,
	"thermal-critical": `event.event_type == "thermal" && event.metrics.temperature >= 95`,
	"thermal-warning":  `event.event_type == "thermal" && event.metrics.temperature >= 85`,
	"nvlink-error":     `event.event_type == "nvlink" || event.system == "DCGM_HEALTH_WATCH_NVLINK"`,
	"pcie-error":       `event.event_type == "pcie" || event.system == "DCGM_HEALTH_WATCH_PCIE"`,
	"power-warning":    `event.event_type == "power" || event.system == "DCGM_HEALTH_WATCH_POWER"`,
	"default":  `true`,
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

	var ftLabels []string
	var ftData []int64
	for ftype, count := range r.Failures.ByType {
		ftLabels = append(ftLabels, ftype)
		ftData = append(ftData, count)
	}
	data.FailureTypeLabels = toJSArray(ftLabels)
	data.FailureTypeData = toJSArray(ftData)

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

	if c != nil {
		data.HasConfig = true
		data.TestDuration = c.Duration.Duration().String()
		data.MetricsInterval = c.MetricsInterval.Duration().String()
		data.Seed = c.Seed

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
			if c.FleetGen.Startup.ColdStartMin.Duration() > 0 || c.FleetGen.Startup.ColdStartMax.Duration() > 0 ||
				c.FleetGen.Startup.ColdStartMean.Duration() > 0 || c.FleetGen.Startup.ColdStartStdDev.Duration() > 0 {
				data.HasColdStart = true
				if c.FleetGen.Startup.ColdStartMin.Duration() > 0 {
					data.ColdStartMin = c.FleetGen.Startup.ColdStartMin.Duration().String()
				}
				if c.FleetGen.Startup.ColdStartMax.Duration() > 0 {
					data.ColdStartMax = c.FleetGen.Startup.ColdStartMax.Duration().String()
				}
				if c.FleetGen.Startup.ColdStartMean.Duration() > 0 {
					data.ColdStartMean = c.FleetGen.Startup.ColdStartMean.Duration().String()
				}
				if c.FleetGen.Startup.ColdStartStdDev.Duration() > 0 {
					data.ColdStartStdDev = c.FleetGen.Startup.ColdStartStdDev.Duration().String()
				}
			}

			var provLabels []string
			var provData []int
			for p, pct := range c.FleetGen.Providers {
				provLabels = append(provLabels, p)
				provData = append(provData, pct)
			}
			data.ProviderLabels = toJSArray(provLabels)
			data.ProviderData = toJSArray(provData)

			var regLabels []string
			var regData []int
			for r, pct := range c.FleetGen.Regions {
				regLabels = append(regLabels, r)
				regData = append(regData, pct)
			}
			data.RegionLabels = toJSArray(regLabels)
			data.RegionData = toJSArray(regData)

			var tmplLabels []string
			var tmplData []int
			for _, t := range c.FleetGen.Templates {
				tmplLabels = append(tmplLabels, t.Name)
				tmplData = append(tmplData, t.Weight)
			}
			data.TemplateLabels = toJSArray(tmplLabels)
			data.TemplateData = toJSArray(tmplData)
		}

		if c.Chaos != nil {
			data.HasChaos = true
			data.ChaosEnabled = c.Chaos.Enabled

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

			for _, ft := range c.Chaos.FailureTypes {
				data.FailureTypeWeights = append(data.FailureTypeWeights, kvPair{Key: ft.Type, Value: ft.Weight})
			}

			if c.Chaos.Cascading != nil {
				data.HasCascading = true
				data.CascadeProbability = c.Chaos.Cascading.Probability
				data.CascadeMaxDepth = c.Chaos.Cascading.MaxDepth
				data.CascadeMinDelay = c.Chaos.Cascading.MinDelay.Duration().String()
				data.CascadeMaxDelay = c.Chaos.Cascading.MaxDelay.Duration().String()
				data.CascadeScope = c.Chaos.Cascading.Scope
				data.CascadeMaxAffected = c.Chaos.Cascading.MaxAffectedPercent
			}

			if c.Chaos.Recovery != nil {
				data.HasRecovery = true
				data.RecoveryProbability = c.Chaos.Recovery.Probability
				data.RecoveryMeanTime = c.Chaos.Recovery.MeanTime.Duration().String()
				data.RecoveryStdDev = c.Chaos.Recovery.StdDev.Duration().String()
			}

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

	data.NodeReports = r.Nodes
	data.NodesJSON = toJSArray(r.Nodes)
	data.LogsDirectory = r.LogsDirectory

	// Policy rule hits
	if len(r.PolicyRuleHits) > 0 {
		var ruleLabels []string
		var ruleData []int64
		var ruleColors []string
		for _, hit := range r.PolicyRuleHits {
			expr := policyRuleExpressions[hit.Name]
			data.PolicyRuleHits = append(data.PolicyRuleHits, policyRuleHitData{
				Name:       hit.Name,
				Hits:       hit.Hits,
				Result:     hit.Result,
				Priority:   hit.Priority,
				Expression: expr,
			})
			ruleLabels = append(ruleLabels, hit.Name)
			ruleData = append(ruleData, hit.Hits)
			// Color based on result
			switch hit.Result {
			case "unhealthy":
				ruleColors = append(ruleColors, "#e00")
			case "degraded":
				ruleColors = append(ruleColors, "#f5a623")
			default:
				ruleColors = append(ruleColors, "#0070f3")
			}
		}
		data.PolicyRuleLabels = toJSArray(ruleLabels)
		data.PolicyRuleData = toJSArray(ruleData)
		data.PolicyRuleColors = toJSArray(ruleColors)
	}

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
