# Testing on real GPUs

This guide describes how to test Navarch on real GPU instances.

## Prerequisites

Before testing on real GPUs, ensure:

- All unit tests pass locally: `go test ./... -short`
- The simulator scenarios pass: `./bin/simulator run scenarios/basic-fleet.yaml`
- You have access to a GPU cloud provider (Lambda Labs, GCP, AWS)

## Quick start

### 1. Provision a GPU instance

Lambda Labs:
```bash
# Use Lambda Labs console to create an instance
# Recommended: gpu_1x_a100_sxm4 or gpu_8x_a100_80gb_sxm4
```

GCP:
```bash
gcloud compute instances create navarch-test \
  --zone=us-central1-a \
  --machine-type=a2-highgpu-1g \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --accelerator=type=nvidia-tesla-a100,count=1 \
  --maintenance-policy=TERMINATE
```

AWS:
```bash
aws ec2 run-instances \
  --image-id ami-0abcdef1234567890 \
  --instance-type p4d.24xlarge \
  --key-name your-key
```

### 2. Connect and verify GPU

```bash
ssh -i your-key.pem ubuntu@<instance-ip>

# Verify NVIDIA driver
nvidia-smi
```

### 3. Install dependencies

```bash
# Install Go
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Clone Navarch (or copy from local)
git clone https://github.com/NavarchProject/navarch.git
cd navarch
```

### 4. Run tests

```bash
# Quick GPU test
./scripts/test-gpu.sh

# Full end-to-end test
./scripts/test-e2e.sh

# Stress test (5 minutes)
./scripts/stress-gpu.sh 300
```

## Test scenarios

### Basic GPU detection

Run GPU tests to verify the GPU manager works correctly:

```bash
go test ./pkg/gpu/... -v
```

Expected output:
```
=== RUN   TestInjectable_NewInjectable
--- PASS: TestInjectable_NewInjectable (0.00s)
=== RUN   TestInjectable_CollectHealthEvents
--- PASS: TestInjectable_CollectHealthEvents (0.00s)
...
```

### Health check validation

Run the node daemon and verify health checks:

```bash
# Terminal 1: Start control plane
go run ./cmd/control-plane

# Terminal 2: Start node daemon
go run ./cmd/node --node-id test-gpu --provider test

# Terminal 3: Check health after 65 seconds
sleep 65
go run ./cmd/navarch get test-gpu
```

Expected output:
```
Health: Healthy
```

### Health event detection

Check for GPU errors in system logs:

```bash
# Check for existing XID errors in dmesg
sudo dmesg | grep -i "NVRM: Xid"

# Run health event tests
go test ./pkg/gpu/... -v -run TestInjectable_CollectHealthEvents
```

### Command delivery

Test that commands reach the node:

```bash
# Cordon the node
go run ./cmd/navarch cordon test-gpu

# Check node logs for command receipt
grep "received command" /tmp/node.log
```

## Multi-GPU testing

For instances with multiple GPUs (e.g., 8x A100):

```bash
# Verify all GPUs detected
go run ./cmd/navarch get test-gpu | grep -A 100 "GPUs:"
```

Expected: All 8 GPUs listed with unique UUIDs.

## Testing with control plane on separate machine

Use SSH reverse tunnel to run control plane locally:

```bash
# On your laptop: Start control plane
go run ./cmd/control-plane

# SSH with reverse tunnel
ssh -R 50051:localhost:50051 ubuntu@<gpu-instance>

# On GPU instance: Start node
go run ./cmd/node --node-id remote-gpu --provider lambda
```

This allows testing the full distributed architecture.

## Stress testing

### GPU load test

```bash
# Run stress test with health monitoring
./scripts/stress-gpu.sh 600  # 10 minutes

# Watch for:
# - Temperature increases (should stay below 83°C)
# - Power usage (should stay within TDP)
# - XID errors (should be none)
```

### Sustained operation test

Run the node daemon for extended periods:

```bash
# Start node daemon
nohup go run ./cmd/node --node-id endurance-test > /tmp/node.log 2>&1 &

# Monitor for 24 hours
watch -n 60 'go run ./cmd/navarch get endurance-test | grep -E "Health|Heartbeat"'
```

## Validating specific GPU types

### NVIDIA A100

- Memory: 40GB or 80GB
- Expected temperature: 30-45°C idle, 60-75°C under load
- XID codes to watch: 79 (fallen off bus), 48 (DBE)

### NVIDIA H100

- Memory: 80GB
- Expected temperature: 25-40°C idle, 55-70°C under load
- Higher power consumption (700W TDP)

### NVIDIA L4

- Memory: 24GB
- Lower power consumption
- Good for inference workloads

## Troubleshooting

### GPU not detected

```bash
# Check NVIDIA driver
nvidia-smi

# Check that driver is loaded
lsmod | grep nvidia

# Reinstall driver if needed
sudo apt-get install --reinstall nvidia-driver-535
```

### Health checks failing

```bash
# Check node daemon logs
cat /tmp/node.log | grep -i error

# Check dmesg for GPU errors
sudo dmesg | grep -i nvidia | tail -20
```

### High temperature warnings

```bash
# Check current temperature
nvidia-smi --query-gpu=temperature.gpu --format=csv

# Check cooling (data center GPUs need proper airflow)
nvidia-smi --query-gpu=fan.speed --format=csv
```

## Cleanup

Remember to terminate GPU instances after testing:

```bash
# Lambda Labs: Use console

# GCP
gcloud compute instances delete navarch-test --zone=us-central1-a

# AWS
aws ec2 terminate-instances --instance-ids i-xxxxx
```

GPU instances are expensive. Always terminate when done.

