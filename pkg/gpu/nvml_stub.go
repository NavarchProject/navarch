//go:build !(linux && cgo)

package gpu

func IsNVMLAvailable() bool { return false }

// NewNVML returns nil on platforms without NVML (non-Linux or CGO disabled).
// Only reachable if IsNVMLAvailable() returns true, which it never does here.
func NewNVML() Manager { return nil }
