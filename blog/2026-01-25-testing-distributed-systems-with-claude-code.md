# Testing a 5,000-Node Distributed System Without Spending $50K on Cloud Compute

*How we used Claude Code to build simulators, chaos testing, and load testing for a production GPU fleet manager*

---

Running a distributed GPU fleet across multiple cloud providers means accepting that something will fail constantly. At scale, "rare" events happen every few minutes. A GPU throws an XID error. A node loses network connectivity. An entire availability zone goes down.

We needed to test our fleet management system, Navarch, against these realities. But running 5,000 GPU nodes across GCP, AWS, and Lambda Labs for even an hour would cost tens of thousands of dollars. Doing it repeatedly during development wasn't feasible.

So we built a simulation and chaos testing framework that lets us stress-test the entire system on a laptop. This post explains how we used Claude Code throughout that process—what worked well, what required iteration, and what we learned about human-AI collaboration on complex systems work.

## The Problem: You Can't Test Distributed Systems in Production

Navarch manages GPU fleets for ML training workloads. Its core job is to:

1. Provision nodes across multiple cloud providers
2. Monitor GPU health via agents running on each node
3. Detect failures (hardware faults, thermal issues, connectivity loss)
4. Automatically replace unhealthy nodes
5. Scale the fleet based on demand

Testing this properly requires simulating thousands of nodes, realistic failure patterns, and the timing-dependent behaviors that only emerge at scale. Unit tests catch logic bugs but miss the emergent complexity of distributed systems.

We needed three things:

- **A fleet simulator** that could run thousands of virtual nodes in-process
- **A chaos engine** that could inject realistic failures
- **Metrics and reporting** to understand system behavior under stress

## Starting Point: Describing the Architecture to Claude

Our first conversation with Claude Code wasn't about writing code—it was about articulating what we needed. We described the control plane architecture, the node agent design, and the failure modes we cared about.

Claude helped us think through the simulator design. The key insight from that conversation: rather than mocking individual components, we should run the real control plane and simulate only the node agents. This approach tests the actual production code paths while avoiding cloud API calls.

```
Control Plane (real)  <--->  Simulated Nodes (virtual)
        |                            |
   Real logic                   Fake GPUs
   Real state                   Injected failures
   Real decisions               Simulated metrics
```

This architecture meant our stress tests exercise the same code that runs in production. The simulator creates virtual nodes that register with the control plane, send heartbeats, and respond to commands—just like real nodes.

## Building the Simulator: Iterative Collaboration

We built the simulator incrementally over several sessions with Claude Code. Here's how that collaboration typically worked.

### Scenario Definition System

We started by describing what we wanted: YAML-based scenario files that define fleets, timing, and expected behaviors. Claude generated the initial schema and parser:

```yaml
# Example scenario structure
name: "basic-fleet-test"
description: "Verify node registration and health reporting"

nodes:
  - id: node-1
    gpus: 8
    provider: gcp
    region: us-central1

events:
  - time: 5s
    type: inject_failure
    target: node-1
    failure_type: xid_error
    xid_code: 74  # NVLink error
```

The first version worked but felt clunky for large-scale tests. We didn't want to define 5,000 individual nodes. This led to the fleet generator.

### Fleet Generator

We asked Claude to help design a system for generating large fleets programmatically. After discussing requirements, we landed on a template-based approach:

```yaml
fleet:
  total_nodes: 5000
  templates:
    - name: h100-8gpu
      weight: 50
      gpus: 8
      instance_type: a3-highgpu-8g
    - name: a100-8gpu
      weight: 30
      gpus: 8
      instance_type: a2-highgpu-8g

  provider_distribution:
    gcp: 45
    aws: 40
    lambda: 15

  startup_pattern:
    type: exponential  # Ramp up gradually
    duration: 2m
    jitter_percent: 15
```

Claude generated the generator code, but we iterated several times on the startup patterns. The exponential pattern—starting 1 node, then 2, then 4, then 8—mimics how real deployments scale up. We added jitter to prevent artificial synchronization.

### What Required Human Judgment

Claude is excellent at implementing well-specified requirements. The harder part is deciding *what* to build. Throughout the simulator work, we made judgment calls that Claude couldn't make for us:

