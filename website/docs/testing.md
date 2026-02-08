# Testing

## Unit tests

Run all unit tests:

```bash
make test
```

Run with race detection:

```bash
make test-race
```

Run all checks (format, lint, test):

```bash
make test-all
```

## Docker provider

The `docker` provider spawns SSH-enabled containers for end-to-end bootstrap testing without cloud infrastructure.

```go
package main

import (
    "context"
    "log"

    "github.com/NavarchProject/navarch/pkg/provider"
    "github.com/NavarchProject/navarch/pkg/provider/docker"
)

func main() {
    ctx := context.Background()

    p, err := docker.New(docker.Config{
        SSHPublicKeyPath: "~/.ssh/id_rsa.pub",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer p.TerminateAll()

    if err := p.EnsureImage(ctx); err != nil {
        log.Fatal(err)
    }

    node, err := p.Provision(ctx, provider.ProvisionRequest{
        Labels: map[string]string{"pool": "test"},
    })
    if err != nil {
        log.Fatal(err)
    }

    // node.IPAddress is "127.0.0.1"
    // node.SSHPort is the dynamically assigned host port
    log.Printf("Provisioned %s on port %d", node.ID, node.SSHPort)
}
```

The provider:

- Uses the `linuxserver/openssh-server` image
- Maps container port 22 to a random host port
- Sets `SSHPort` on the returned node for bootstrap

## Simulator

The simulator runs a virtual GPU fleet for scenario testing without cloud resources.

```bash
./bin/simulator run scenarios/gpu-failure.yaml -v
```

Interactive mode starts a control plane you can query with the CLI:

```bash
./bin/simulator interactive -v
```

See [Scenarios](simulator/scenarios.md) for the scenario file format and [Stress Testing](simulator/stress-testing.md) for chaos testing.

## Hardware testing

To test on real GPU machines, see [Testing on Hardware](testing-on-gpu.md).
