package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// JobConfig defines a task or job to run on GPU instances.
// Used by `navarch run` and `navarch dev` commands.
type JobConfig struct {
	// Name is an optional identifier for the job.
	Name string `yaml:"name,omitempty"`

	// Resources specifies GPU and compute requirements.
	Resources ResourcesCfg `yaml:"resources,omitempty"`

	// Run is the command or script to execute.
	// Can be a single command string or multi-line script.
	Run string `yaml:"run,omitempty"`

	// Setup contains commands to run before the main task.
	// Useful for installing dependencies, downloading data, etc.
	Setup string `yaml:"setup,omitempty"`

	// Envs are environment variables to set.
	Envs map[string]string `yaml:"envs,omitempty"`

	// WorkDir is the local directory to sync to the instance.
	// Defaults to current directory.
	WorkDir string `yaml:"workdir,omitempty"`

	// Image is the Docker image to use.
	// If not specified, runs on bare metal with system Python.
	Image string `yaml:"image,omitempty"`

	// Ports to expose (for dev mode).
	Ports []int `yaml:"ports,omitempty"`

	// Spot enables spot/preemptible instances for cost savings.
	Spot bool `yaml:"spot,omitempty"`

	// Pool specifies which pool to use (if multiple pools exist).
	Pool string `yaml:"pool,omitempty"`

	// Nodes specifies the number of nodes for distributed training.
	// Default is 1 (single node). When > 1, the job runs across multiple nodes
	// with distributed training environment variables set automatically.
	Nodes int `yaml:"nodes,omitempty"`
}

// IsMultiNode returns true if the job requires multiple nodes.
func (j *JobConfig) IsMultiNode() bool {
	return j.Nodes > 1
}

// GetNodeCount returns the number of nodes, defaulting to 1.
func (j *JobConfig) GetNodeCount() int {
	if j.Nodes <= 0 {
		return 1
	}
	return j.Nodes
}

// ResourcesCfg specifies compute resource requirements.
type ResourcesCfg struct {
	// GPU specifies GPU requirements.
	// Can be: "H100", "H100:8", "A100", "any", etc.
	GPU string `yaml:"gpu,omitempty"`

	// GPUCount specifies number of GPUs (alternative to GPU suffix).
	GPUCount int `yaml:"gpu_count,omitempty"`

	// Memory specifies minimum memory (e.g., "64GB").
	Memory string `yaml:"memory,omitempty"`

	// CPUs specifies minimum vCPUs.
	CPUs int `yaml:"cpus,omitempty"`

	// Cloud restricts to specific cloud provider(s).
	// Can be: "aws", "gcp", "lambda", or comma-separated list.
	Cloud string `yaml:"cloud,omitempty"`

	// Region restricts to specific region(s).
	Region string `yaml:"region,omitempty"`

	// Zone restricts to specific zone(s).
	Zone string `yaml:"zone,omitempty"`
}

// LoadJob reads a job configuration from a YAML file.
func LoadJob(path string) (*JobConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading job config: %w", err)
	}

	var job JobConfig
	if err := yaml.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("parsing job config: %w", err)
	}

	if err := job.Validate(); err != nil {
		return nil, fmt.Errorf("invalid job config: %w", err)
	}

	job.applyDefaults()
	return &job, nil
}

// LoadJobFromString parses a job configuration from YAML string.
func LoadJobFromString(data string) (*JobConfig, error) {
	var job JobConfig
	if err := yaml.Unmarshal([]byte(data), &job); err != nil {
		return nil, fmt.Errorf("parsing job config: %w", err)
	}

	if err := job.Validate(); err != nil {
		return nil, fmt.Errorf("invalid job config: %w", err)
	}

	job.applyDefaults()
	return &job, nil
}

// Validate checks the job configuration for errors.
func (j *JobConfig) Validate() error {
	// At minimum, a job needs either a run command or to be a dev session
	// (dev sessions don't require a run command since they're interactive)
	// This validation is minimal - more validation happens at runtime

	if j.Resources.GPU != "" {
		if err := validateGPUSpec(j.Resources.GPU); err != nil {
			return err
		}
	}

	if j.Resources.Memory != "" {
		if err := validateMemorySpec(j.Resources.Memory); err != nil {
			return err
		}
	}

	return nil
}

func (j *JobConfig) applyDefaults() {
	if j.WorkDir == "" {
		j.WorkDir = "."
	}
}

