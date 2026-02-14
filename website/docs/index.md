---
hide:
  - navigation
  - toc
---

<div style="text-align: center; padding: 2rem 1rem 1.5rem;">

<img src="images/logo.svg" alt="Navarch" style="width: 100px; height: auto; margin-bottom: 1rem;">

<h1 style="font-size: 2.5rem; font-weight: 700; margin: 0 0 0.25rem; letter-spacing: -0.02em; color: var(--md-default-fg-color);">Navarch</h1>

<p style="font-size: 1.25rem; font-weight: 500; margin: 0 0 0.75rem; opacity: 0.8;">Open-source GPU fleet management</p>

<p style="font-size: 0.875rem; max-width: 520px; margin: 0 auto 1.5rem; line-height: 1.6; opacity: 0.7;">Navarch automates provisioning, health monitoring, and lifecycle management of GPU nodes across cloud providers.</p>

<div style="display: flex; justify-content: center; gap: 0.75rem; flex-wrap: wrap; margin-bottom: 1rem;">
<a href="getting-started/" class="md-button md-button--primary">Get Started</a>
<a href="concepts/" class="md-button">Learn Concepts</a>
<a href="https://github.com/NavarchProject/navarch" class="md-button">GitHub</a>
</div>

<span style="display: inline-block; font-size: 0.75rem; font-weight: 500; padding: 0.25rem 0.75rem; background: rgba(255, 178, 36, 0.15); color: #d97706; border-radius: 9999px;">⚠️ Experimental — not production-ready</span>

</div>

---

<div class="grid cards" markdown>

-   :material-heart-pulse: **[Health Monitoring](concepts/health.md)**

    ---

    Detect GPU failures in real time. Catches XID errors, thermal issues, ECC faults, and NVLink failures via NVML before they crash your workloads.

-   :material-refresh-auto: **[Auto-Replacement](concepts/lifecycle.md)**

    ---

    Unhealthy nodes get terminated and replaced automatically. Define health policies with CEL expressions. Your pool stays at capacity.

-   :material-cloud-sync: **[Multi-Cloud](concepts/pools.md)**

    ---

    Provision across Lambda Labs, GCP, and AWS from a single config. Failover between providers or optimize for cost.

-   :material-arrow-expand-all: **[Autoscaling](concepts/autoscaling.md)**

    ---

    Scale based on GPU utilization, queue depth, schedules, or predictions. Cooldown prevents thrashing. Combine multiple strategies.

-   :material-view-grid-outline: **[Pool Management](pool-management.md)**

    ---

    Group nodes by instance type, region, or workload. Set scaling limits, health policies, and labels per pool.

-   :material-flask-outline: **[Simulator](simulator/index.md)**

    ---

    Test policies and failure scenarios locally. Stress test with 1000+ simulated nodes before deploying to production.

</div>

---

## Why Navarch

GPUs fail. Cloud providers give you instances, but detecting hardware failures and replacing bad nodes is your problem. Teams end up building custom monitoring with DCGM, dmesg parsing, and cloud-specific scripts. Then there's the multi-cloud problem: different APIs, different instance types, different tooling.

Navarch makes your GPU supply self-healing and fungible across clouds, all under one system to manage it all:

- **Unified health monitoring** for XID errors, thermal events, ECC faults, and NVLink
- **Automatic replacement** when nodes fail health checks
- **Source GPUs anywhere.** Lambda out of H100s? Failover to GCP or AWS automatically.
- **Single control plane** for Lambda, GCP, and AWS. One config, one API.
- **Works with your scheduler.** Kubernetes, SLURM, or bare metal.

---

## How it works

<img src="images/navarch_overview.png" alt="Navarch architecture" style="max-width: 540px; width: 100%;">

The **control plane** manages pools, evaluates health policies, and provisions or terminates instances through cloud provider APIs.

The **node agent** runs on each GPU instance. It reports health via NVML, sends heartbeats, and executes commands from the control plane.

Navarch complements your existing scheduler. It handles infrastructure; your scheduler places workloads.

---

## Quick look

```yaml
# navarch.yaml
providers:
  lambda:
    type: lambda
    api_key_env: LAMBDA_API_KEY

pools:
  training:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-1
    min_nodes: 2
    max_nodes: 8
    health:
      auto_replace: true
    autoscaling:
      type: reactive
      scale_up_at: 80
      scale_down_at: 20
```

```bash
control-plane -config navarch.yaml
```

---

## Next steps

<div class="grid cards" markdown>

-   **Getting Started**

    ---

    Set up Navarch with Lambda Labs.

    [:octicons-arrow-right-24: Getting started](getting-started.md)

-   **Core Concepts**

    ---

    Pools, providers, health checks, node lifecycle.

    [:octicons-arrow-right-24: Concepts](concepts/index.md)

-   **Configuration**

    ---

    Full reference for navarch.yaml.

    [:octicons-arrow-right-24: Configuration](configuration.md)

-   **Architecture**

    ---

    How Navarch integrates with your stack.

    [:octicons-arrow-right-24: Architecture](architecture.md)

</div>
