# Node Doctor DaemonSet Deployment Guide

This directory contains the production-ready Kubernetes DaemonSet manifest for Node Doctor, a comprehensive node monitoring and auto-remediation tool.

## Quick Start

Deploy Node Doctor to your cluster:

```bash
kubectl apply -f deployment/daemonset.yaml
```

## What's Included

The `daemonset.yaml` file contains all necessary Kubernetes resources:

1. **DaemonSet** - Main Node Doctor deployment
2. **ServiceAccount** - Service account for API access
3. **ClusterRole** - RBAC permissions for node monitoring
4. **ClusterRoleBinding** - Binds service account to cluster role
5. **ConfigMap** - Node Doctor configuration
6. **Service** - Headless service for metrics scraping

## Security Requirements

### Privileged Access
Node Doctor requires privileged access for:
- **CPU Monitoring**: Load average, thermal throttling detection
- **Memory Monitoring**: Memory/swap usage, OOM detection via `/dev/kmsg`
- **Disk Monitoring**: Disk space, inodes, readonly detection, I/O health
- **Process Monitoring**: System process health checks
- **Container Runtime**: Docker/containerd socket access

### Host Path Mounts
- `/` → `/host` (readonly) - Host filesystem access
- `/dev/kmsg` → `/dev/kmsg` (readonly) - Kernel messages for OOM detection
- `/var/log` → `/var/log` (readonly) - System logs
- `/proc` → `/host/proc` (readonly) - Process information
- `/sys` → `/host/sys` (readonly) - System information
- `/etc/kubernetes` → `/etc/kubernetes` (readonly) - Kubernetes config
- Container runtime sockets (readonly) - Runtime monitoring

## Resource Requirements

- **CPU Request**: 50m (lightweight monitoring)
- **CPU Limit**: 200m (prevent runaway usage)
- **Memory Request**: 128Mi (sufficient for monitoring data)
- **Memory Limit**: 256Mi (prevent memory leaks)

## Tolerations

Node Doctor runs on **ALL** nodes with comprehensive tolerations:
- All taints with `operator: Exists`
- Control plane nodes (`master`, `control-plane`)
- Unhealthy nodes (`not-ready`, `unreachable`)
- Resource pressure (`disk-pressure`, `memory-pressure`, `pid-pressure`)

## Ports

- **8080**: Health/status endpoint (`/healthz`, `/ready`, `/status`)
- **9100**: Prometheus metrics endpoint (`/metrics`)

## Monitoring

### Health Checks
- **Liveness Probe**: `/healthz` on port 8080
- **Readiness Probe**: `/ready` on port 8080
- **Startup Probe**: `/healthz` with extended timeout

### Metrics
Prometheus metrics available at `:9100/metrics` with annotations for auto-discovery.

## Configuration

The ConfigMap includes monitoring for:
- **CPU**: Load average, thermal, usage thresholds
- **Memory**: Memory/swap usage, OOM detection
- **Disk**: Space, inodes, readonly, I/O health
- **Kubelet**: Health endpoint monitoring

## Verification

Check deployment status:

```bash
# Check DaemonSet status
kubectl get daemonset -n kube-system node-doctor

# Check pods on all nodes
kubectl get pods -n kube-system -l app=node-doctor -o wide

# Check node conditions
kubectl describe nodes | grep -A 5 "Conditions:"

# Check logs
kubectl logs -n kube-system -l app=node-doctor --tail=100

# Test health endpoint
kubectl exec -n kube-system $(kubectl get pods -n kube-system -l app=node-doctor -o jsonpath='{.items[0].metadata.name}') -- curl -s localhost:8080/healthz

# Check metrics
kubectl exec -n kube-system $(kubectl get pods -n kube-system -l app=node-doctor -o jsonpath='{.items[0].metadata.name}') -- curl -s localhost:9100/metrics | head -20
```

## Monitoring Integration

### Prometheus
Service includes prometheus scraping annotations:
```yaml
prometheus.io/scrape: "true"
prometheus.io/port: "9100"
prometheus.io/path: "/metrics"
```

### Grafana Dashboard
Metrics available for dashboard creation:
- `node_doctor_monitor_cpu_*` - CPU monitoring metrics
- `node_doctor_monitor_memory_*` - Memory monitoring metrics
- `node_doctor_monitor_disk_*` - Disk monitoring metrics
- `node_doctor_monitor_health_*` - Overall health metrics

## Troubleshooting

### Common Issues

1. **Permission Denied**
   - Ensure privileged security context is enabled
   - Check cluster security policies (PodSecurityPolicy/PodSecurityStandards)

2. **Host Path Mount Failures**
   - Verify paths exist on nodes
   - Check SELinux/AppArmor policies

3. **Network Issues**
   - Ensure hostNetwork is enabled
   - Check DNS resolution with ClusterFirstWithHostNet

4. **Resource Constraints**
   - Monitor resource usage
   - Adjust requests/limits if needed

### Debug Commands

```bash
# Check security context
kubectl get pods -n kube-system -l app=node-doctor -o jsonpath='{.items[0].spec.securityContext}'

# Check mounts
kubectl describe pods -n kube-system -l app=node-doctor | grep -A 20 "Mounts:"

# Check tolerations
kubectl get pods -n kube-system -l app=node-doctor -o jsonpath='{.items[0].spec.tolerations}'

# Check events
kubectl get events -n kube-system --field-selector involvedObject.name=node-doctor
```

## Production Considerations

1. **Resource Monitoring**: Monitor CPU/memory usage across nodes
2. **Log Rotation**: Ensure log rotation is configured for container logs
3. **Alerting**: Set up alerts for Node Doctor health and node conditions
4. **Updates**: Use rolling updates with `maxUnavailable: 1`
5. **Backup**: Include ConfigMap in cluster backup procedures

## Security Best Practices

1. **Least Privilege**: Only grant necessary RBAC permissions
2. **Read-Only Mounts**: Use read-only mounts where possible
3. **Resource Limits**: Enforce CPU/memory limits
4. **Network Policies**: Consider network policies for pod communication
5. **Image Security**: Use signed, scanned container images

## Support

For issues and questions:
- GitHub: https://github.com/supporttools/node-doctor
- Documentation: See `/docs` directory
- Configuration: See `/config` directory for examples