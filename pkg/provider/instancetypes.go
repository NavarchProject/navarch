package provider

// AbstractType represents a hardware specification independent of provider.
// Users can request "h100-8x" and Navarch maps it to provider-specific types.
type AbstractType struct {
	GPUCount int
	GPUModel string // h100, a100, l4, etc.
}

// InstanceTypeMappings maps abstract instance types to provider-specific type names.
// Key is the abstract type (e.g., "h100-8x"), value is a map of provider to concrete type.
var InstanceTypeMappings = map[string]map[string]string{
	"h100-8x": {
		"lambda": "gpu_8x_h100_sxm5",
		"gcp":    "a3-highgpu-8g",
		"aws":    "p5.48xlarge",
	},
	"h100-1x": {
		"lambda": "gpu_1x_h100_pcie",
		"gcp":    "a3-highgpu-1g",
		"aws":    "p5.xlarge",
	},
	"a100-8x": {
		"lambda": "gpu_8x_a100",
		"gcp":    "a2-highgpu-8g",
		"aws":    "p4d.24xlarge",
	},
	"a100-4x": {
		"lambda": "gpu_4x_a100",
		"gcp":    "a2-highgpu-4g",
		"aws":    "p4de.24xlarge",
	},
	"a100-1x": {
		"lambda": "gpu_1x_a100",
		"gcp":    "a2-highgpu-1g",
	},
	"a10-1x": {
		"lambda": "gpu_1x_a10",
		"gcp":    "g2-standard-4",
		"aws":    "g5.xlarge",
	},
	"l4-1x": {
		"gcp": "g2-standard-4",
		"aws": "g6.xlarge",
	},
}

// ResolveInstanceType maps an abstract type to a provider-specific type.
// If the type is already provider-specific or not found in mappings, returns as-is.
func ResolveInstanceType(abstractType, providerName string) string {
	if mapping, ok := InstanceTypeMappings[abstractType]; ok {
		if concreteType, ok := mapping[providerName]; ok {
			return concreteType
		}
	}
	return abstractType
}

// IsAbstractType checks if a type name is an abstract type with mappings.
func IsAbstractType(typeName string) bool {
	_, ok := InstanceTypeMappings[typeName]
	return ok
}

// GetSupportedProviders returns which providers support an abstract type.
func GetSupportedProviders(abstractType string) []string {
	mapping, ok := InstanceTypeMappings[abstractType]
	if !ok {
		return nil
	}
	providers := make([]string, 0, len(mapping))
	for p := range mapping {
		providers = append(providers, p)
	}
	return providers
}