- **Which startup patterns matter?** We chose exponential because it matches real autoscaler behavior, not because Claude suggested it.
- **How realistic should failures be?** We decided to use actual XID error codes from NVIDIA documentation rather than generic "failure" events.
- **What's the right abstraction level?** We chose to simulate at the node level rather than the GPU level, trading some fidelity for performance.

Claude helped us implement these decisions quickly, but the decisions themselves required understanding our production environment and use cases.

## The Chaos Engine: Where Things Got Interesting

The chaos engine is the heart of our testing framework. It injects failures into the simulated fleet to verify the system handles them correctly.

### Failure Types

We started with a simple list of failure types:

- **XID errors**: GPU hardware faults with specific codes (some fatal, some recoverable)
- **Temperature failures**: Thermal throttling or shutdown
- **NVML failures**: GPU driver communication issues
- **Boot failures**: Nodes that fail to initialize
- **Network failures**: Loss of connectivity to control plane

Claude generated the initial implementation for each type. The XID error handling required the most iteration because different codes have different severity levels:

```go
// Fatal XID codes require immediate node replacement
var fatalXIDCodes = map[int]string{
    43: "GPU has fallen off the bus",
    48: "Double Bit ECC Error",
    63: "ECC page retirement or row remapping failure",
    74: "NVLink Error",
    79: "GPU has fallen off the bus",
    95: "GPU firmware error",
}

// Recoverable XID codes may resolve without intervention
var recoverableXIDCodes = map[int]string{
    13: "Graphics Engine Exception",
    31: "GPU memory page fault",
    // ... additional codes
}
```

We populated these tables from NVIDIA's documentation and our own production experience. Claude didn't know which codes mattered most—we did.

### Cascading Failures

Real infrastructure failures often cascade. A power issue in one rack affects multiple nodes. A network partition isolates an entire zone. We asked Claude to help design a cascading failure system.

The implementation tracks failure relationships:

```yaml
chaos:
  cascading:
    enabled: true
    probability: 0.15  # 15% of failures trigger cascades
    max_depth: 3       # Maximum cascade chain length
    scope: same_zone   # Cascades affect nearby nodes
    max_affected_percent: 10  # Limit blast radius
    delay:
      min: 100ms
      max: 2s
```

This was one of the more complex pieces to get right. The first implementation cascaded too aggressively—a single failure could take down half the fleet. We added the `max_affected_percent` parameter after seeing runaway cascades in testing.

Claude's implementation of the scoping logic (same_rack, same_zone, same_region, same_provider) was clean on the first try. The configuration parameters required tuning based on our tests.

### Recovery Modeling

Failures aren't permanent. Non-fatal XID errors often resolve. Temperature issues clear after cooling. We added automatic recovery with configurable timing:

```yaml
chaos:
  recovery:
    enabled: true
    probability: 0.70  # 70% of non-fatal failures recover
    mean_time_seconds: 300  # 5 minutes average
    std_dev_seconds: 120    # With variance
```

The normal distribution around mean recovery time produces realistic MTTR (Mean Time To Recovery) curves. Claude implemented the statistics correctly; we chose the parameters based on our production observations.

## Metrics and Reporting: Seeing What Happened

Running a 5,000-node chaos test generates enormous amounts of data. We needed reporting that helped us understand system behavior.

### Metrics Collection

The simulator tracks:

- Node state transitions (healthy → unhealthy → recovered)
- Failure counts by type and XID code
- Cascading failure chains
- Recovery statistics
- Timeline samples at configurable intervals

Claude generated the metrics collection code, including proper locking for concurrent access from simulated nodes. The trickiest part was the timeline sampling—we wanted fine-grained data without memory exhaustion.

### HTML Reports

We asked Claude to generate interactive HTML reports with charts. This was a case where Claude's ability to produce complete, working code in one pass saved significant time.

The report includes:

- Summary statistics (peak healthy nodes, total failures, recovery rate)
- Timeline charts showing fleet health over time
- Failure breakdown by type and XID code
- Full configuration capture for reproducibility

