# Troubleshooting

Common issues and how to resolve them.

## Control plane issues

### Control plane won't start

**Symptom**: Control plane exits immediately or fails to bind.

**Check the logs**:
```bash
control-plane -config navarch.yaml 2>&1 | head -50
```

**Common causes**:

| Error | Cause | Fix |
|-------|-------|-----|
| `address already in use` | Another process on the port | Change `server.grpc_port` or stop the other process |
| `invalid configuration` | YAML syntax error | Validate with `yq` or an online YAML validator |
| `provider not found` | Missing provider config | Add the provider to `providers:` section |
| `failed to initialize provider` | Bad credentials | Check API key environment variables |

### Nodes not registering

**Symptom**: Nodes are running but don't appear in `navarch list`.

**Check node agent logs**:
```bash
journalctl -u navarch-node -f
```

**Common causes**:

| Error | Cause | Fix |
|-------|-------|-----|
| `connection refused` | Wrong control plane address | Check `--server` flag or `NAVARCH_SERVER` env var |
| `TLS handshake failed` | Certificate mismatch | Use `--insecure` for testing or fix certificates |
| `authentication failed` | Bad or missing token | Check `--token` flag matches control plane config |
| `context deadline exceeded` | Network/firewall issue | Check security groups allow gRPC port |

### Health checks not running

**Symptom**: Nodes show as healthy but health data is stale.

**Check**:
1. Node agent is running: `systemctl status navarch-node`
2. NVML is accessible: `nvidia-smi` works on the node
3. Health check interval in config isn't too long

## Node agent issues

### NVML initialization failed

**Symptom**: Node agent logs `failed to initialize NVML` or `NVML not found`.

**Causes and fixes**:

1. **NVIDIA drivers not installed**
   ```bash
   nvidia-smi  # Should show GPU info
   ```
   If not working, install drivers.

2. **libnvidia-ml.so not in path**
   ```bash
   ldconfig -p | grep nvidia-ml
   ```
   If missing, the NVIDIA driver installation is incomplete.

3. **Running in container without GPU access**
   ```bash
   docker run --gpus all ...  # Need --gpus flag
   ```

### Node marked unhealthy but GPUs are fine

**Symptom**: Node transitions to unhealthy but `nvidia-smi` shows no issues.

**Check health events**:
```bash
navarch get <node-id> -o json | jq '.health_events'
```

**Common causes**:
- Transient XID error that resolved
- Thermal throttling that recovered
- Network blip caused missed heartbeats

**Resolution**: If the issue resolved, the node will recover automatically (for non-fatal errors). For fatal XID errors, the node must be replaced.

### Node agent high CPU usage

**Symptom**: Node agent using excessive CPU.

**Common causes**:
- Health check interval too short (sub-second)
- NVML calls hanging and retrying
- Debug logging enabled in production

**Fix**: Set reasonable intervals in config:
```yaml
heartbeat_interval: 30s
health_check_interval: 60s
```

## Autoscaling issues

### Pool not scaling up

**Symptom**: Utilization is high but no new nodes are provisioned.

**Check**:

1. **At max capacity?**
   ```bash
   navarch pool status <pool-name>
   ```
   If `total_nodes == max_nodes`, can't scale further.

2. **Cooldown active?**
   ```bash
   grep "cooldown" control-plane.log
   ```
   Wait for cooldown to expire.

3. **Provider quota exceeded?**
   Check cloud provider console for quota limits.

4. **Metrics not flowing?**
   Ensure nodes are sending heartbeats with utilization data.

### Pool not scaling down

**Symptom**: Utilization is low but nodes aren't terminated.

**Check**:

1. **At min capacity?**
   If `total_nodes == min_nodes`, can't scale lower.

2. **Cooldown active?**
   Scale-down also respects cooldown.

3. **Nodes cordoned?**
   Cordoned nodes are prioritized for removal but won't be removed if at min.

### Autoscaler oscillating

**Symptom**: Pool scales up and down repeatedly.

**Causes**:
- Cooldown too short
- Thresholds too close together
- Utilization hovering at threshold

**Fix**: Increase cooldown and widen threshold gap:
```yaml
autoscaling:
  type: reactive
  scale_up_at: 80
  scale_down_at: 30  # Gap of 50 points
cooldown_period: 10m
```

## Provider issues

### Lambda Labs: "No available instances"

**Symptom**: Provisioning fails with availability error.

**Cause**: Lambda Labs has limited inventory. Requested instance type not available in region.

**Fixes**:
- Try a different region
- Try a different instance type
- Set up multi-region failover in pool config
- Wait and retry (inventory changes)

### GCP: "Quota exceeded"

**Symptom**: Provisioning fails with quota error.

**Fix**: Request quota increase in GCP console for:
- GPUs (per region)
- vCPUs
- IP addresses

### AWS: "InsufficientInstanceCapacity"

**Symptom**: Provisioning fails with capacity error.

**Fixes**:
- Try a different availability zone
- Use capacity reservations for guaranteed access
- Try Spot instances if workload tolerates interruption

## CLI issues

### "Connection refused"

**Symptom**: CLI commands fail with connection error.

**Fixes**:
1. Check control plane is running
2. Check address: `navarch -s http://correct-host:50051 list`
3. Check firewall allows traffic

### "Permission denied"

**Symptom**: Commands fail with auth error.

**Fix**: Provide valid token:
```bash
export NAVARCH_TOKEN=your-token
navarch list
```

Or:
```bash
navarch --token your-token list
```

## Simulator issues

### Scenario fails to load

**Symptom**: `simulator run` fails with parse error.

**Fix**: Validate scenario syntax:
```bash
simulator validate scenarios/my-scenario.yaml
```

Check for:
- YAML indentation errors
- Invalid action names
- Missing required fields

### Stress test runs out of memory

**Symptom**: Simulator crashes or system becomes unresponsive during stress test.

**Cause**: Too many simulated nodes for available RAM.

**Fixes**:
- Reduce `total_nodes`
- Use `wave` startup pattern with longer duration
- Increase system memory
- Run on a larger machine

See [Simulator performance considerations](simulator/stress-testing.md#performance-considerations) for memory estimates.

## Getting more help

If your issue isn't covered here:

1. Check existing [GitHub issues](https://github.com/NavarchProject/navarch/issues)
2. Open a new issue with:
   - Navarch version (`navarch version`)
   - Config file (redact secrets)
   - Relevant logs
   - Steps to reproduce
