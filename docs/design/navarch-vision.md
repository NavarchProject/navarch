# Navarch: Simple Multi-Cloud GPU Infrastructure

## Philosophy

**Do one thing well**: Provision and manage GPU nodes across clouds.

**Simple interfaces**: One obvious way to do each thing.

**Composable**: Works with tools you already know (ssh, rsync, kubectl).

**Explicit**: No magic. You see what's happening.

---

## Quick Start

```bash
# Install
curl -sSL https://navarch.io/install | sh

# Configure (interactive)
navarch init

# Run on a GPU
navarch run train.py --gpu h100
```

---

## CLI Reference

```bash
# Running jobs
navarch run <script>         Run script on GPU
navarch run --gpu h100       Specify GPU type
navarch run -f config.yaml   Use config file

# Interactive development
navarch dev                  Start dev environment
navarch dev --gpu a100       With specific GPU

# Job management
navarch ps                   List jobs
navarch logs <id>            View logs
navarch stop <id>            Stop job
navarch ssh <id>             SSH into job

# Pools (optional)
navarch pool create <name>   Create node pool
navarch pool scale <name> N  Scale to N nodes
navarch pool status          Show all pools
navarch pool delete <name>   Delete pool

# Info
navarch status               Overview of everything
navarch gpus                 List available GPU types and prices
```

---

## Configuration

### Minimal (most jobs)

```yaml
gpu: h100
run: python train.py
```

### Full Specification

```yaml
# navarch.yaml
name: llama-finetune          # Job name (optional)

# Compute
gpu: h100                     # GPU type (required)
gpus: 8                       # GPU count (default: 1)
nodes: 1                      # Node count (default: 1)

# Cost
spot: true                    # Use spot instances (default: false)
max_price: 2.50               # Max $/GPU/hr (optional)

# Environment
image: pytorch/pytorch:2.5    # Docker image (optional)
env:                          # Environment variables
  WANDB_API_KEY: ${WANDB_KEY}

# Code
workdir: .                    # Sync this directory (default: current)

# Data
mounts:                       # Cloud storage mounts
  - s3://my-bucket/data:/data

# Execution
setup: pip install -r requirements.txt
run: torchrun --nproc_per_node=8 train.py
```

### What We Don't Include

| Field | Why Not |
|-------|---------|
| `cpu`, `memory` | Auto-sized based on GPU |
| `disk` | Sensible default (200GB), override only if needed |
| `region`, `zone` | Auto-selected for cost/availability |
| `providers` | Global config, not per-job |
| `volumes` | Use `mounts` with cloud storage |
| `recovery` | App responsibility (checkpointing) |

---

## Components

### Core: Node Provisioning

Navarch provisions GPU nodes. That's the core job.

```
┌─────────────────────────────────────────────────┐
│                   navarch CLI                   │
├─────────────────────────────────────────────────┤
│                                                 │
│  ┌─────────────┐  ┌─────────────────────────┐  │
│  │  Scheduler  │  │       Providers         │  │
│  │             │──│  AWS | GCP | Lambda     │  │
│  │  • Cost     │  │  Azure | CoreWeave      │  │
│  │  • Spot     │  │  RunPod | Vast.ai       │  │
│  │  • Failover │  │  Kubernetes | On-prem   │  │
│  └─────────────┘  └─────────────────────────┘  │
│                                                 │
└─────────────────────────────────────────────────┘
```

### Optional: Pools

Long-running node pools for teams that need persistent capacity.

```bash
navarch pool create training --gpu h100 --min 2 --max 10 --spot
```

Pools handle:
- Maintaining minimum node count
- Health monitoring and replacement
- Scaling up/down

### Optional: Reservations

Guarantee capacity for scheduled work.

```bash
navarch reserve --name "big-training" \
  --gpu h100 --count 32 \
  --start "2026-02-20 09:00" --hours 12
```

---

## Storage

### Cloud Storage Mounts

```yaml
mounts:
  - s3://my-bucket/datasets:/data
  - gs://my-bucket/checkpoints:/checkpoints
```

Uses cloud-native storage. No abstraction layer. Works everywhere.

### Why No Volume Abstraction?

| Abstraction | Problem |
|------------|---------|
| Persistent volumes | Cloud-specific, region-locked |
| Distributed FS | Complex, expensive |
| Our own abstraction | More code, more bugs |

