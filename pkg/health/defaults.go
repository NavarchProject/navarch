package health

// XID error documentation: https://docs.nvidia.com/deploy/xid-errors/index.html
// DCGM health systems: https://docs.nvidia.com/datacenter/dcgm/latest/user-guide/feature-overview.html#health

// DefaultPolicy returns the default health policy.
// The policy is loaded from the embedded default_policy.yaml file.
// See https://docs.nvidia.com/deploy/xid-errors/index.html for XID codes.
func DefaultPolicy() *Policy {
	policy, err := LoadDefaultPolicy()
	if err != nil {
		// This should never happen since the embedded YAML is validated at build time.
		// If it does, return a minimal safe policy.
		return &Policy{
			Rules: []Rule{
				{Name: "default", Condition: "true", Result: ResultHealthy},
			},
		}
	}
	return policy
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