```
┌─────────────────────────────────────────────────┐
│  Stress Test Results                            │
├─────────────────────────────────────────────────┤
│  Duration: 30 minutes                           │
│  Nodes Started: 5,000                           │
│  Peak Healthy: 4,847                            │
│  Min Healthy: 4,612                             │
│  Total Failures: 1,523                          │
│  Cascading Failures: 287                        │
│  Recoveries: 1,089                              │
└─────────────────────────────────────────────────┘
```

The chart rendering uses Chart.js. Claude produced working chart code that we only needed to adjust for styling preferences.

## Stress Test Results: What We Learned

Running the simulator revealed several issues we wouldn't have found otherwise.

### Finding 1: Pool Manager Timing

Under high failure rates, the pool manager's 30-second evaluation interval was too slow. Failures accumulated faster than the system replaced nodes. We discovered this in the 5,000-node extreme test with 50 failures per minute per 1,000 nodes.

The fix was straightforward once we understood the problem: add an emergency evaluation trigger when failure count exceeds a threshold.

### Finding 2: Cascading Failure Isolation

Our initial NVLink error handling didn't account for the fact that NVLink connects multiple GPUs in a node. An NVLink failure often indicates all eight GPUs are affected, not just one. The simulator helped us see this pattern in aggregate.

### Finding 3: Recovery Timing Matters

With automatic recovery enabled, we saw oscillation: nodes would fail, trigger replacement provisioning, then recover before the replacement arrived. This wasted cloud spend on unnecessary nodes.

The solution was to add a "grace period" before initiating replacement—giving non-fatal failures time to recover.

## What Worked Well in the Claude Code Collaboration

**Rapid implementation of well-specified features.** Once we knew what we wanted, Claude could produce working code quickly. The HTML report generator, for example, went from concept to working implementation in a single session.

**Iterating on complex logic.** The cascading failure system required several rounds of refinement. We could describe problems ("cascades are too aggressive") and get targeted fixes.

**Boilerplate and plumbing.** YAML parsing, metrics collection, file I/O—Claude handled these efficiently, letting us focus on the interesting parts.

**Cross-cutting changes.** When we added the recovery system, it touched the chaos engine, metrics collection, and reporting. Claude tracked all the integration points.

## What Required Human Direction

**Architecture decisions.** Whether to simulate at the node or GPU level, which failure types to model, how to structure the scenario files—these required understanding our production environment.

**Parameter tuning.** Failure rates, cascade probabilities, recovery timing—these came from our operational experience, not from Claude.

**Prioritization.** We had many ideas for features. Deciding what to build first required understanding which tests would catch the most bugs.

**Validation.** Claude can produce plausible code, but verifying it behaves correctly under edge cases required careful testing and review.

## Reproducibility: The Underrated Feature

One feature we're particularly glad we built: deterministic test execution via seeded randomness.

```bash
# Run the same test with the same seed
./bin/simulator run scenarios/stress/5000-node-extreme.yaml --seed 12345
```

With the same seed, the simulator produces identical failure sequences. This makes debugging much easier—when we find a bug, we can reproduce it exactly.

Claude implemented the seeded random number generator correctly, but we had to be careful to pass the seed through all the places that make random choices (failure selection, timing jitter, recovery decisions).

## Try It Yourself

The simulator is part of the Navarch codebase. To run a stress test:

```bash
# Build the simulator
make build

# Run the 1,000-node chaos test
./bin/simulator run scenarios/stress/1000-node-chaos.yaml -v

# Run with a specific seed for reproducibility
./bin/simulator run scenarios/stress/1000-node-chaos.yaml --seed 42 -v
```

The HTML report appears in the `reports/` directory.

## Conclusion

Building the simulation and chaos testing framework took roughly two weeks of active development. Without Claude Code, we estimate it would have taken six to eight weeks—the implementation work alone would have been three to four times longer, and we would have spent more time debugging boilerplate issues.

More importantly, the framework paid for itself quickly. We found three significant bugs in the first round of stress testing that would have been difficult to catch in production. One of them—the cascading failure isolation issue—could have caused real problems during an actual zone failure.

If you're building distributed systems, invest in simulation early. Test at the scale you expect to run. And if you have access to AI coding assistants, use them for the implementation work so you can focus on the harder problems: deciding what to build and validating that it works.

---

*This is part of a series on building production distributed systems with AI assistance. Next post: Multi-cloud provider abstraction and the challenges of heterogeneous GPU fleets.*
