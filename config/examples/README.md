# Node Doctor Example Configurations

This directory contains example configuration files for Node Doctor that demonstrate various use cases and features.

## Quick Start

Choose the configuration that best matches your needs:

| Configuration | Use Case | Features |
|--------------|----------|----------|
| **[minimal.yaml](#minimal)** | Quick start, learning | Single monitor, basic setup |
| **[development.yaml](#development)** | Local development | Debug logging, all monitors, dry-run remediation |
| **[production.yaml](#production)** | Production deployment | Comprehensive monitoring, safety mechanisms |
| **[custom-plugins.yaml](#custom-plugins)** | Extensibility showcase | Custom monitors, log patterns, multi-step remediation |

## Configuration Files

### minimal.yaml

**Purpose:** Bare minimum configuration for getting started quickly

**Use cases:**
- Learning Node Doctor basics
- Quick testing and validation
- Minimal resource footprint

**Key features:**
- Single disk monitor (most critical for node health)
- Kubernetes exporter only
- Remediation disabled for safety
- Minimal resource usage

**To use:**
```bash
node-doctor --config=/path/to/config/examples/minimal.yaml
```

**Environment variables required:**
- `NODE_NAME`: Kubernetes node name

---

### development.yaml

**Purpose:** Development and debugging configuration

**Use cases:**
- Local development and testing
- Debugging monitor behavior
- Testing remediation logic safely

**Key features:**
- **Debug logging** - Maximum visibility into operations
- **All monitor types enabled** - Test complete functionality
- **Short intervals** - Fast feedback (15s monitors, 1m heartbeat)
- **Dry-run remediation** - Logs actions without executing
- **Profiling enabled** - Performance analysis (port 6060)
- **Local webhook example** - HTTP exporter to localhost:8080
- **Text log format** - Human-readable for development

**To use:**
```bash
# Set environment variables
export NODE_NAME=dev-node
export VERSION=v0.1.0-dev

# Run with development config
node-doctor --config=/path/to/config/examples/development.yaml
```

**Environment variables required:**
- `NODE_NAME`: Node name
- `VERSION`: Application version (optional)

**Monitors included:**
- System: CPU, Memory, Disk
- Kubernetes: Kubelet, API Server, Runtime, Capacity
- Network: DNS, Gateway, Connectivity

---

### production.yaml

**Purpose:** Comprehensive production-ready configuration

**Use cases:**
- Production Kubernetes cluster deployment
- Reference for production best practices
- Starting point for customization

**Key features:**
- **Comprehensive monitoring** - All system, Kubernetes, and network monitors
- **Production logging** - JSON format for log aggregation
- **Safety mechanisms** - Circuit breaker, rate limiting, cooldowns
- **All exporters** - Kubernetes, HTTP webhooks, Prometheus
- **Remediation enabled** - Automated problem fixing
- **Multiple webhook examples** - PagerDuty, Slack, internal monitoring
- **Problem-specific overrides** - Custom settings per problem type

**To use:**
```bash
# Deploy via Kubernetes ConfigMap
kubectl create configmap node-doctor-config \
  --from-file=config.yaml=production.yaml \
  -n node-doctor

# Or run directly
node-doctor --config=/path/to/config/examples/production.yaml
```

**Environment variables required:**
- `NODE_NAME`: Kubernetes node name
- `VERSION`: Application version
- `CLUSTER_NAME`: Cluster identifier
- `PAGERDUTY_TOKEN`: PagerDuty integration token (if using PagerDuty webhook)
- `SLACK_WEBHOOK_PATH`: Slack webhook path (if using Slack webhook)
- `MONITORING_USER`: Monitoring system username (if using authenticated webhook)
- `MONITORING_PASSWORD`: Monitoring system password (if using authenticated webhook)

**Important notes:**
- Review and customize thresholds for your workload
- Configure webhook URLs for your environment
- Adjust remediation cooldowns based on your SLAs
- Test remediation in dry-run mode first

---

### custom-plugins.yaml

**Purpose:** Demonstrate Node Doctor extensibility

**Use cases:**
- Implementing custom monitoring logic
- Log pattern monitoring
- Multi-step remediation strategies
- Integration with existing monitoring scripts

**Key features:**
- **Custom plugin monitors** - Execute external scripts/binaries
- **Log pattern monitoring** - Regex-based log analysis
- **Multi-step remediation** - Progressive escalation strategies
- **Monitor dependencies** - Control execution order
- **Comprehensive examples** - Hardware checks, compliance, app health

**To use:**
```bash
# Ensure custom scripts exist and are executable
chmod +x /usr/local/bin/check-hardware-health.sh
chmod +x /usr/local/bin/check-critical-app.py

# Run with custom plugins config
node-doctor --config=/path/to/config/examples/custom-plugins.yaml
```

**Plugin requirements:**
- Executables must return exit codes: 0=OK, 1=WARNING, 2=CRITICAL, 3=UNKNOWN
- Must complete within the configured timeout
- Should print details to stdout for logging

**Example custom plugin:**
```bash
#!/bin/bash
# /usr/local/bin/check-hardware-health.sh
if mdadm --detail /dev/md0 | grep -q "State : clean"; then
    echo "RAID healthy"
    exit 0
else
    echo "RAID degraded"
    exit 2
fi
```

---

## Customization Guide

### Choosing a Starting Point

1. **New to Node Doctor?** Start with `minimal.yaml`
2. **Developing/Testing?** Use `development.yaml`
3. **Production deployment?** Start with `production.yaml` and customize
4. **Need custom monitors?** Reference `custom-plugins.yaml`

### Common Customizations

#### Adjust Monitor Intervals

```yaml
monitors:
  - name: disk-health
    type: system-disk
    interval: 5m      # Change from default 1m
    timeout: 1m       # Increase timeout for slower systems
```

#### Change Threshold Values

```yaml
monitors:
  - name: cpu-health
    type: system-cpu
    config:
      loadAverageThresholds:
        warning: 70   # Lower threshold (more sensitive)
        critical: 90  # Higher threshold (less aggressive)
```

#### Enable/Disable Monitors

```yaml
monitors:
  - name: memory-health
    type: system-memory
    enabled: false    # Disable this monitor
```

#### Configure Remediation

```yaml
monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    remediation:
      enabled: true              # Enable remediation
      strategy: systemd-restart  # Strategy to use
      service: kubelet           # Service to restart
      cooldown: 15m              # Wait 15m between attempts
      maxAttempts: 2             # Try at most 2 times
```

#### Add Custom Webhooks

```yaml
exporters:
  http:
    enabled: true
    webhooks:
      - name: my-webhook
        url: https://monitoring.mycompany.com/api/v1/webhook
        sendStatus: true
        sendProblems: true
        auth:
          type: basic
          username: "${WEBHOOK_USER}"
          password: "${WEBHOOK_PASS}"
```

### Environment Variable Substitution

All configuration values support environment variable substitution using `${VAR_NAME}` syntax:

```yaml
settings:
  nodeName: "${NODE_NAME}"           # Required

exporters:
  prometheus:
    labels:
      cluster: "${CLUSTER_NAME}"     # Optional
      datacenter: "${DATACENTER}"    # Optional
```

### Validation

Before deploying a custom configuration, validate it:

```bash
# Run validation tests
cd config/examples
go test -v

# Test configuration loading
node-doctor --config=my-custom-config.yaml --validate
```

## Configuration Schema

For complete schema documentation, see:
- [docs/configuration.md](../../docs/configuration.md) - Full configuration reference
- [pkg/types/config.go](../../pkg/types/config.go) - Type definitions

## Best Practices

### Monitor Configuration

1. **Use appropriate intervals**
   - Critical checks: 30s-1m
   - Standard checks: 1m-5m
   - Expensive checks: 5m-15m

2. **Set realistic timeouts**
   - Timeout should be < interval
   - Account for slow I/O operations
   - Consider network latency

3. **Configure dependencies**
   - Run network checks before API server checks
   - Run basic checks before dependent checks

### Remediation Safety

1. **Start with dry-run mode**
   ```yaml
   remediation:
     enabled: true
     dryRun: true  # Log actions without executing
   ```

2. **Use conservative cooldowns**
   - Kubelet restart: 10m-15m minimum
   - Container runtime: 15m-30m minimum
   - Node reboot: Never automated (requires manual intervention)

3. **Enable circuit breaker**
   ```yaml
   remediation:
     circuitBreaker:
       enabled: true
       threshold: 5       # Open after 5 failures
       timeout: 30m       # Stay open for 30 minutes
   ```

4. **Use problem-specific overrides**
   ```yaml
   remediation:
     overrides:
       - problem: critical-system-failure
         cooldown: 30m
         maxAttempts: 1
   ```

### Exporter Configuration

1. **Kubernetes exporter** - Always enabled for node conditions
2. **Prometheus exporter** - Enable for metrics collection
3. **HTTP exporter** - Configure for external alerting systems

### Performance Tuning

1. **Limit concurrent operations**
   ```yaml
   settings:
     qps: 50      # Queries per second
     burst: 100   # Burst capacity
   ```

2. **Adjust HTTP worker pool**
   ```yaml
   exporters:
     http:
       workers: 5       # Concurrent webhook workers
       queueSize: 100   # Queue buffer size
   ```

3. **Configure hot reload carefully**
   ```yaml
   reload:
     enabled: true
     debounceInterval: 2s  # Wait for multiple changes
   ```

## Troubleshooting

### Configuration won't load

```bash
# Check YAML syntax
yamllint my-config.yaml

# Validate configuration
node-doctor --config=my-config.yaml --validate

# Check environment variables
env | grep NODE_NAME
```

### Monitor type not found

```
Error: unknown monitor type "my-custom-monitor"
```

**Solution:** Check that the monitor type is registered. Available types:
- System: `system-cpu`, `system-memory`, `system-disk`
- Kubernetes: `kubernetes-kubelet-check`, `kubernetes-apiserver-check`, `kubernetes-runtime-check`, `kubernetes-capacity-check`
- Network: `network-dns-check`, `network-gateway-check`, `network-connectivity-check`
- Custom: `custom-plugin`, `custom-logpattern`

### Circular dependency detected

```
Error: circular dependency detected in monitors: monitor-a → monitor-b → monitor-a
```

**Solution:** Review `dependsOn` fields and remove circular references.

### Remediation not executing

**Check:**
1. Global remediation enabled: `remediation.enabled: true`
2. Not in dry-run mode: `remediation.dryRun: false`
3. Monitor remediation enabled: `monitors[].remediation.enabled: true`
4. Cooldown period hasn't been violated
5. Circuit breaker isn't open

## Additional Resources

- [Node Doctor Documentation](../../docs/)
- [Configuration Reference](../../docs/configuration.md)
- [Monitor Reference](../../docs/monitors.md)
- [Remediation Guide](../../docs/remediation.md)
- [Architecture Overview](../../docs/architecture.md)

## Contributing

Found an issue with an example configuration? Have a suggestion for improvement?

1. Open an issue: [GitHub Issues](https://github.com/supporttools/node-doctor/issues)
2. Submit a pull request with fixes or new examples
3. Share your production configurations (with sensitive data removed)

## License

See [LICENSE](../../LICENSE) for license information.
