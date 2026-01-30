package health

// XID error documentation: https://docs.nvidia.com/deploy/xid-errors/index.html
// DCGM health systems: https://docs.nvidia.com/datacenter/dcgm/latest/user-guide/feature-overview.html#health

// DefaultPolicy returns the default health policy with sensible rules
// that match the behavior of the hardcoded XID classification.
// See https://docs.nvidia.com/deploy/xid-errors/index.html for XID codes.
func DefaultPolicy() *Policy {
	return &Policy{
		Rules: []Rule{
			// Fatal XID errors → unhealthy (triggers node replacement)
			// Based on NVIDIA XID documentation and operational experience
			{
				Name:      "fatal-xid",
				Condition: `event.event_type == "xid" && event.metrics.xid_code in [13, 31, 32, 43, 45, 48, 61, 62, 63, 64, 68, 69, 74, 79, 92, 94, 95, 100, 119, 120]`,
				Result:    ResultUnhealthy,
				Priority:  100,
			},

			// Recoverable XID errors → degraded (alerts but no replacement)
			{
				Name:      "recoverable-xid",
				Condition: `event.event_type == "xid" && !(event.metrics.xid_code in [13, 31, 32, 43, 45, 48, 61, 62, 63, 64, 68, 69, 74, 79, 92, 94, 95, 100, 119, 120])`,
				Result:    ResultDegraded,
				Priority:  90,
			},

			// Double-bit ECC errors → unhealthy (uncorrectable)
			{
				Name:      "ecc-dbe",
				Condition: `event.event_type == "ecc_dbe" || (event.system == "DCGM_HEALTH_WATCH_MEM" && has(event.metrics.ecc_dbe_count) && event.metrics.ecc_dbe_count > 0)`,
				Result:    ResultUnhealthy,
				Priority:  100,
			},

			// High single-bit ECC error rate → degraded (correctable but concerning)
			{
				Name:      "ecc-sbe-high",
				Condition: `event.event_type == "ecc_sbe" && has(event.metrics.ecc_sbe_count) && event.metrics.ecc_sbe_count > 100`,
				Result:    ResultDegraded,
				Priority:  80,
			},

			// Critical temperature → unhealthy (thermal throttling or shutdown imminent)
			{
				Name:      "thermal-critical",
				Condition: `event.event_type == "thermal" && has(event.metrics.temperature) && event.metrics.temperature >= 95`,
				Result:    ResultUnhealthy,
				Priority:  100,
			},

			// High temperature → degraded (thermal throttling likely)
			{
				Name:      "thermal-warning",
				Condition: `event.event_type == "thermal" && has(event.metrics.temperature) && event.metrics.temperature >= 85`,
				Result:    ResultDegraded,
				Priority:  50,
			},

			// NVLink errors → degraded (affects multi-GPU workloads)
			{
				Name:      "nvlink-error",
				Condition: `event.event_type == "nvlink" || event.system == "DCGM_HEALTH_WATCH_NVLINK"`,
				Result:    ResultDegraded,
				Priority:  60,
			},

			// PCIe errors → degraded (connectivity issues)
			{
				Name:      "pcie-error",
				Condition: `event.event_type == "pcie" || event.system == "DCGM_HEALTH_WATCH_PCIE"`,
				Result:    ResultDegraded,
				Priority:  60,
			},

			// Power issues → degraded
			{
				Name:      "power-warning",
				Condition: `event.event_type == "power" || event.system == "DCGM_HEALTH_WATCH_POWER"`,
				Result:    ResultDegraded,
				Priority:  50,
			},

			// Default: if no rule matches, the event is informational (healthy)
			{
				Name:      "default-healthy",
				Condition: `true`,
				Result:    ResultHealthy,
				Priority:  0,
			},
		},
	}
}

// FatalXIDCodes returns the list of XID codes considered fatal.
// This is used for documentation and backward compatibility.
var FatalXIDCodes = []int{
	13,  // Graphics Engine Exception
	31,  // GPU memory page fault
	32,  // Invalid or corrupted push buffer stream
	43,  // GPU stopped processing
	45,  // Preemptive cleanup, due to previous errors
	48,  // Double Bit ECC Error
	61,  // Internal micro-controller breakpoint/warning
	62,  // Internal micro-controller halt
	63,  // ECC page retirement or row remapping event
	64,  // ECC page retirement or row remapping recording failure
	68,  // Video processor exception
	69,  // Graphics Engine class error
	74,  // NVLINK Error
	79,  // GPU has fallen off the bus
	92,  // High single-bit ECC error rate
	94,  // Contained ECC error
	95,  // Uncontained ECC error
	100, // Timeout waiting for semaphore
	119, // GSP RPC timeout
	120, // GSP error
}