**Just use S3/GCS.** It's simple, it works, users understand it.

---

## Spot Instances

### Simple Opt-In

```yaml
spot: true
```

### What Happens on Preemption

1. Navarch detects preemption (AWS 2-min warning, GCP 30-sec, or health check)
2. Job status changes to `PREEMPTED`
3. If pool exists, new node provisions automatically
4. Your app restores from checkpoint (app's responsibility)

### Checkpointing (App Side)

```python
# Your training script handles checkpoints
checkpoint_dir = os.environ.get('CHECKPOINT_DIR', './checkpoints')

# Save periodically
if step % 1000 == 0:
    torch.save(model.state_dict(), f'{checkpoint_dir}/step_{step}.pt')

# Load on start
latest = find_latest_checkpoint(checkpoint_dir)
if latest:
    model.load_state_dict(torch.load(latest))
```

We don't wrap this. Checkpointing is framework-specific. Your code, your choice.

---

## Multi-Node Training

```yaml
gpu: h100
gpus: 8
nodes: 4           # 4 nodes × 8 GPUs = 32 total

run: |
  torchrun \
    --nnodes=$NAVARCH_NNODES \
    --nproc_per_node=$NAVARCH_GPUS \
    --master_addr=$NAVARCH_MASTER \
    train.py
```

Environment variables we set:
- `NAVARCH_NNODES` - Node count
- `NAVARCH_NODE_RANK` - This node's rank
- `NAVARCH_GPUS` - GPUs per node
- `NAVARCH_MASTER` - Master node address

That's it. Standard torchrun/deepspeed/etc. works.

---

## Dev Environments

```bash
navarch dev --gpu h100
```

Output:
```
Provisioning H100 on Lambda Labs... done (18s)

SSH:     ssh navarch-abc123
VS Code: code --remote ssh-remote+navarch-abc123 /home/user/project

Code synced from: /Users/you/project
Auto-stop after: 30m idle

Ctrl+C to detach (instance keeps running)
```

### What It Does

1. Provisions GPU instance
2. Sets up SSH access
3. Syncs your code
4. Forwards ports (8888, 6006, etc.)
5. Auto-stops on idle

### What It Doesn't Do

- Custom IDE integration (use standard SSH remote)
- Special Jupyter setup (just `pip install jupyter && jupyter lab`)
- Magic environment management (use conda/venv like normal)

---

## Provider Configuration

### Global Config (~/.navarch/config.yaml)

```yaml
providers:
  lambda:
    api_key: ${LAMBDA_API_KEY}
    priority: 1
  gcp:
    project: my-project
    priority: 2
  aws:
    region: us-east-1
    priority: 3

defaults:
  gpu: h100
  spot: true
```

Per-job config overrides globals. Simple precedence.

---

## What We Don't Do

| Feature | Why Not |
|---------|---------|
| Job queuing | Use existing schedulers (Slurm, K8s, etc.) |
| Workflow DAGs | Use Airflow, Prefect, etc. |
| Experiment tracking | Use W&B, MLflow, etc. |
| Model registry | Use HuggingFace, your own, etc. |
| Kubernetes orchestration | Use SkyPilot, etc. |

**We provision GPUs.** Everything else composes with tools built for that job.

---

## Comparison

| | Navarch | SkyPilot | dstack |
|-|---------|----------|--------|
| **Focus** | GPU provisioning | Job orchestration | AI platform |
| **Complexity** | Minimal | Medium | Higher |
| **Config** | ~5 fields | ~30 fields | ~40 fields |
| **Abstractions** | Few | Many | Many |
| **Best for** | "Just give me GPUs" | Full ML workflow | Teams wanting platform |

---

## Implementation Priority

### Phase 1: Core (Now)
- [x] Multi-provider pools
- [x] Price-aware selection
- [ ] Spot support with preemption detection
- [ ] `navarch run` one-liner

### Phase 2: Dev Experience
- [ ] `navarch dev` environments
- [ ] Code sync
- [ ] Auto-stop on idle

### Phase 3: Scale
- [ ] Multi-node support
- [ ] Reservations
- [ ] Team features

---

## Success Metrics

1. **Time to GPU**: < 30 seconds from `navarch run` to code executing
2. **Config lines**: < 5 for typical job
3. **Learning curve**: Productive in 5 minutes
4. **Reliability**: 99.9% successful provisions when capacity exists
