# Node Doctor Troubleshooting Guide

This guide provides solutions to common issues, debugging techniques, and best practices for operating Node Doctor in production Kubernetes environments.

## Table of Contents

- [Quick Diagnostics Checklist](#quick-diagnostics-checklist)
- [Common Issues and Solutions](#common-issues-and-solutions)
  - [Monitor Not Running](#monitor-not-running)
  - [Remediation Not Triggering](#remediation-not-triggering)
  - [High Resource Usage](#high-resource-usage)
  - [API Connection Issues](#api-connection-issues)
- [Debugging Techniques](#debugging-techniques)
  - [Analyzing Logs](#analyzing-logs)
  - [Monitoring Metrics](#monitoring-metrics)
  - [Kubernetes Events](#kubernetes-events)
  - [Health Endpoints](#health-endpoints)
- [Performance Tuning](#performance-tuning)
- [Security Considerations](#security-considerations)
- [Frequently Asked Questions](#frequently-asked-questions)
- [Advanced Troubleshooting](#advanced-troubleshooting)
- [Getting Help](#getting-help)

---

## Quick Diagnostics Checklist

When encountering issues with Node Doctor, start with this checklist:

- [ ] **Pod Status**: `kubectl get pods -n <namespace> -l app=node-doctor` - Are all pods Running?
- [ ] **Recent Logs**: `kubectl logs -n <namespace> <pod-name> --tail=50` - Any error messages?
- [ ] **Events**: `kubectl get events -n <namespace> --sort-by='.lastTimestamp'` - Recent warnings or errors?
- [ ] **ConfigMap**: `kubectl get configmap node-doctor-config -n <namespace> -o yaml` - Configuration valid?
- [ ] **RBAC**: `kubectl auth can-i --list --as=system:serviceaccount:<namespace>:node-doctor` - Correct permissions?
- [ ] **Node Conditions**: `kubectl get nodes -o custom-columns=NAME:.metadata.name,READY:.status.conditions[?\(@.type==\"Ready\"\)].status` - Nodes healthy?
- [ ] **Resource Limits**: `kubectl top pods -n <namespace> -l app=node-doctor` - Resource usage within limits?
- [ ] **Network Connectivity**: `kubectl exec -n <namespace> <pod-name> -- wget -O- http://kubernetes.default.svc/healthz` - API server reachable?

---

## Common Issues and Solutions

### Monitor Not Running

**Symptoms**:
- Monitor missing from status output
- No problems detected despite known issues
- Log messages: "Monitor disabled", "Failed to start monitor"

**Possible Causes**:

#### 1. Monitor Disabled in Configuration

**Diagnosis**:
```bash
kubectl get configmap node-doctor-config -n <namespace> -o jsonpath='{.data.config\.yaml}' | grep -A5 "monitors:"
```

**Solution**:
Ensure the monitor is enabled in the ConfigMap:
```yaml
monitors:
  - type: cpu
    enabled: true  # Must be true
    config:
      threshold: 80
```

Apply changes:
```bash
kubectl apply -f config.yaml
# Configuration hot reload will apply changes automatically
```

#### 2. Monitor Initialization Failure

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep "Failed to initialize monitor"
```

Common error patterns:
- `invalid config: threshold must be between 0 and 100` - Configuration validation failed
- `failed to create monitor factory` - Missing or invalid monitor type
- `monitor dependencies not met` - Prerequisites not satisfied

**Solution**:
1. Validate configuration against schema:
   ```bash
   # Extract config and validate locally
   kubectl get configmap node-doctor-config -n <namespace> -o jsonpath='{.data.config\.yaml}' > /tmp/config.yaml
   # Review for syntax errors
   yamllint /tmp/config.yaml
   ```

2. Check monitor-specific requirements:
   - **Disk Monitor**: Requires `/host` volume mount to access host filesystem
   - **Kubelet Monitor**: Requires kubelet port (default 10250) accessibility
   - **Log Pattern Monitor**: Requires valid regex patterns

3. Review monitor logs for detailed error:
   ```bash
   kubectl logs -n <namespace> <pod-name> | grep -A10 "Monitor.*failed"
   ```

#### 3. Missing RBAC Permissions

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep "forbidden"
```

**Solution**:
Verify ServiceAccount permissions:
```bash
# Check if ServiceAccount can list nodes
kubectl auth can-i list nodes --as=system:serviceaccount:<namespace>:node-doctor

# Check if ServiceAccount can create events
kubectl auth can-i create events --as=system:serviceaccount:<namespace>:node-doctor
```

Apply correct RBAC:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-doctor
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "watch", "patch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: node-doctor
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: node-doctor
subjects:
- kind: ServiceAccount
  name: node-doctor
  namespace: <namespace>
```

#### 4. Resource Constraints

**Diagnosis**:
```bash
kubectl describe pod -n <namespace> <pod-name> | grep -A5 "Limits"
kubectl top pod -n <namespace> <pod-name>
```

**Solution**:
If OOMKilled or CPU throttling detected, increase resource limits:
```yaml
resources:
  requests:
    memory: "128Mi"
    cpu: "100m"
  limits:
    memory: "512Mi"  # Increase if OOMKilled
    cpu: "500m"      # Increase if CPU throttled
```

---

### Remediation Not Triggering

**Symptoms**:
- Problems detected but not fixed
- Remediator status shows "disabled" or "failed"
- Log messages: "Remediation skipped", "Circuit breaker open"

**Possible Causes**:

#### 1. Remediator Disabled

**Diagnosis**:
```bash
kubectl get configmap node-doctor-config -n <namespace> -o jsonpath='{.data.config\.yaml}' | grep -A10 "remediators:"
```

**Solution**:
Ensure remediator is enabled:
```yaml
remediators:
  - type: service-restart
    enabled: true  # Must be true
    config:
      max_attempts: 3
      cooldown_period: 5m
```

#### 2. Circuit Breaker Open

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep "circuit breaker"
```

Circuit breaker opens after consecutive failures to prevent infinite retry loops.

**Solution**:
1. Check circuit breaker status:
   ```bash
   kubectl logs -n <namespace> <pod-name> | grep -A5 "circuit.*open"
   ```

2. Identify root cause of failures:
   ```bash
   kubectl logs -n <namespace> <pod-name> | grep "remediation failed"
   ```

3. Fix underlying issue (e.g., insufficient permissions, invalid configuration)

4. Circuit breaker will automatically close after cooldown period (default: 5 minutes)

5. To force reset (use with caution):
   ```bash
   # Restart the pod to reset circuit breaker state
   kubectl delete pod -n <namespace> <pod-name>
   # DaemonSet will recreate the pod
   ```

#### 3. Cooldown Period Active

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep "cooldown period not elapsed"
```

Cooldown prevents rapid repeated remediation attempts.

**Solution**:
This is expected behavior. The remediator will retry after cooldown expires.

Check cooldown configuration:
```yaml
remediators:
  - type: service-restart
    config:
      cooldown_period: 5m  # Adjust if too long/short
```

Recommended cooldown periods:
- Service restart: 5-10 minutes
- Node drain: 15-30 minutes
- Configuration reload: 1-2 minutes

#### 4. Rate Limit Exceeded

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep "rate limit exceeded"
```

**Solution**:
Rate limiting prevents remediation storms. Adjust if necessary:
```yaml
remediators:
  - type: service-restart
    config:
      rate_limit: 10     # Max remediations per interval
      rate_interval: 1m  # Interval duration
```

#### 5. Max Attempts Reached

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep "max attempts reached"
```

**Solution**:
After max attempts, remediation stops to prevent infinite loops.

1. Check attempt count:
   ```bash
   kubectl logs -n <namespace> <pod-name> | grep -B5 "max attempts"
   ```

2. Investigate why remediation keeps failing:
   ```bash
   kubectl logs -n <namespace> <pod-name> | grep "remediation.*failed" | tail -n 20
   ```

3. Fix root cause, then reset by restarting pod:
   ```bash
   kubectl delete pod -n <namespace> <pod-name>
   ```

#### 6. Insufficient Permissions

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep "forbidden.*remediat"
```

**Solution**:
Grant required permissions based on remediator type:

**Service Restart Remediator**:
```yaml
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "delete"]
```

**Node Drain Remediator**:
```yaml
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "patch"]
- apiGroups: [""]
  resources: ["pods/eviction"]
  verbs: ["create"]
```

**Configuration Reload Remediator**:
```yaml
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "watch"]
```

---

### High Resource Usage

**Symptoms**:
- Pod CPU/memory usage approaching limits
- OOMKilled pod restarts
- CPU throttling warnings
- Slow performance or timeouts

**Possible Causes**:

#### 1. Too Many Monitors

**Diagnosis**:
```bash
# Count active monitors
kubectl get configmap node-doctor-config -n <namespace> -o jsonpath='{.data.config\.yaml}' | grep -c "type:"

# Check resource usage
kubectl top pod -n <namespace> <pod-name>
```

**Solution**:
Each monitor consumes resources. Optimize monitor count:

1. Disable unnecessary monitors:
   ```yaml
   monitors:
     - type: cpu
       enabled: true
     - type: memory
       enabled: true
     - type: disk
       enabled: false  # Disable if not needed
   ```

2. Increase check intervals to reduce CPU usage:
   ```yaml
   monitors:
     - type: cpu
       config:
         interval: 30s  # Increase from default 10s
   ```

3. Adjust resource limits:
   ```yaml
   resources:
     requests:
       memory: "256Mi"  # Increase based on monitor count
       cpu: "200m"
     limits:
       memory: "1Gi"
       cpu: "1000m"
   ```

Resource guidelines:
- Base: 128Mi memory, 100m CPU
- Add per monitor: ~20Mi memory, ~10m CPU
- Log pattern monitor: Add extra 50Mi memory

#### 2. Excessive Logging

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> --tail=1000 | wc -l
```

If producing >100 logs/second, logging is excessive.

**Solution**:
Adjust log level:
```yaml
log_level: info  # Change from debug to info or warn
```

Reduce verbose logging:
```yaml
monitors:
  - type: cpu
    config:
      log_checks: false  # Disable per-check logging
```

#### 3. Memory Leak

**Diagnosis**:
```bash
# Watch memory usage over time
kubectl top pod -n <namespace> <pod-name> --watch

# Check for gradual memory increase pattern
```

**Solution**:
1. Restart pod to reclaim memory:
   ```bash
   kubectl delete pod -n <namespace> <pod-name>
   ```

2. If issue persists, report bug with:
   - Pod logs: `kubectl logs -n <namespace> <pod-name> > logs.txt`
   - Resource usage: `kubectl top pod -n <namespace> <pod-name>`
   - Configuration: `kubectl get configmap node-doctor-config -n <namespace> -o yaml`
   - Node Doctor version: `kubectl get daemonset -n <namespace> node-doctor -o jsonpath='{.spec.template.spec.containers[0].image}'`

#### 4. Large Problem Queue

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep "problem queue size"
```

**Solution**:
If problem queue is growing, remediators can't keep up:

1. Check remediator status:
   ```bash
   kubectl logs -n <namespace> <pod-name> | grep -A5 "remediator status"
   ```

2. Increase remediator throughput:
   ```yaml
   remediators:
     - type: service-restart
       config:
         rate_limit: 20        # Increase from default 10
         max_concurrent: 5     # Increase concurrent operations
   ```

3. Add resource limits for remediators:
   ```yaml
   resources:
     limits:
       memory: "2Gi"  # Increase for large problem queues
   ```

---

### API Connection Issues

**Symptoms**:
- Unable to list nodes
- Cannot create Kubernetes events
- Log messages: "connection refused", "timeout", "unauthorized"

**Possible Causes**:

#### 1. API Server Unreachable

**Diagnosis**:
```bash
# Test connectivity from pod
kubectl exec -n <namespace> <pod-name> -- wget -O- --timeout=5 https://kubernetes.default.svc/healthz

# Check DNS resolution
kubectl exec -n <namespace> <pod-name> -- nslookup kubernetes.default.svc
```

**Solution**:
1. Verify network policy allows egress to API server:
   ```yaml
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: node-doctor-egress
   spec:
     podSelector:
       matchLabels:
         app: node-doctor
     policyTypes:
     - Egress
     egress:
     - to:
       - namespaceSelector:
           matchLabels:
             name: kube-system
       ports:
       - protocol: TCP
         port: 443  # API server
   ```

2. Check cluster network connectivity:
   ```bash
   kubectl get pods -n kube-system -l component=kube-apiserver
   kubectl logs -n kube-system <kube-apiserver-pod>
   ```

#### 2. Authentication Failure

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep -i "unauthorized\|forbidden"
```

**Solution**:
1. Verify ServiceAccount token is mounted:
   ```bash
   kubectl exec -n <namespace> <pod-name> -- ls -la /var/run/secrets/kubernetes.io/serviceaccount/
   ```

   Expected output:
   ```
   ca.crt
   namespace
   token
   ```

2. Ensure ServiceAccount exists:
   ```bash
   kubectl get serviceaccount node-doctor -n <namespace>
   ```

3. Verify RBAC bindings:
   ```bash
   kubectl get clusterrolebinding | grep node-doctor
   ```

4. Test authentication:
   ```bash
   kubectl auth can-i list nodes --as=system:serviceaccount:<namespace>:node-doctor
   ```

#### 3. Certificate Issues

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep -i "certificate\|tls\|x509"
```

Common errors:
- `x509: certificate signed by unknown authority` - CA certificate mismatch
- `x509: certificate has expired` - Expired certificates
- `tls: failed to verify certificate` - Certificate validation failed

**Solution**:
1. Verify CA certificate is mounted:
   ```bash
   kubectl exec -n <namespace> <pod-name> -- cat /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
   ```

2. Check certificate expiration:
   ```bash
   kubectl exec -n <namespace> <pod-name> -- openssl x509 -in /var/run/secrets/kubernetes.io/serviceaccount/ca.crt -noout -dates
   ```

3. If using custom CA, ensure it's properly configured:
   ```yaml
   spec:
     containers:
     - name: node-doctor
       env:
       - name: KUBERNETES_SERVICE_HOST
         value: "custom-api-server.example.com"
       - name: KUBERNETES_SERVICE_PORT
         value: "6443"
       volumeMounts:
       - name: ca-cert
         mountPath: /etc/kubernetes/pki
         readOnly: true
   ```

#### 4. Rate Limiting

**Diagnosis**:
```bash
kubectl logs -n <namespace> <pod-name> | grep "rate limit\|429\|too many requests"
```

**Solution**:
API server is rate limiting requests. Reduce request frequency:

1. Increase monitor intervals:
   ```yaml
   monitors:
     - type: kubelet
       config:
         interval: 60s  # Increase from 30s
   ```

2. Configure client-side rate limiting:
   ```yaml
   kubernetes:
     qps: 5           # Queries per second (default: 5)
     burst: 10        # Burst allowance (default: 10)
     timeout: 30s     # Request timeout
   ```

3. Check API server rate limit configuration:
   ```bash
   kubectl describe pod -n kube-system <kube-apiserver-pod> | grep rate-limit
   ```

---

## Debugging Techniques

### Analyzing Logs

**Access Logs**:
```bash
# Recent logs (last 50 lines)
kubectl logs -n <namespace> <pod-name> --tail=50

# Follow logs in real-time
kubectl logs -n <namespace> <pod-name> --follow

# Logs from previous pod instance (after restart)
kubectl logs -n <namespace> <pod-name> --previous

# All pods in DaemonSet
kubectl logs -n <namespace> -l app=node-doctor --tail=20 --prefix=true
```

**Filter Logs by Severity**:
```bash
# Errors only
kubectl logs -n <namespace> <pod-name> | grep "ERROR"

# Warnings and errors
kubectl logs -n <namespace> <pod-name> | grep -E "ERROR|WARN"

# Specific monitor logs
kubectl logs -n <namespace> <pod-name> | grep "cpu_monitor"
```

**Common Log Patterns**:

**Successful Monitor Execution**:
```
INFO  monitor/cpu: Check completed successfully - CPU usage: 45.2% (threshold: 80%)
INFO  detector: Problem detected - type=cpu_high, severity=warning
INFO  remediator/service-restart: Remediation successful - service restarted
```

**Configuration Reload**:
```
INFO  reload: Configuration change detected - /etc/node-doctor/config.yaml
INFO  reload: Validating new configuration
INFO  reload: Stopping 5 monitors
INFO  reload: Starting 6 monitors
INFO  reload: Configuration reload completed - duration=847ms
```

**Error Patterns**:
```
ERROR monitor/kubelet: Failed to connect to kubelet API - connection refused
ERROR remediator: Circuit breaker open - too many consecutive failures (5)
ERROR detector: Failed to export problem - exporter unreachable
WARN  monitor/disk: Approaching threshold - 78% (threshold: 80%)
```

**Performance Logs**:
```bash
# Monitor execution times
kubectl logs -n <namespace> <pod-name> | grep "monitor.*duration"

# Remediation times
kubectl logs -n <namespace> <pod-name> | grep "remediation.*duration"

# Configuration reload times
kubectl logs -n <namespace> <pod-name> | grep "reload.*duration"
```

---

### Monitoring Metrics

Node Doctor exposes Prometheus metrics for observability.

**Access Metrics Endpoint**:
```bash
# Port forward to access metrics
kubectl port-forward -n <namespace> <pod-name> 9090:9090

# Fetch metrics
curl http://localhost:9090/metrics
```

**Key Metrics**:

**Monitor Metrics**:
```
# Monitor check count
node_doctor_monitor_checks_total{monitor="cpu",status="success"} 1234

# Monitor failures
node_doctor_monitor_checks_total{monitor="disk",status="failure"} 5

# Check duration
node_doctor_monitor_check_duration_seconds{monitor="memory",quantile="0.99"} 0.045
```

**Detector Metrics**:
```
# Problems detected
node_doctor_problems_detected_total{type="cpu_high",severity="warning"} 42

# Active problems
node_doctor_problems_active{type="memory_pressure"} 3
```

**Remediator Metrics**:
```
# Remediation attempts
node_doctor_remediation_attempts_total{remediator="service-restart",status="success"} 28
node_doctor_remediation_attempts_total{remediator="service-restart",status="failure"} 2

# Circuit breaker state
node_doctor_circuit_breaker_state{remediator="node-drain"} 0  # 0=closed, 1=open

# Remediation duration
node_doctor_remediation_duration_seconds{remediator="config-reload",quantile="0.95"} 1.2
```

**Configuration Reload Metrics**:
```
# Reload attempts
node_doctor_config_reload_total{status="success"} 15
node_doctor_config_reload_total{status="failure"} 1

# Reload duration
node_doctor_config_reload_duration_seconds{quantile="0.99"} 0.85
```

**Resource Metrics**:
```bash
# Pod resource usage
kubectl top pod -n <namespace> <pod-name>

# Historical resource usage (if metrics-server installed)
kubectl get --raw "/apis/metrics.k8s.io/v1beta1/namespaces/<namespace>/pods/<pod-name>" | jq .
```

**Prometheus Queries**:

If using Prometheus for monitoring:
```promql
# Monitor success rate
rate(node_doctor_monitor_checks_total{status="success"}[5m])
/
rate(node_doctor_monitor_checks_total[5m])

# Remediation success rate
sum(rate(node_doctor_remediation_attempts_total{status="success"}[5m]))
/
sum(rate(node_doctor_remediation_attempts_total[5m]))

# 95th percentile check duration
histogram_quantile(0.95, rate(node_doctor_monitor_check_duration_seconds_bucket[5m]))

# Active circuit breakers
sum(node_doctor_circuit_breaker_state) by (remediator)
```

---

### Kubernetes Events

Node Doctor emits Kubernetes events for important operations.

**View Events**:
```bash
# All events in namespace
kubectl get events -n <namespace> --sort-by='.lastTimestamp'

# Node Doctor events only
kubectl get events -n <namespace> --field-selector involvedObject.name=<pod-name>

# Configuration reload events
kubectl get events -n <namespace> | grep ConfigReload

# Specific event types
kubectl get events -n <namespace> --field-selector reason=ConfigReloadSucceeded
```

**Event Types**:

**Configuration Events**:
- `ConfigReloadSucceeded` - Configuration successfully reloaded
- `ConfigReloadFailed` - Configuration reload failed (check logs for details)
- `ConfigValidationFailed` - New configuration failed validation

**Monitor Events**:
- `MonitorStarted` - Monitor successfully initialized and running
- `MonitorStopped` - Monitor gracefully stopped
- `MonitorFailed` - Monitor encountered fatal error

**Remediation Events**:
- `RemediationSucceeded` - Problem successfully remediated
- `RemediationFailed` - Remediation attempt failed
- `CircuitBreakerOpened` - Circuit breaker opened due to failures

**Example Events**:
```bash
kubectl get events -n <namespace> -o custom-columns=TIME:.lastTimestamp,TYPE:.type,REASON:.reason,MESSAGE:.message
```

Output:
```
TIME                    TYPE      REASON                   MESSAGE
2025-11-17T10:15:23Z   Normal    ConfigReloadSucceeded    Configuration reloaded successfully in 847ms
2025-11-17T10:20:45Z   Warning   MonitorFailed            CPU monitor failed: connection timeout
2025-11-17T10:25:12Z   Normal    RemediationSucceeded     Service restarted successfully
```

**Event Filtering**:
```bash
# Only warnings and errors
kubectl get events -n <namespace> --field-selector type!=Normal

# Events in last hour
kubectl get events -n <namespace> --sort-by='.lastTimestamp' | grep "$(date -u -d '1 hour ago' '+%Y-%m-%d')"
```

---

### Health Endpoints

Node Doctor exposes health check endpoints.

**Liveness Probe**:
```bash
kubectl port-forward -n <namespace> <pod-name> 8080:8080
curl http://localhost:8080/healthz
```

Response:
```json
{
  "status": "healthy",
  "timestamp": "2025-11-17T10:30:00Z"
}
```

**Readiness Probe**:
```bash
curl http://localhost:8080/ready
```

Response:
```json
{
  "status": "ready",
  "monitors": {
    "cpu": "running",
    "memory": "running",
    "disk": "running"
  },
  "exporters": {
    "prometheus": "connected",
    "kubernetes": "connected"
  }
}
```

**Health Check Failures**:

If health check fails:
```json
{
  "status": "unhealthy",
  "reason": "api_server_unreachable",
  "timestamp": "2025-11-17T10:30:00Z"
}
```

Check pod description:
```bash
kubectl describe pod -n <namespace> <pod-name> | grep -A10 "Liveness\|Readiness"
```

---

## Performance Tuning

### Optimize Monitor Performance

**Reduce Check Frequency**:
```yaml
monitors:
  - type: cpu
    config:
      interval: 60s  # Increase from default 30s for less critical monitors
```

Guidelines:
- **Critical monitors** (CPU, Memory): 10-30s
- **Important monitors** (Disk, Network): 30-60s
- **Non-critical monitors** (Log patterns): 60-300s

**Batch Operations**:
```yaml
monitors:
  - type: kubelet
    config:
      batch_size: 10      # Check multiple resources per cycle
      batch_interval: 5s  # Delay between batches
```

**Cache API Responses**:
```yaml
kubernetes:
  cache_ttl: 30s  # Cache node list for 30 seconds
```

### Optimize Resource Usage

**Memory Optimization**:
```yaml
# Limit concurrent operations
max_concurrent_monitors: 10
max_concurrent_remediations: 5

# Reduce retention
problem_retention: 1h  # Keep problems for 1 hour (default: 24h)
```

**CPU Optimization**:
```yaml
# Reduce goroutine count
monitors:
  - type: disk
    config:
      workers: 2  # Reduce parallel workers (default: 5)
```

**I/O Optimization**:
```yaml
# Batch events
exporters:
  - type: kubernetes
    config:
      batch_events: true
      batch_size: 10
      flush_interval: 5s
```

### Tune Remediation Performance

**Adjust Rate Limits**:
```yaml
remediators:
  - type: service-restart
    config:
      rate_limit: 20       # Max operations per interval
      rate_interval: 1m    # Interval duration
      max_concurrent: 5    # Concurrent operations
```

**Optimize Cooldown Periods**:
```yaml
remediators:
  - type: service-restart
    config:
      cooldown_period: 5m   # Reduce for fast recovery
  - type: node-drain
    config:
      cooldown_period: 30m  # Increase for expensive operations
```

### Configuration Reload Performance

**Debounce Frequent Changes**:
```yaml
reload:
  debounce_interval: 5s  # Wait 5s after last change before reloading
```

**Optimize Validation**:
```yaml
reload:
  validation_timeout: 5s     # Timeout for validation (default: 10s)
  skip_dependency_check: false  # Set true to skip circular dependency check (faster but less safe)
```

---

## Security Considerations

### Pod Security Standards

**Recommended Pod Security Context**:
```yaml
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: node-doctor
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
      readOnlyRootFilesystem: true
```

**Required Capabilities**:
- None - Node Doctor runs without elevated privileges
- Uses Kubernetes API for all operations
- No direct host access required (except disk monitor)

**Volume Mounts**:
```yaml
volumeMounts:
- name: config
  mountPath: /etc/node-doctor
  readOnly: true  # Configuration should be read-only
- name: tmp
  mountPath: /tmp  # Writable temp directory
```

### Network Security

**NetworkPolicy**:
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: node-doctor
spec:
  podSelector:
    matchLabels:
      app: node-doctor
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: monitoring
    ports:
    - protocol: TCP
      port: 9090  # Metrics endpoint
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: kube-system
    ports:
    - protocol: TCP
      port: 443  # API server
  - to:
    - podSelector: {}
    ports:
    - protocol: TCP
      port: 10250  # Kubelet API
```

### RBAC Best Practices

**Principle of Least Privilege**:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-doctor
rules:
# Only required permissions
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "watch"]  # No write permissions
- apiGroups: [""]
  resources: ["nodes/status"]
  verbs: ["patch"]  # Only status updates
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]  # Event creation only
# Do NOT grant:
# - Pod deletion (unless service-restart remediator enabled)
# - Node deletion
# - ConfigMap write access
# - Secret access
```

**ServiceAccount Isolation**:
```yaml
# Use dedicated ServiceAccount per namespace
apiVersion: v1
kind: ServiceAccount
metadata:
  name: node-doctor
  namespace: production
---
# Do NOT use default ServiceAccount
```

### Configuration Security

**Secrets Management**:
```yaml
# DO NOT store secrets in ConfigMap
# Use Kubernetes Secrets for sensitive data
apiVersion: v1
kind: Secret
metadata:
  name: node-doctor-secrets
type: Opaque
data:
  api-token: <base64-encoded-token>
---
# Reference in pod spec
env:
- name: API_TOKEN
  valueFrom:
    secretKeyRef:
      name: node-doctor-secrets
      key: api-token
```

**Input Validation**:
All configuration is validated to prevent:
- Path traversal attacks
- Command injection
- Regex DoS (ReDoS)
- Circular dependencies
- Resource exhaustion

**Security Checklist**:
- [ ] Non-root container user
- [ ] Read-only root filesystem
- [ ] No privileged mode
- [ ] NetworkPolicy applied
- [ ] RBAC with least privilege
- [ ] Secrets in Kubernetes Secrets (not ConfigMap)
- [ ] TLS for external exporters
- [ ] Pod Security Standards enforced
- [ ] Regular security updates
- [ ] Audit logging enabled

---

## Frequently Asked Questions

### General Questions

**Q: What is Node Doctor?**
A: Node Doctor is a Kubernetes DaemonSet that monitors node health and automatically remediates common issues. It runs on every node in the cluster, detecting problems like high CPU usage, memory pressure, disk space issues, and failed services, then applying automated fixes based on configured remediation strategies.

**Q: How does Node Doctor differ from Kubernetes native monitoring?**
A: While Kubernetes provides basic node conditions and resource metrics, Node Doctor adds:
- **Proactive monitoring** with customizable thresholds
- **Automated remediation** instead of just detection
- **Rich problem context** with severity levels and metadata
- **Flexible exporters** for integration with existing tools
- **Hot reload** for configuration changes without pod restarts

**Q: Can I use Node Doctor in production?**
A: Yes. Node Doctor is designed for production use with:
- Comprehensive testing (97.9% test pass rate)
- Safety-first principles (circuit breakers, rate limiting)
- Zero-downtime configuration reloads
- Graceful degradation on errors
- Extensive observability (metrics, events, logs)

**Q: What Kubernetes versions are supported?**
A: Node Doctor supports Kubernetes 1.21+ and is tested against:
- Kubernetes 1.21, 1.22, 1.23, 1.24, 1.25
- EKS, GKE, AKS managed Kubernetes
- On-premises Kubernetes distributions

**Q: How do I upgrade Node Doctor?**
A:
```bash
# Update image version in DaemonSet
kubectl set image daemonset/node-doctor -n <namespace> node-doctor=supporttools/node-doctor:v1.2.0

# Verify rollout
kubectl rollout status daemonset/node-doctor -n <namespace>

# Check pods are running new version
kubectl get pods -n <namespace> -l app=node-doctor -o jsonpath='{.items[*].spec.containers[0].image}'
```

### Configuration Questions

**Q: How do I reload configuration without restarting pods?**
A: Node Doctor supports hot reload. Simply update the ConfigMap:
```bash
kubectl apply -f node-doctor-config.yaml
```
Changes are detected automatically and applied within the debounce interval (default: 5s).

**Q: Can I disable a monitor temporarily?**
A: Yes, set `enabled: false` in the monitor configuration:
```yaml
monitors:
  - type: cpu
    enabled: false  # Monitor disabled
```
Or comment out the entire monitor section.

**Q: How do I validate configuration before applying?**
A: Use the standalone validator:
```bash
# Extract current config
kubectl get configmap node-doctor-config -n <namespace> -o jsonpath='{.data.config\.yaml}' > config.yaml

# Validate locally
yamllint config.yaml

# Check for syntax errors
kubectl apply -f config.yaml --dry-run=client
```

**Q: What happens if I apply invalid configuration?**
A: The configuration reload will fail with validation errors, but:
- Existing monitors continue running with old configuration
- Kubernetes event emitted: `ConfigValidationFailed`
- Detailed error message logged
- Zero impact on monitoring

**Q: Can I have different configurations per node?**
A: Not directly. Node Doctor uses a single ConfigMap for all nodes. For node-specific configuration, use:
- Node selectors in monitor conditions
- Environment variables per node
- Multiple DaemonSets with different selectors

### Monitor Questions

**Q: How many monitors can I run simultaneously?**
A: There's no hard limit, but consider:
- **Resource usage**: Each monitor adds ~20Mi memory, ~10m CPU
- **API load**: More monitors = more API requests
- **Practical limit**: 10-20 monitors per node is typical
- **Optimization**: Increase check intervals for non-critical monitors

**Q: Can I create custom monitors?**
A: Yes. Create a custom monitor by:
1. Implementing the `Monitor` interface:
   ```go
   type Monitor interface {
       Start() (<-chan *types.Status, error)
       Stop()
   }
   ```
2. Registering with the MonitorFactory
3. Rebuilding the binary
4. See CONTRIBUTING.md for detailed guide

**Q: Why is my monitor not detecting known issues?**
A: Check:
- Monitor is enabled: `enabled: true`
- Threshold is appropriate: Too high won't trigger
- Check interval is reasonable: Too long delays detection
- Monitor has required permissions
- Monitor logs for errors: `kubectl logs <pod> | grep <monitor-type>`

**Q: How do I adjust monitor sensitivity?**
A: Modify thresholds and intervals:
```yaml
monitors:
  - type: cpu
    config:
      threshold: 70  # Lower threshold = more sensitive
      interval: 10s  # Shorter interval = faster detection
```

### Remediation Questions

**Q: What happens if remediation fails?**
A: Node Doctor implements multi-layered safety:
1. **Retry logic**: Attempts up to `max_attempts` (default: 3)
2. **Circuit breaker**: Opens after consecutive failures, prevents retry storms
3. **Rate limiting**: Prevents excessive remediation attempts
4. **Event emission**: `RemediationFailed` event created
5. **Logging**: Detailed error logged for troubleshooting

**Q: Can remediation cause service disruption?**
A: Remediation is designed to fix issues, but some actions are disruptive:
- **Service restart**: Brief service interruption (typically <30s)
- **Node drain**: Pods evicted and rescheduled (use with caution)
- **Configuration reload**: No disruption (hot reload)

Use cooldown periods and rate limits to minimize impact.

**Q: How do I prevent remediation in maintenance windows?**
A: Temporarily disable remediators:
```yaml
remediators:
  - type: service-restart
    enabled: false  # Disable during maintenance
```
Or scale down the DaemonSet:
```bash
kubectl patch daemonset node-doctor -n <namespace> -p '{"spec":{"template":{"spec":{"nodeSelector":{"maintenance":"false"}}}}}'
```

**Q: Can I test remediation without applying changes?**
A: Yes, use dry-run mode:
```yaml
remediators:
  - type: service-restart
    config:
      dry_run: true  # Log actions without executing
```
Check logs to see what would be remediated.

### Performance Questions

**Q: How much overhead does Node Doctor add?**
A: Typical resource usage:
- **Memory**: 128-256Mi (base) + 20Mi per monitor
- **CPU**: 50-100m (base) + 10m per monitor
- **Network**: Minimal (API calls for monitoring)
- **Disk**: None (no persistent storage)

**Q: Will Node Doctor impact application performance?**
A: No. Node Doctor:
- Runs with low CPU/memory limits
- Uses efficient polling intervals
- Implements rate limiting for API calls
- No impact on application pods unless remediation triggered

**Q: How can I reduce resource usage?**
A: Optimization strategies:
- Increase check intervals
- Disable unnecessary monitors
- Reduce log verbosity
- Enable API response caching
- Batch event exports

**Q: What's the performance impact of configuration reloads?**
A: Minimal. Configuration reloads:
- Complete in <1 second typically
- Monitors continue running during reload
- No dropped metrics or events
- Graceful monitor shutdown (5s timeout)

### Troubleshooting Questions

**Q: Pods stuck in CrashLoopBackOff. How do I debug?**
A:
```bash
# Check pod events
kubectl describe pod -n <namespace> <pod-name>

# Check previous logs (before crash)
kubectl logs -n <namespace> <pod-name> --previous

# Common causes:
# - Invalid configuration
# - Missing RBAC permissions
# - Resource limits too low
# - Required volumes not mounted
```

**Q: Monitors running but not detecting issues. Why?**
A:
```bash
# Check monitor status
kubectl logs -n <namespace> <pod-name> | grep "monitor.*status"

# Verify thresholds
kubectl get configmap node-doctor-config -n <namespace> -o yaml | grep threshold

# Test manually
kubectl exec -n <namespace> <pod-name> -- /bin/sh -c "cat /proc/loadavg"
```

**Q: How do I get help with issues?**
A: See [Getting Help](#getting-help) section below.

---

## Advanced Troubleshooting

### Debug Mode

Enable debug logging for detailed diagnostics:

```yaml
log_level: debug  # Change from info to debug
```

**Warning**: Debug mode produces verbose logs (>1000 lines/minute). Use temporarily for troubleshooting only.

**Debug Output Examples**:
```
DEBUG monitor/cpu: Starting check cycle
DEBUG monitor/cpu: Reading /proc/stat
DEBUG monitor/cpu: Calculating CPU usage - user=1234, system=567, idle=8901
DEBUG monitor/cpu: CPU usage: 45.2% (threshold: 80%)
DEBUG monitor/cpu: Check completed - no problems detected
```

### Enable Request Tracing

Trace API requests for network debugging:

```yaml
kubernetes:
  debug: true  # Enable request/response logging
```

Output:
```
DEBUG kubernetes: GET https://kubernetes.default.svc/api/v1/nodes
DEBUG kubernetes: Response: 200 OK (duration: 45ms)
```

### Memory Profiling

Capture memory profile for leak investigation:

```bash
# Port forward to pprof endpoint
kubectl port-forward -n <namespace> <pod-name> 6060:6060

# Capture heap profile
curl http://localhost:6060/debug/pprof/heap > heap.prof

# Analyze with go tool
go tool pprof heap.prof
```

### CPU Profiling

Capture CPU profile for performance investigation:

```bash
# Capture 30-second CPU profile
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof

# Analyze
go tool pprof cpu.prof
```

### Goroutine Dump

Capture goroutine dump for deadlock investigation:

```bash
# Dump all goroutines
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutines.txt

# Check for stuck goroutines
grep -A5 "state:" goroutines.txt
```

### Network Packet Capture

Capture network traffic for API debugging:

```bash
# Install tcpdump in pod (debug container)
kubectl debug -it <pod-name> -n <namespace> --image=nicolaka/netshoot

# In debug container
tcpdump -i eth0 -w /tmp/capture.pcap host kubernetes.default.svc

# Download capture file
kubectl cp <namespace>/<debug-pod>:/tmp/capture.pcap ./capture.pcap

# Analyze with Wireshark
wireshark capture.pcap
```

### Configuration Dump

Dump runtime configuration for verification:

```bash
# Get effective configuration
kubectl logs -n <namespace> <pod-name> | grep "Loaded configuration"

# Or via debug endpoint
curl http://localhost:8080/debug/config
```

### Strace System Calls

Trace system calls for low-level debugging:

```bash
# Attach strace to running process
kubectl debug -it <pod-name> -n <namespace> --image=nicolaka/netshoot -- \
  strace -p $(pgrep node-doctor)

# Trace specific system calls
strace -e trace=open,read,write -p $(pgrep node-doctor)
```

### Kubernetes API Audit Logs

Check API server audit logs for permission issues:

```bash
# If audit logging enabled
kubectl logs -n kube-system <kube-apiserver-pod> | \
  grep "node-doctor" | \
  grep -E "forbidden|unauthorized"
```

### Container Filesystem Inspection

Inspect container filesystem for configuration issues:

```bash
# List mounted volumes
kubectl exec -n <namespace> <pod-name> -- df -h

# Check configuration file
kubectl exec -n <namespace> <pod-name> -- cat /etc/node-doctor/config.yaml

# Check file permissions
kubectl exec -n <namespace> <pod-name> -- ls -la /etc/node-doctor
```

### Rollback Configuration

Rollback to previous working configuration:

```bash
# View ConfigMap history (if using version control)
kubectl rollout history configmap node-doctor-config -n <namespace>

# Restore previous version
kubectl apply -f node-doctor-config-v1.yaml

# Verify reload succeeded
kubectl get events -n <namespace> | grep ConfigReload
```

---

## Getting Help

### Community Support

**GitHub Issues**:
- Bug reports: https://github.com/supporttools/node-doctor/issues/new?template=bug_report.md
- Feature requests: https://github.com/supporttools/node-doctor/issues/new?template=feature_request.md
- Questions: https://github.com/supporttools/node-doctor/discussions

**Documentation**:
- Architecture: [docs/architecture.md](architecture.md)
- Monitors: [docs/monitors.md](monitors.md)
- Remediation: [docs/remediation.md](remediation.md)
- Contributing: [CONTRIBUTING.md](../CONTRIBUTING.md)

### Reporting Bugs

When reporting bugs, include:

1. **Environment**:
   ```bash
   kubectl version
   kubectl get nodes -o wide
   kubectl get daemonset node-doctor -n <namespace> -o yaml
   ```

2. **Logs** (last 500 lines):
   ```bash
   kubectl logs -n <namespace> <pod-name> --tail=500 > logs.txt
   ```

3. **Configuration**:
   ```bash
   kubectl get configmap node-doctor-config -n <namespace> -o yaml > config.yaml
   ```

4. **Events**:
   ```bash
   kubectl get events -n <namespace> --sort-by='.lastTimestamp' > events.txt
   ```

5. **Description**:
   - What were you trying to do?
   - What did you expect to happen?
   - What actually happened?
   - Can you reproduce the issue?

### Security Vulnerabilities

**DO NOT** report security vulnerabilities via public GitHub issues.

Instead, email: security@supporttools.com

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We follow responsible disclosure:
- Acknowledge receipt within 48 hours
- Provide initial assessment within 7 days
- Aim to fix critical issues within 30 days
- Coordinate public disclosure after fix is available

### Commercial Support

For enterprise support, contact: support@supporttools.com

Enterprise support includes:
- 24/7 incident response
- Dedicated Slack channel
- Custom feature development
- Training and onboarding
- SLA guarantees

---

## Appendix: Quick Reference

### Essential Commands

```bash
# Check pod status
kubectl get pods -n <namespace> -l app=node-doctor

# View recent logs
kubectl logs -n <namespace> <pod-name> --tail=50

# Follow logs in real-time
kubectl logs -n <namespace> <pod-name> --follow

# Check events
kubectl get events -n <namespace> --sort-by='.lastTimestamp'

# Verify configuration
kubectl get configmap node-doctor-config -n <namespace> -o yaml

# Reload configuration
kubectl apply -f node-doctor-config.yaml

# Check resource usage
kubectl top pod -n <namespace> <pod-name>

# Describe pod
kubectl describe pod -n <namespace> <pod-name>

# Access metrics
kubectl port-forward -n <namespace> <pod-name> 9090:9090
curl http://localhost:9090/metrics

# Restart pod
kubectl delete pod -n <namespace> <pod-name>
```

### Configuration Templates

**Minimal Configuration**:
```yaml
monitors:
  - type: cpu
    enabled: true
    config:
      threshold: 80
      interval: 30s

exporters:
  - type: kubernetes
    enabled: true
```

**Production Configuration**:
```yaml
log_level: info

monitors:
  - type: cpu
    enabled: true
    config:
      threshold: 80
      interval: 30s
  - type: memory
    enabled: true
    config:
      threshold: 85
      interval: 30s
  - type: disk
    enabled: true
    config:
      threshold: 90
      interval: 60s

remediators:
  - type: service-restart
    enabled: true
    config:
      max_attempts: 3
      cooldown_period: 5m
      rate_limit: 10
      rate_interval: 1m

exporters:
  - type: kubernetes
    enabled: true
  - type: prometheus
    enabled: true
    config:
      port: 9090

reload:
  debounce_interval: 5s
```

### Common Error Messages

| Error Message | Cause | Solution |
|--------------|-------|----------|
| `monitor not found` | Invalid monitor type | Check monitor type spelling |
| `threshold must be between 0 and 100` | Invalid threshold value | Set threshold 0-100 |
| `forbidden: nodes` | Missing RBAC permissions | Apply ClusterRole |
| `connection refused` | API server unreachable | Check network connectivity |
| `circuit breaker open` | Too many failures | Fix underlying issue, wait for cooldown |
| `rate limit exceeded` | Too many requests | Increase intervals, reduce monitors |
| `max attempts reached` | Remediation keeps failing | Investigate root cause |
| `configuration validation failed` | Invalid config syntax | Check YAML syntax, review logs |

### Useful Links

- **GitHub Repository**: https://github.com/supporttools/node-doctor
- **Documentation**: https://github.com/supporttools/node-doctor/tree/main/docs
- **Issue Tracker**: https://github.com/supporttools/node-doctor/issues
- **Discussions**: https://github.com/supporttools/node-doctor/discussions
- **Releases**: https://github.com/supporttools/node-doctor/releases
- **Docker Images**: https://hub.docker.com/r/supporttools/node-doctor

---

**Last Updated**: 2025-11-17
**Version**: 1.0.0
**Maintainer**: SupportTools Team
