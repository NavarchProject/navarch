package simulator

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestNodeStarter_EmptySpecs_NoPanic(t *testing.T) {
	// Test that empty specs don't cause division by zero panics
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	testCases := []struct {
		name    string
		pattern string
	}{
		{"linear", "linear"},
		{"instant", "instant"},
		{"exponential", "exponential"},
		{"wave", "wave"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := StartupConfig{
				Pattern:  tc.pattern,
				Duration: Duration(5 * time.Second),
			}

			starter := NewNodeStarter(config, "http://localhost:8080", 42, logger)

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			// Should not panic with empty specs
			nodes, err := starter.StartFleet(ctx, []NodeSpec{})
			if err != nil {
				t.Errorf("StartFleet with empty specs returned error: %v", err)
			}
			if len(nodes) != 0 {
				t.Errorf("StartFleet with empty specs returned %d nodes, want 0", len(nodes))
			}
		})
	}
}

func TestFleetGenerator_GenerateFleet(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	config := &FleetGeneratorConfig{
		TotalNodes: 10,
		Templates: []NodeTemplate{
			{
				Name:         "test-template",
				Weight:       100,
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
				InstanceType: "a3-highgpu-8g",
			},
		},
		Providers: map[string]int{"gcp": 100},
		Regions:   map[string]int{"us-central1": 100},
	}

	gen := NewFleetGenerator(config, 42, logger)
	specs := gen.GenerateFleet()

	if len(specs) != 10 {
		t.Errorf("GenerateFleet returned %d specs, want 10", len(specs))
	}

	// Verify all specs have expected values
	for i, spec := range specs {
		if spec.GPUCount != 8 {
			t.Errorf("spec[%d].GPUCount = %d, want 8", i, spec.GPUCount)
		}
		if spec.Provider != "gcp" {
			t.Errorf("spec[%d].Provider = %s, want gcp", i, spec.Provider)
		}
	}
}

func TestFleetGenerator_GenerateFleet_Zero(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	config := &FleetGeneratorConfig{
		TotalNodes: 0, // Zero nodes
		Templates: []NodeTemplate{
			{
				Name:         "test-template",
				Weight:       100,
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
				InstanceType: "a3-highgpu-8g",
			},
		},
		Providers: map[string]int{"gcp": 100},
		Regions:   map[string]int{"us-central1": 100},
	}

	gen := NewFleetGenerator(config, 42, logger)
	specs := gen.GenerateFleet()

	if len(specs) != 0 {
		t.Errorf("GenerateFleet with TotalNodes=0 returned %d specs, want 0", len(specs))
	}
}

func TestFleetGenerator_WeightedDistribution(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	config := &FleetGeneratorConfig{
		TotalNodes: 100,
		Templates: []NodeTemplate{
			{
				Name:         "h100",
				Weight:       70,
				GPUCount:     8,
				GPUType:      "NVIDIA H100",
				InstanceType: "a3-highgpu-8g",
			},
			{
				Name:         "a100",
				Weight:       30,
				GPUCount:     8,
				GPUType:      "NVIDIA A100",
				InstanceType: "a2-ultragpu-8g",
			},
		},
		Providers: map[string]int{
			"gcp": 60,
			"aws": 40,
		},
		Regions: map[string]int{
			"us-central1": 50,
			"us-east1":    50,
		},
	}

	gen := NewFleetGenerator(config, 42, logger)
	specs := gen.GenerateFleet()

	if len(specs) != 100 {
		t.Fatalf("GenerateFleet returned %d specs, want 100", len(specs))
	}

	// Count distributions
	gcpCount := 0
	awsCount := 0
	for _, spec := range specs {
		if spec.Provider == "gcp" {
			gcpCount++
		} else if spec.Provider == "aws" {
			awsCount++
		}
	}

	// With 60/40 weights and seed=42, we should see roughly 60% gcp
	// Allow some variance due to random selection
	if gcpCount < 40 || gcpCount > 80 {
		t.Errorf("GCP count = %d, expected roughly 60%% of 100", gcpCount)
	}
	if awsCount < 20 || awsCount > 60 {
		t.Errorf("AWS count = %d, expected roughly 40%% of 100", awsCount)
	}
}
