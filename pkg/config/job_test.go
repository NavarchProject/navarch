package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadJobFromString(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		check   func(*testing.T, *JobConfig)
	}{
		{
			name: "minimal config",
			yaml: `
run: python train.py
`,
			wantErr: false,
			check: func(t *testing.T, j *JobConfig) {
				if j.Run != "python train.py" {
					t.Errorf("expected run='python train.py', got %q", j.Run)
				}
				if j.WorkDir != "." {
					t.Errorf("expected default workdir='.', got %q", j.WorkDir)
				}
			},
		},
		{
			name: "full config",
			yaml: `
name: training-job
resources:
  gpu: H100:8
  memory: 128GB
  cpus: 32
  cloud: aws,gcp
  region: us-east-1,us-west-2
run: |
  cd /workspace
  python train.py --epochs 100
setup: |
  pip install -r requirements.txt
envs:
  WANDB_API_KEY: secret
  CUDA_VISIBLE_DEVICES: "0,1,2,3"
workdir: ./src
image: pytorch/pytorch:2.0-cuda11.8
ports:
  - 8888
  - 6006
spot: true
pool: training
`,
			wantErr: false,
			check: func(t *testing.T, j *JobConfig) {
				if j.Name != "training-job" {
					t.Errorf("expected name='training-job', got %q", j.Name)
				}
				if j.Resources.GPU != "H100:8" {
					t.Errorf("expected gpu='H100:8', got %q", j.Resources.GPU)
				}
				if j.Resources.Memory != "128GB" {
					t.Errorf("expected memory='128GB', got %q", j.Resources.Memory)
				}
				if j.Resources.CPUs != 32 {
					t.Errorf("expected cpus=32, got %d", j.Resources.CPUs)
				}
				if !j.Spot {
					t.Error("expected spot=true")
				}
				if j.Pool != "training" {
					t.Errorf("expected pool='training', got %q", j.Pool)
				}
				if len(j.Ports) != 2 {
					t.Errorf("expected 2 ports, got %d", len(j.Ports))
				}
				if j.Image != "pytorch/pytorch:2.0-cuda11.8" {
					t.Errorf("expected image='pytorch/pytorch:2.0-cuda11.8', got %q", j.Image)
				}
			},
		},
		{
			name: "gpu count alternative",
			yaml: `
resources:
  gpu: A100
  gpu_count: 4
run: python train.py
`,
			wantErr: false,
			check: func(t *testing.T, j *JobConfig) {
				gpuType, count := j.Resources.ParseGPU()
				if gpuType != "A100" {
					t.Errorf("expected gpu type='A100', got %q", gpuType)
				}
				if count != 4 {
					t.Errorf("expected gpu count=4, got %d", count)
				}
			},
		},
		{
			name: "invalid gpu count",
			yaml: `
resources:
  gpu: H100:invalid
run: test
`,
			wantErr: true,
		},
		{
			name: "invalid memory",
			yaml: `
resources:
  memory: invalid
run: test
`,
			wantErr: true,
		},
		{
			name: "multi-node config",
			yaml: `
nodes: 4
resources:
  gpu: H100:8
run: torchrun --nproc_per_node=8 train.py
`,
			wantErr: false,
			check: func(t *testing.T, j *JobConfig) {
				if j.Nodes != 4 {
					t.Errorf("expected nodes=4, got %d", j.Nodes)
				}
				if !j.IsMultiNode() {
					t.Error("expected IsMultiNode()=true")
				}
				if j.GetNodeCount() != 4 {
					t.Errorf("expected GetNodeCount()=4, got %d", j.GetNodeCount())
				}
			},
		},
		{
			name: "single node default",
			yaml: `
run: python train.py
`,
			wantErr: false,
			check: func(t *testing.T, j *JobConfig) {
				if j.IsMultiNode() {
					t.Error("expected IsMultiNode()=false for single node")
				}
				if j.GetNodeCount() != 1 {
					t.Errorf("expected GetNodeCount()=1, got %d", j.GetNodeCount())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, err := LoadJobFromString(tt.yaml)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, job)
			}
		})
	}
}

