package simulator

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DefaultGPUPricing contains realistic hourly prices (USD) for common GPU instance types.
// Prices are approximate and based on major cloud providers (GCP, AWS, Lambda Labs) as of 2024.
// Users can override these in their scenario YAML files.
var DefaultGPUPricing = map[string]float64{
	// H100 instances (high-end training)
	"h100-8gpu":     28.00, // 8x H100 80GB SXM5 (~$3.50/GPU/hr)
	"h100-4gpu":     14.00, // 4x H100 80GB
	"h100-2gpu":     7.00,  // 2x H100 80GB
	"h100-1gpu":     3.50,  // 1x H100 80GB

	// A100 instances (training/inference)
	"a100-8gpu":     16.00, // 8x A100 80GB (~$2.00/GPU/hr)
	"a100-4gpu":     8.00,  // 4x A100 80GB
	"a100-2gpu":     4.00,  // 2x A100 80GB
	"a100-1gpu":     2.00,  // 1x A100 80GB
	"a100-40gb-8gpu": 12.00, // 8x A100 40GB (~$1.50/GPU/hr)
	"a100-40gb-4gpu": 6.00,  // 4x A100 40GB

	// A10 instances (inference)
	"a10-8gpu":      8.00,  // 8x A10 (~$1.00/GPU/hr)
	"a10-4gpu":      4.00,  // 4x A10
	"a10-2gpu":      2.00,  // 2x A10
	"a10-1gpu":      1.00,  // 1x A10

	// L4 instances (inference/light training)
	"l4-8gpu":       4.80,  // 8x L4 (~$0.60/GPU/hr)
	"l4-4gpu":       2.40,  // 4x L4
	"l4-2gpu":       1.20,  // 2x L4
	"l4-1gpu":       0.60,  // 1x L4

	// T4 instances (inference)
	"t4-4gpu":       1.40,  // 4x T4 (~$0.35/GPU/hr)
	"t4-2gpu":       0.70,  // 2x T4
	"t4-1gpu":       0.35,  // 1x T4

	// V100 instances (legacy)
	"v100-8gpu":     12.00, // 8x V100 (~$1.50/GPU/hr)
	"v100-4gpu":     6.00,  // 4x V100
	"v100-2gpu":     3.00,  // 2x V100
	"v100-1gpu":     1.50,  // 1x V100
}

// GPUTypePricing maps GPU types to per-GPU hourly prices (fallback pricing).
var GPUTypePricing = map[string]float64{
	"NVIDIA H100 80GB HBM3":  3.50,
	"NVIDIA H100 80GB":       3.50,
	"NVIDIA H100":            3.50,
	"NVIDIA A100 80GB":       2.00,
	"NVIDIA A100 40GB":       1.50,
	"NVIDIA A100":            1.75,
	"NVIDIA A10":             1.00,
	"NVIDIA L4":              0.60,
	"NVIDIA T4":              0.35,
	"NVIDIA V100":            1.50,
}

// CostReport contains all cost-related metrics for a stress test.
type CostReport struct {
	// Total costs
	TotalCost            float64 `json:"total_cost"`              // Total cost of running the fleet
	TotalComputeHours    float64 `json:"total_compute_hours"`     // Total node-hours of compute
	TotalGPUHours        float64 `json:"total_gpu_hours"`         // Total GPU-hours of compute
	EffectiveCostPerHour float64 `json:"effective_cost_per_hour"` // Average hourly cost

	// Wasted compute (time nodes were unhealthy but still billed)
	WastedCost        float64 `json:"wasted_cost"`         // Cost of compute lost to failures
	WastedComputeHours float64 `json:"wasted_compute_hours"` // Hours of compute lost
	WastedGPUHours    float64 `json:"wasted_gpu_hours"`     // GPU-hours lost to failures
	WastedPercentage  float64 `json:"wasted_percentage"`    // Percentage of total cost wasted

	// Cost per failure
	AvgCostPerFailure   float64 `json:"avg_cost_per_failure"`    // Average cost impact per failure event
	TotalFailureCost    float64 `json:"total_failure_cost"`      // Total cost attributed to failures

	// Breakdown by dimension
	CostByGPUType   map[string]float64 `json:"cost_by_gpu_type"`   // Cost breakdown by GPU type
	CostByProvider  map[string]float64 `json:"cost_by_provider"`   // Cost breakdown by cloud provider
	CostByRegion    map[string]float64 `json:"cost_by_region"`     // Cost breakdown by region
	CostByTemplate  map[string]float64 `json:"cost_by_template"`   // Cost breakdown by node template

	// Per-GPU-type efficiency
	GPUTypeEfficiency map[string]GPUEfficiency `json:"gpu_type_efficiency"` // Efficiency metrics per GPU type

	// Top cost drivers
	TopCostDrivers []CostDriver `json:"top_cost_drivers"` // Top contributors to cost
}

