# Navarch MVP Gaps

Status of remaining work for MVP release.

---

## Completed ✅

- **NVML GPU Manager** — Real NVML integration with automatic fallback to fake manager
- **XID Error Collection** — Dual strategy: NVML events + kernel log parsing
- **Prometheus Metrics** — `/metrics` endpoint with node counts
- **Basic Authentication** — Token-based auth with middleware
- **File Upload in Bootstrap** — SCP files before running setup commands
- **Cross-compilation** — Build tags enable `CGO_ENABLED=0` Linux builds from macOS

---

## Remaining Work

### Task 1: AWS Provider

**Priority**: 🟡 High
**Estimated Effort**: 2-3 days

The AWS provider is stubbed. Implement using AWS SDK v2:

- `Provision` via `ec2.RunInstances`
- `Terminate` via `ec2.TerminateInstances`
- `List` via `ec2.DescribeInstances`

GPU instance types: p4d.24xlarge, p5.48xlarge, g5.xlarge, etc.

**Files**: `pkg/provider/aws/aws.go`

---

### Task 2: Uncordon Command

**Priority**: 🟢 Low
**Estimated Effort**: 0.5 days

The `navarch uncordon` CLI command is stubbed. Implement to transition cordoned nodes back to ready.

**Files**: `cmd/navarch/drain.go`

---

### Task 3: Partial Provisioning Bug

**Priority**: 🟡 High
**Estimated Effort**: 1 day

When provisioning N nodes and some fail, successfully provisioned nodes may not be tracked. Observed during E2E testing: provision 2 nodes, second fails, first is orphaned.

**Files**: `pkg/pool/pool.go` (ScaleUp logic)

---

### Task 4: Documentation

**Priority**: 🟡 High
**Estimated Effort**: 1-2 days

- Getting started guide
- Configuration reference
- Bootstrap template variables
- Provider setup (Lambda, GCP)

---

## Summary

| Task | Priority | Effort |
|------|----------|--------|
| AWS Provider | 🟡 High | 2-3 days |
| Uncordon Command | 🟢 Low | 0.5 days |
| Partial Provisioning Bug | 🟡 High | 1 day |
| Documentation | 🟡 High | 1-2 days |
