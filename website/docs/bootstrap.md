# Node Bootstrap

Navarch can run setup commands on newly provisioned instances via SSH. This is useful for installing the node agent, configuring GPU drivers, or running custom initialization scripts.

## Configuration

```yaml
pools:
  training:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    min_nodes: 2
    max_nodes: 20
    ssh_user: ubuntu
    ssh_private_key_path: ~/.ssh/navarch-key
    setup_commands:
      - |
        curl -L https://github.com/NavarchProject/navarch/releases/latest/download/navarch-node-linux-amd64 \
          -o /usr/local/bin/navarch-node && chmod +x /usr/local/bin/navarch-node
      - |
        navarch-node --server {{.ControlPlane}} --node-id {{.NodeID}} &
```

## Bootstrap fields

| Field | Required | Description |
|-------|----------|-------------|
| `setup_commands` | No | List of shell commands to run on the node after provisioning |
| `ssh_user` | No | SSH username (default: `ubuntu`) |
| `ssh_private_key_path` | Yes* | Path to SSH private key file |
| `ip_wait_timeout` | No | Max time to wait for instance IP (default: `15m`) |
| `ssh_timeout` | No | Max time to wait for SSH to become available (default: `10m`) |
| `ssh_connect_timeout` | No | Timeout for each SSH connection attempt (default: `30s`) |
| `command_timeout` | No | Max time for each command to execute (default: `5m`) |

*Required when `setup_commands` is specified.

These fields can also be set in `defaults` to apply to all pools:

```yaml
defaults:
  ssh_user: ubuntu
  ssh_private_key_path: ~/.ssh/navarch-key
```

## Template variables

Setup commands support Go template syntax. The following variables are available:

| Variable | Description | Example |
|----------|-------------|---------|
| `{{.ControlPlane}}` | Control plane URL | `http://control-plane.example.com:50051` |
| `{{.Pool}}` | Pool name | `training` |
| `{{.NodeID}}` | Unique node identifier | `node-abc123` |
| `{{.Provider}}` | Provider name | `lambda` |
| `{{.Region}}` | Region where node is provisioned | `us-west-2` |
| `{{.InstanceType}}` | Instance type | `gpu_8x_h100_sxm5` |

## How it works

1. When Navarch provisions a new instance, it waits for the instance to receive an IP address.
2. Once the IP is available, it waits for SSH to become available.
3. Once connected, it runs each setup command in order, enforcing the command timeout.
4. If any command fails or times out, the bootstrap is aborted and the node is marked as failed.
5. On success, the node is ready to receive workloads.

Commands that exceed `command_timeout` receive a `SIGKILL` signal on the remote host.

## Timeouts

Configure timeouts based on your infrastructure:

```yaml
pools:
  training:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    setup_commands:
      - ./long-running-setup.sh

    # Fast-booting instances with long setup scripts
    ip_wait_timeout: 5m
    ssh_timeout: 3m
    command_timeout: 30m
```

| Timeout | Default | Use case |
|---------|---------|----------|
| `ip_wait_timeout` | 15m | Increase for slow cloud providers or complex networking |
| `ssh_timeout` | 10m | Decrease for pre-configured images with fast boot |
| `ssh_connect_timeout` | 30s | Increase for high-latency networks |
| `command_timeout` | 5m | Increase for large downloads or compilations |

The control plane logs detailed information about each bootstrap phase:

- SSH connection attempts and timing
- Each command executed with duration
- stdout/stderr output on failure
- Total bootstrap duration

## Example: Full node setup

```yaml
defaults:
  ssh_user: ubuntu
  ssh_private_key_path: ~/.ssh/navarch-key

pools:
  training:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    min_nodes: 2
    max_nodes: 20
    setup_commands:
      # Install NVIDIA drivers if not present
      - |
        if ! command -v nvidia-smi &> /dev/null; then
          apt-get update && apt-get install -y nvidia-driver-535
        fi
      # Download and install the node agent
      - |
        curl -L https://github.com/NavarchProject/navarch/releases/latest/download/navarch-node-linux-amd64 \
          -o /usr/local/bin/navarch-node
        chmod +x /usr/local/bin/navarch-node
      # Create systemd service
      - |
        cat > /etc/systemd/system/navarch-node.service << EOF
        [Unit]
        Description=Navarch Node Agent
        After=network.target

        [Service]
        ExecStart=/usr/local/bin/navarch-node --server {{.ControlPlane}} --node-id {{.NodeID}} --pool {{.Pool}}
        Restart=always
        RestartSec=10

        [Install]
        WantedBy=multi-user.target
        EOF
      # Start the agent
      - systemctl daemon-reload && systemctl enable navarch-node && systemctl start navarch-node
```

## Comparison with other deployment methods

| Method | Use case |
|--------|----------|
| **SSH bootstrap** | Control plane manages agent installation. Good for managed fleets. |
| [Custom images](deployment.md#option-1-custom-machine-images-recommended) | Pre-bake agent into AMI/image. Fastest startup. |
| [Cloud-init](deployment.md#option-2-cloud-init-user-data) | Provider runs script at boot. No SSH needed. |
| [Container](deployment.md#option-3-container-deployment) | Run agent as Docker/K8s workload. |

See [Deployment](deployment.md) for details on each approach.