// GPUEfficiency tracks efficiency metrics for a GPU type.
type GPUEfficiency struct {
	GPUType         string  `json:"gpu_type"`
	TotalCost       float64 `json:"total_cost"`
	TotalHours      float64 `json:"total_hours"`
	WastedCost      float64 `json:"wasted_cost"`
	WastedHours     float64 `json:"wasted_hours"`
	EfficiencyPct   float64 `json:"efficiency_pct"`   // (Total - Wasted) / Total * 100
	FailureCount    int64   `json:"failure_count"`
	CostPerFailure  float64 `json:"cost_per_failure"`
}

// CostDriver represents a significant cost contributor.
type CostDriver struct {
	Category    string  `json:"category"`    // "gpu_type", "provider", "region", "template"
	Name        string  `json:"name"`        // Specific name
	TotalCost   float64 `json:"total_cost"`
	Percentage  float64 `json:"percentage"`  // Percentage of total cost
	NodeCount   int     `json:"node_count"`
}

// NodeCostTracker tracks cost-related data for a single node.
type NodeCostTracker struct {
	NodeID       string
	Spec         NodeSpec
	PricePerHour float64

	StartTime       time.Time
	EndTime         time.Time     // Set when node stops (or test ends)
	TotalUptime     time.Duration // Total time node was running
	UnhealthyTime   time.Duration // Time spent in unhealthy/degraded state
	FailureCount    int64

	// State tracking
	lastStateChange time.Time
	currentState    string // "healthy", "unhealthy", "degraded"
}

// CostCalculator calculates cost metrics from stress test data.
type CostCalculator struct {
	nodeTrackers map[string]*NodeCostTracker
	startTime    time.Time
	endTime      time.Time
}

// NewCostCalculator creates a new cost calculator.
func NewCostCalculator() *CostCalculator {
	return &CostCalculator{
		nodeTrackers: make(map[string]*NodeCostTracker),
	}
}

// RegisterNode registers a node for cost tracking.
func (c *CostCalculator) RegisterNode(spec NodeSpec, pricePerHour float64) {
	now := time.Now()
	if c.startTime.IsZero() {
		c.startTime = now
	}

	c.nodeTrackers[spec.ID] = &NodeCostTracker{
		NodeID:          spec.ID,
		Spec:            spec,
		PricePerHour:    pricePerHour,
		StartTime:       now,
		lastStateChange: now,
		currentState:    "healthy",
	}
}

// UpdateNodeState updates the health state of a node.
func (c *CostCalculator) UpdateNodeState(nodeID, newState string) {
	tracker, ok := c.nodeTrackers[nodeID]
	if !ok {
		return
	}

	now := time.Now()
	duration := now.Sub(tracker.lastStateChange)

	// Accumulate time in previous state
	if tracker.currentState == "unhealthy" || tracker.currentState == "degraded" {
		tracker.UnhealthyTime += duration
	}

	tracker.lastStateChange = now
	tracker.currentState = newState
}

// RecordFailure records a failure event for a node.
func (c *CostCalculator) RecordFailure(nodeID string) {
	if tracker, ok := c.nodeTrackers[nodeID]; ok {
		tracker.FailureCount++
	}
}