// ParseGPU parses a GPU specification string and returns GPU type and count.
// Examples: "H100" -> ("H100", 1), "H100:8" -> ("H100", 8), "any:4" -> ("any", 4)
func (r *ResourcesCfg) ParseGPU() (gpuType string, count int) {
	if r.GPU == "" {
		return "", r.GPUCount
	}

	parts := strings.SplitN(r.GPU, ":", 2)
	gpuType = parts[0]
	count = 1

	if len(parts) == 2 {
		fmt.Sscanf(parts[1], "%d", &count)
	}

	// GPUCount takes precedence if specified
	if r.GPUCount > 0 {
		count = r.GPUCount
	}

	return gpuType, count
}

// ParseMemoryGB parses memory specification and returns gigabytes.
// Examples: "64GB" -> 64, "128G" -> 128, "256" -> 256
func (r *ResourcesCfg) ParseMemoryGB() int {
	if r.Memory == "" {
		return 0
	}

	mem := strings.TrimSpace(strings.ToUpper(r.Memory))
	mem = strings.TrimSuffix(mem, "GB")
	mem = strings.TrimSuffix(mem, "G")

	var gb int
	fmt.Sscanf(mem, "%d", &gb)
	return gb
}

// ParseClouds returns the list of allowed cloud providers.
func (r *ResourcesCfg) ParseClouds() []string {
	if r.Cloud == "" {
		return nil
	}

	clouds := strings.Split(r.Cloud, ",")
	for i := range clouds {
		clouds[i] = strings.TrimSpace(strings.ToLower(clouds[i]))
	}
	return clouds
}

// ParseRegions returns the list of allowed regions.
func (r *ResourcesCfg) ParseRegions() []string {
	if r.Region == "" {
		return nil
	}

	regions := strings.Split(r.Region, ",")
	for i := range regions {
		regions[i] = strings.TrimSpace(regions[i])
	}
	return regions
}

func validateGPUSpec(spec string) error {
	parts := strings.SplitN(spec, ":", 2)
	gpuType := strings.ToUpper(parts[0])

	// Valid GPU types (common ones)
	validTypes := map[string]bool{
		"ANY":   true,
		"H100":  true,
		"A100":  true,
		"A10G":  true,
		"L4":    true,
		"L40S":  true,
		"V100":  true,
		"T4":    true,
		"A6000": true,
		"RTX":   true, // Allow RTX* pattern
	}

	// Check if type is valid or starts with a valid prefix
	if !validTypes[gpuType] && !strings.HasPrefix(gpuType, "RTX") {
		// Allow unknown types with a warning (flexibility for new GPUs)
	}

	if len(parts) == 2 {
		var count int
		if _, err := fmt.Sscanf(parts[1], "%d", &count); err != nil || count <= 0 {
			return fmt.Errorf("invalid GPU count in %q: must be positive integer", spec)
		}
	}

	return nil
}

func validateMemorySpec(spec string) error {
	mem := strings.TrimSpace(strings.ToUpper(spec))
	mem = strings.TrimSuffix(mem, "GB")
	mem = strings.TrimSuffix(mem, "G")

	var gb int
	if _, err := fmt.Sscanf(mem, "%d", &gb); err != nil || gb <= 0 {
		return fmt.Errorf("invalid memory specification %q: must be positive integer with optional GB suffix", spec)
	}

	return nil
}

// FindJobConfig looks for a job configuration file in standard locations.
// Checks: navarch.yaml, navarch.yml, .navarch.yaml, .navarch.yml
func FindJobConfig() (string, error) {
	candidates := []string{
		"navarch.yaml",
		"navarch.yml",
		".navarch.yaml",
		".navarch.yml",
	}

	for _, name := range candidates {
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
	}

	return "", fmt.Errorf("no job config found (tried: %s)", strings.Join(candidates, ", "))
}

// ResolveWorkDir resolves the workdir path relative to the config file.
func (j *JobConfig) ResolveWorkDir(configPath string) (string, error) {
	if filepath.IsAbs(j.WorkDir) {
		return j.WorkDir, nil
	}

	// If config path is provided, resolve relative to it
	if configPath != "" {
		configDir := filepath.Dir(configPath)
		return filepath.Join(configDir, j.WorkDir), nil
	}

	// Otherwise resolve relative to cwd
	return filepath.Abs(j.WorkDir)
}
