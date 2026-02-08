# Simulator

The Navarch fleet simulator creates a simulated GPU fleet and control plane for testing, development, and demonstration purposes.

## Overview

The simulator runs an embedded control plane and spawns simulated nodes that behave like real GPU instances. You can inject failures, issue commands, and observe how the system responds without provisioning actual cloud resources.

Use the simulator to:

- Test health check logic and failure detection
- Verify command flows (cordon, drain, terminate)
- Develop and debug new features locally
- Run automated integration tests
- Demo Navarch to others

<div class="grid cards" markdown>

-   **Scenarios**

    ---

    Define fleets and events in YAML. Inject failures, issue commands, verify behavior.

    [:octicons-arrow-right-24: Scenario reference](scenarios.md)

-   **Stress Testing**

    ---

    Simulate 1000+ nodes with realistic failure patterns, cascading failures, and auto-recovery.

    [:octicons-arrow-right-24: Stress testing guide](stress-testing.md)

</div>

## Building

```bash
make build
```

This creates `bin/simulator` along with the other Navarch binaries.

## Running scenarios

Scenarios are YAML files that define a fleet configuration and a sequence of events.

```bash
# Run a scenario
./bin/simulator run scenarios/gpu-failure.yaml -v

# Validate without running
./bin/simulator validate scenarios/gpu-failure.yaml
```

### Command-line options

| Flag | Description |
|------|-------------|
| `-v, --verbose` | Enable verbose output (INFO level) |
| `--debug` | Enable debug output (DEBUG level) |
| `--seed` | Random seed for reproducible stress tests |

## Interactive mode

Interactive mode starts a control plane and a default two-node fleet, then waits for you to interact with it using the Navarch CLI.

```bash
./bin/simulator interactive -v
```

In another terminal:

```bash
# List all nodes
navarch list -s http://localhost:8080

# Get details about a node
navarch get node-1 -s http://localhost:8080

# Cordon a node
navarch cordon node-1 -s http://localhost:8080
```

Press `Ctrl+C` to stop.

## Makefile targets

```bash
# Run interactive mode
make sim

# Run a specific scenario
make sim-run SCENARIO=scenarios/gpu-failure.yaml

# Validate a scenario
make sim-validate SCENARIO=scenarios/basic-fleet.yaml

# Run stress tests
make sim-run SCENARIO=scenarios/stress/1000-node-chaos.yaml
```

## Next steps

- [Scenario reference](scenarios.md) — Learn the scenario file format and available actions
- [Stress testing](stress-testing.md) — Run large-scale chaos tests