// Finalize closes out all tracking for end-of-test calculations.
func (c *CostCalculator) Finalize() {
	c.endTime = time.Now()

	for _, tracker := range c.nodeTrackers {
		// Close out current state
		now := c.endTime
		if tracker.EndTime.IsZero() {
			tracker.EndTime = now
		}

		duration := now.Sub(tracker.lastStateChange)
		if tracker.currentState == "unhealthy" || tracker.currentState == "degraded" {
			tracker.UnhealthyTime += duration
		}

		tracker.TotalUptime = tracker.EndTime.Sub(tracker.StartTime)
	}
}

// GenerateCostReport generates a comprehensive cost report.
func (c *CostCalculator) GenerateCostReport() *CostReport {
	report := &CostReport{
		CostByGPUType:     make(map[string]float64),
		CostByProvider:    make(map[string]float64),
		CostByRegion:      make(map[string]float64),
		CostByTemplate:    make(map[string]float64),
		GPUTypeEfficiency: make(map[string]GPUEfficiency),
	}

	// Track per-GPU-type data for efficiency calculation
	gpuTypeData := make(map[string]struct {
		totalCost    float64
		totalHours   float64
		wastedCost   float64
		wastedHours  float64
		failureCount int64
	})

	// Node counts for cost drivers
	nodeCountByGPUType := make(map[string]int)
	nodeCountByProvider := make(map[string]int)
	nodeCountByRegion := make(map[string]int)
	nodeCountByTemplate := make(map[string]int)

	var totalFailures int64

	for _, tracker := range c.nodeTrackers {
		uptimeHours := tracker.TotalUptime.Hours()
		unhealthyHours := tracker.UnhealthyTime.Hours()
		nodeCost := uptimeHours * tracker.PricePerHour
		wastedCost := unhealthyHours * tracker.PricePerHour
		gpuHours := uptimeHours * float64(tracker.Spec.GPUCount)
		wastedGPUHours := unhealthyHours * float64(tracker.Spec.GPUCount)

		// Totals
		report.TotalCost += nodeCost
		report.TotalComputeHours += uptimeHours
		report.TotalGPUHours += gpuHours
		report.WastedCost += wastedCost
		report.WastedComputeHours += unhealthyHours
		report.WastedGPUHours += wastedGPUHours
		totalFailures += tracker.FailureCount

		// By GPU type
		gpuType := tracker.Spec.GPUType
		report.CostByGPUType[gpuType] += nodeCost
		nodeCountByGPUType[gpuType]++
		data := gpuTypeData[gpuType]
		data.totalCost += nodeCost
		data.totalHours += uptimeHours
		data.wastedCost += wastedCost
		data.wastedHours += unhealthyHours
		data.failureCount += tracker.FailureCount
		gpuTypeData[gpuType] = data

		// By provider
		report.CostByProvider[tracker.Spec.Provider] += nodeCost
		nodeCountByProvider[tracker.Spec.Provider]++

		// By region
		report.CostByRegion[tracker.Spec.Region] += nodeCost
		nodeCountByRegion[tracker.Spec.Region]++

		// By template
		if tracker.Spec.TemplateName != "" {
			report.CostByTemplate[tracker.Spec.TemplateName] += nodeCost
			nodeCountByTemplate[tracker.Spec.TemplateName]++
		}
	}

	// Calculate derived metrics
	testDuration := c.endTime.Sub(c.startTime).Hours()
	if testDuration > 0 {
		report.EffectiveCostPerHour = report.TotalCost / testDuration
	}

	if report.TotalCost > 0 {
		report.WastedPercentage = (report.WastedCost / report.TotalCost) * 100
	}

	if totalFailures > 0 {
		report.AvgCostPerFailure = report.WastedCost / float64(totalFailures)
		report.TotalFailureCost = report.WastedCost
	}

	// Calculate GPU type efficiency
	for gpuType, data := range gpuTypeData {
		efficiency := GPUEfficiency{
			GPUType:      gpuType,
			TotalCost:    data.totalCost,
			TotalHours:   data.totalHours,
			WastedCost:   data.wastedCost,
			WastedHours:  data.wastedHours,
			FailureCount: data.failureCount,
		}
		if data.totalCost > 0 {
			efficiency.EfficiencyPct = ((data.totalCost - data.wastedCost) / data.totalCost) * 100
		} else {
			efficiency.EfficiencyPct = 100.0
		}
		if data.failureCount > 0 {
			efficiency.CostPerFailure = data.wastedCost / float64(data.failureCount)
		}
		report.GPUTypeEfficiency[gpuType] = efficiency
	}

	// Compile top cost drivers
	var drivers []CostDriver

	for gpuType, cost := range report.CostByGPUType {
		pct := 0.0
		if report.TotalCost > 0 {
			pct = (cost / report.TotalCost) * 100
		}
		drivers = append(drivers, CostDriver{
			Category:   "gpu_type",
			Name:       gpuType,
			TotalCost:  cost,
			Percentage: pct,
			NodeCount:  nodeCountByGPUType[gpuType],
		})
	}

	for provider, cost := range report.CostByProvider {
		pct := 0.0
		if report.TotalCost > 0 {
			pct = (cost / report.TotalCost) * 100
		}
		drivers = append(drivers, CostDriver{
			Category:   "provider",
			Name:       provider,
			TotalCost:  cost,
			Percentage: pct,
			NodeCount:  nodeCountByProvider[provider],
		})
	}

	// Sort by cost and take top 10
	sort.Slice(drivers, func(i, j int) bool {
		return drivers[i].TotalCost > drivers[j].TotalCost
	})
	if len(drivers) > 10 {
		drivers = drivers[:10]
	}
	report.TopCostDrivers = drivers

	return report
}

