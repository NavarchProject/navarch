package simulator

import (
	"math/rand"
	"testing"
)

func TestSelectXIDCode_Reproducibility(t *testing.T) {
	config := &ChaosConfig{
		XIDDistribution: map[int]int{
			79: 30,
			48: 20,
			31: 25,
			13: 15,
			74: 10,
		},
	}

	// Run multiple times with the same seed and verify same sequence
	const seed = 12345
	const iterations = 100

	var firstRun []int
	for run := 0; run < 3; run++ {
		engine := &ChaosEngine{
			config: config,
			rng:    rand.New(rand.NewSource(seed)),
		}

		var results []int
		for i := 0; i < iterations; i++ {
			results = append(results, engine.selectXIDCode())
		}

		if run == 0 {
			firstRun = results
		} else {
			for i := range results {
				if results[i] != firstRun[i] {
					t.Errorf("run %d: result[%d] = %d, want %d (same as first run)", run, i, results[i], firstRun[i])
				}
			}
		}
	}
}

func TestSelectXIDCode_DefaultDistribution(t *testing.T) {
	engine := &ChaosEngine{
		config: &ChaosConfig{},
		rng:    rand.New(rand.NewSource(42)),
	}

	// With empty distribution, should pick from default codes
	defaultCodes := map[int]bool{
		13: true, 31: true, 32: true, 43: true, 45: true,
		48: true, 63: true, 64: true, 74: true, 79: true,
		92: true, 94: true, 95: true,
	}

	for i := 0; i < 50; i++ {
		code := engine.selectXIDCode()
		if !defaultCodes[code] {
			t.Errorf("selectXIDCode() returned %d, which is not in default codes", code)
		}
	}
}

func TestSelectXIDCode_WeightedDistribution(t *testing.T) {
	config := &ChaosConfig{
		XIDDistribution: map[int]int{
			79: 100, // Only code with weight
		},
	}

	engine := &ChaosEngine{
		config: config,
		rng:    rand.New(rand.NewSource(42)),
	}

	// Should always return 79 since it's the only weighted code
	for i := 0; i < 20; i++ {
		code := engine.selectXIDCode()
		if code != 79 {
			t.Errorf("selectXIDCode() = %d, want 79 (only weighted code)", code)
		}
	}
}

func TestSelectXIDCode_ZeroTotalWeight(t *testing.T) {
	config := &ChaosConfig{
		XIDDistribution: map[int]int{
			79: 0,
			48: 0,
		},
	}

	engine := &ChaosEngine{
		config: config,
		rng:    rand.New(rand.NewSource(42)),
	}

	// With zero total weight, should return default (79)
	code := engine.selectXIDCode()
	if code != 79 {
		t.Errorf("selectXIDCode() with zero weights = %d, want 79", code)
	}
}