func TestResourcesCfg_ParseGPU(t *testing.T) {
	tests := []struct {
		gpu       string
		gpuCount  int
		wantType  string
		wantCount int
	}{
		{"H100", 0, "H100", 1},
		{"H100:8", 0, "H100", 8},
		{"A100:4", 0, "A100", 4},
		{"any", 0, "any", 1},
		{"any:2", 0, "any", 2},
		{"", 4, "", 4},
		{"H100", 8, "H100", 8}, // GPUCount takes precedence
	}

	for _, tt := range tests {
		t.Run(tt.gpu, func(t *testing.T) {
			r := ResourcesCfg{GPU: tt.gpu, GPUCount: tt.gpuCount}
			gotType, gotCount := r.ParseGPU()
			if gotType != tt.wantType {
				t.Errorf("ParseGPU() type = %q, want %q", gotType, tt.wantType)
			}
			if gotCount != tt.wantCount {
				t.Errorf("ParseGPU() count = %d, want %d", gotCount, tt.wantCount)
			}
		})
	}
}

func TestResourcesCfg_ParseMemoryGB(t *testing.T) {
	tests := []struct {
		memory string
		want   int
	}{
		{"64GB", 64},
		{"128G", 128},
		{"256", 256},
		{"64gb", 64},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.memory, func(t *testing.T) {
			r := ResourcesCfg{Memory: tt.memory}
			if got := r.ParseMemoryGB(); got != tt.want {
				t.Errorf("ParseMemoryGB() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResourcesCfg_ParseClouds(t *testing.T) {
	tests := []struct {
		cloud string
		want  []string
	}{
		{"aws", []string{"aws"}},
		{"aws,gcp", []string{"aws", "gcp"}},
		{"AWS, GCP, Lambda", []string{"aws", "gcp", "lambda"}},
		{"", nil},
	}

	for _, tt := range tests {
		t.Run(tt.cloud, func(t *testing.T) {
			r := ResourcesCfg{Cloud: tt.cloud}
			got := r.ParseClouds()
			if len(got) != len(tt.want) {
				t.Errorf("ParseClouds() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseClouds()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestLoadJob(t *testing.T) {
	// Create a temporary job config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "navarch.yaml")

	content := `
name: test-job
resources:
  gpu: H100:4
run: python train.py
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	job, err := LoadJob(configPath)
	if err != nil {
		t.Fatalf("LoadJob() error: %v", err)
	}

	if job.Name != "test-job" {
		t.Errorf("expected name='test-job', got %q", job.Name)
	}
	if job.Resources.GPU != "H100:4" {
		t.Errorf("expected gpu='H100:4', got %q", job.Resources.GPU)
	}
}

func TestFindJobConfig(t *testing.T) {
	// Save current dir and change to temp
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// No config should fail
	_, err := FindJobConfig()
	if err == nil {
		t.Error("expected error when no config exists")
	}

	// Create navarch.yaml
	if err := os.WriteFile("navarch.yaml", []byte("run: test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	path, err := FindJobConfig()
	if err != nil {
		t.Errorf("FindJobConfig() error: %v", err)
	}
	if path != "navarch.yaml" {
		t.Errorf("FindJobConfig() = %q, want 'navarch.yaml'", path)
	}
}

func TestJobConfig_ResolveWorkDir(t *testing.T) {
	tests := []struct {
		name       string
		workDir    string
		configPath string
		wantSuffix string
	}{
		{
			name:       "relative to config",
			workDir:    "src",
			configPath: "/home/user/project/navarch.yaml",
			wantSuffix: "/home/user/project/src",
		},
		{
			name:       "absolute path unchanged",
			workDir:    "/absolute/path",
			configPath: "/home/user/navarch.yaml",
			wantSuffix: "/absolute/path",
		},
		{
			name:       "current dir",
			workDir:    ".",
			configPath: "/home/user/navarch.yaml",
			wantSuffix: "/home/user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &JobConfig{WorkDir: tt.workDir}
			got, err := j.ResolveWorkDir(tt.configPath)
			if err != nil {
				t.Fatalf("ResolveWorkDir() error: %v", err)
			}
			if got != tt.wantSuffix {
				t.Errorf("ResolveWorkDir() = %q, want %q", got, tt.wantSuffix)
			}
		})
	}
}