// GetPriceForTemplate determines the price for a node template.
// It checks: 1) explicit price_per_hour, 2) template name defaults, 3) GPU type pricing.
func GetPriceForTemplate(template NodeTemplate) float64 {
	// 1. Check if explicit price is set
	if template.PricePerHour > 0 {
		return template.PricePerHour
	}

	// 2. Check default pricing by template name
	templateKey := strings.ToLower(template.Name)
	if price, ok := DefaultGPUPricing[templateKey]; ok {
		return price
	}

	// 3. Fall back to GPU type pricing
	for gpuType, perGPUPrice := range GPUTypePricing {
		if strings.Contains(strings.ToLower(template.GPUType), strings.ToLower(gpuType)) ||
			strings.Contains(template.GPUType, gpuType) {
			return perGPUPrice * float64(template.GPUCount)
		}
	}

	// 4. Last resort: estimate based on GPU count with generic pricing
	// Assume average GPU price of $2.00/hr
	return 2.00 * float64(template.GPUCount)
}

// GetPriceForNodeSpec determines the price for a node spec.
func GetPriceForNodeSpec(spec NodeSpec) float64 {
	// Check if explicit price is set
	if spec.PricePerHour > 0 {
		return spec.PricePerHour
	}

	// Check template name in defaults
	if spec.TemplateName != "" {
		templateKey := strings.ToLower(spec.TemplateName)
		if price, ok := DefaultGPUPricing[templateKey]; ok {
			return price
		}
	}

	// Fall back to GPU type pricing
	for gpuType, perGPUPrice := range GPUTypePricing {
		if strings.Contains(strings.ToLower(spec.GPUType), strings.ToLower(gpuType)) ||
			strings.Contains(spec.GPUType, gpuType) {
			return perGPUPrice * float64(spec.GPUCount)
		}
	}

	// Last resort: generic pricing
	return 2.00 * float64(spec.GPUCount)
}

// FormatCost formats a cost value as a USD string.
func FormatCost(cost float64) string {
	if cost >= 1000 {
		return fmt.Sprintf("$%.2fk", cost/1000)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// FormatCostPrecise formats a cost value with higher precision.
func FormatCostPrecise(cost float64) string {
	if cost >= 1000000 {
		return fmt.Sprintf("$%.2fM", cost/1000000)
	}
	if cost >= 1000 {
		return fmt.Sprintf("$%.2fk", cost/1000)
	}
	return fmt.Sprintf("$%.4f", cost)
}
