# Contributing to Navarch

This guide covers how to contribute to Navarch, from setting up your development environment to submitting pull requests.

## Development setup

### Prerequisites

- Go 1.21 or later
- Make
- Docker (optional, for Docker provider tests)
- GPU machine (optional, for hardware testing)

### Clone and build

```bash
git clone https://github.com/NavarchProject/navarch.git
cd navarch
make build
```

This produces binaries in `bin/`:
- `control-plane` - the control plane server
- `node` - the node agent
- `navarch` - the CLI
- `simulator` - the fleet simulator

### Run tests

```bash
make test          # Run all tests
make test-race     # With race detection
make test-all      # Format, lint, and test
```

See [Testing](testing.md) for the Docker provider and simulator.

## Code structure

```
navarch/
├── cmd/                    # Entry points
│   ├── control-plane/      # Control plane server
│   ├── node/               # Node agent
│   ├── navarch/            # CLI
│   └── simulator/          # Fleet simulator
├── pkg/
│   ├── bootstrap/          # SSH bootstrap for setup commands
│   ├── config/             # Configuration parsing
│   ├── controlplane/       # Control plane logic
│   ├── health/             # Health policy evaluation
│   ├── node/               # Node agent logic
│   ├── pool/               # Pool management and autoscaling
│   └── provider/           # Cloud provider interfaces
│       ├── docker/         # Docker provider (testing)
│       ├── fake/           # Fake provider (testing)
│       ├── gcp/            # Google Cloud
│       └── lambda/         # Lambda Labs
├── proto/                  # Protocol buffer definitions
└── scenarios/              # Simulator test scenarios
```

## Making changes

### Branch naming

Use descriptive branch names:
- `feature/add-azure-provider`
- `fix/health-check-timeout`
- `docs/improve-deployment-guide`

### Commit messages

Write clear commit messages that explain what and why:

```
Add cooldown period to autoscaler

The autoscaler was scaling up and down too frequently when utilization
hovered near the threshold. This adds a configurable cooldown period
that prevents scaling actions within a specified duration of each other.

Fixes #123
```

### Testing requirements

All changes must include tests:
- Unit tests for new functions
- Integration tests for new features
- Simulator scenarios for behavior changes

Run the full test suite before submitting:

```bash
make test-all
```

## Pull request process

1. **Fork and branch**: Create a feature branch from `main`
2. **Make changes**: Write code and tests
3. **Test locally**: Run `make test-all`
4. **Push and open PR**: Include a clear description of the change
5. **Address feedback**: Respond to code review comments
6. **Merge**: A maintainer will merge once approved

### PR description template

```markdown
## What

Brief description of the change.

## Why

Why is this change needed? Link to issue if applicable.

## How

How does this change work? Any design decisions worth noting?

## Testing

How was this tested? Include simulator scenarios if applicable.
```

## Adding a new cloud provider

See [Extending Navarch](extending.md#custom-providers) for the provider interface and implementation guide.

## Adding a new autoscaler

See [Extending Navarch](extending.md#custom-autoscalers) for the autoscaler interface and implementation guide.

## Documentation

Documentation lives in the `website/` directory and uses MkDocs Material.

To preview documentation changes:

```bash
cd website
pip install mkdocs-material
mkdocs serve
```

## Getting help

- Open an issue for bugs or feature requests
- Start a discussion for questions or ideas

## Code of conduct

Be respectful and constructive. We're all here to build something useful.
