# Contributing to Navarch

This guide covers how to contribute to Navarch, from setting up your development environment to submitting pull requests.

## Development setup

### Prerequisites

- Go 1.21 or later
- Make
- Docker (for integration tests)
- Access to a GPU machine (optional, for hardware testing)

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
# Unit tests
make test

# Unit tests with race detection
make test-race

# Integration tests (requires Docker)
make test-integration
```

### Docker provider for SSH testing

The `docker` provider spawns real containers with SSH servers, enabling end-to-end testing of the bootstrap flow without cloud infrastructure.

```go
import "github.com/NavarchProject/navarch/pkg/provider/docker"

// Create provider
provider, err := docker.New(docker.Config{
    SSHPublicKeyPath: "~/.ssh/id_rsa.pub",
    Logger:           logger,
})
if err != nil {
    log.Fatal(err)
}

// Ensure the SSH image is available
if err := provider.EnsureImage(ctx); err != nil {
    log.Fatal(err)
}

// Provision a container
node, err := provider.Provision(ctx, provider.ProvisionRequest{
    Labels: map[string]string{"pool": "test"},
})
// node.IPAddress = "127.0.0.1"
// node.SSHPort = <dynamically assigned port>

// Clean up
defer provider.TerminateAll()
```

The Docker provider:

- Uses `linuxserver/openssh-server` image
- Maps container port 22 to a random host port
- Sets `SSHPort` on the returned node for bootstrap to use
- Supports `TerminateAll()` for cleanup in tests

## Code structure

```
navarch/
├── cmd/                    # Entry points
│   ├── control-plane/      # Control plane main
│   ├── node/               # Node agent main
│   ├── navarch/            # CLI main
│   └── simulator/          # Simulator main
├── pkg/
│   ├── controlplane/       # Control plane logic
│   │   ├── pool/           # Pool management
│   │   ├── health/         # Health evaluation
│   │   ├── autoscaler/     # Autoscaling strategies
│   │   └── provider/       # Cloud provider interfaces
│   │       └── docker/     # Docker provider for testing
│   ├── node/               # Node agent logic
│   │   ├── gpu/            # GPU monitoring (NVML)
│   │   └── metrics/        # Metrics collection
│   ├── api/                # gRPC service definitions
│   └── config/             # Configuration parsing
├── proto/                  # Protocol buffer definitions
├── scenarios/              # Simulator test scenarios
└── docs/                   # Documentation source
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
- Join the community chat (coming soon)

## Code of conduct

Be respectful and constructive. We're all here to build something useful.
