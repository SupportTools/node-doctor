# DNS Monitor — Troubleshooting Guide

This guide covers common DNS failure patterns and how Node Doctor's advanced monitoring layers
detect and help diagnose them.

## Table of Contents

- [Reading Node Doctor DNS Conditions](#reading-node-doctor-dns-conditions)
- [Scenario 1: Single Nameserver Failure](#scenario-1-single-nameserver-failure)
- [Scenario 2: Gradual DNS Degradation](#scenario-2-gradual-dns-degradation)
- [Scenario 3: Intermittent Flapping](#scenario-3-intermittent-flapping)
- [Scenario 4: Cluster-Wide DNS Outage](#scenario-4-cluster-wide-dns-outage)
- [Scenario 5: Split-Brain DNS (External vs. Cluster)](#scenario-5-split-brain-dns-external-vs-cluster)
- [Scenario 6: CoreDNS Restart Loop](#scenario-6-coredns-restart-loop)
- [Debugging Tips](#debugging-tips)
- [Condition Quick Reference](#condition-quick-reference)

---

## Reading Node Doctor DNS Conditions

Node Doctor writes DNS health conditions to the Kubernetes node object under
`.status.conditions`. You can inspect them with:

```bash
kubectl get node <node-name> -o jsonpath='{.status.conditions[?(@.type=~"DNS.*")]}' | jq .
```

Or use the Node Doctor HTTP API:

```bash
curl -s http://<node-ip>:8080/status | jq '.conditions[] | select(.type | startswith("DNS"))'
```

Each condition has:
- `type`: The condition name (e.g., `ClusterDNSDegrading`)
- `status`: `"True"` (active problem) or `"False"` (resolved)
- `reason`: Machine-readable reason code
- `message`: Human-readable description with numeric details

---

## Scenario 1: Single Nameserver Failure

**Symptoms:**
- Pods on the node intermittently fail to resolve service names
- Some DNS queries succeed (other nameservers are healthy)
- `DNSNameserverUnhealthy` or `DNSNameserverDegraded` condition present

**What Node Doctor sees:**

With `healthScoring` enabled, Node Doctor tracks each nameserver individually. A failing nameserver
will show a low composite score:

```
Condition: DNSNameserverUnhealthy
Reason:    NameserverUnhealthy
Message:   DNS nameserver 10.96.0.11 health score: 23/100
           (success_rate=0.42 latency_p95=1.8s error_types=[SERVFAIL, Timeout])
```

**Investigation steps:**

```bash
# Check which CoreDNS pods are running
kubectl get pods -n kube-system -l k8s-app=kube-dns -o wide

# Test the specific failing nameserver directly
dig @10.96.0.11 kubernetes.default.svc.cluster.local

# Check CoreDNS logs for the failing pod
kubectl logs -n kube-system <failing-coredns-pod> --since=10m
```

**Resolution:** Restart the failing CoreDNS pod. If the pod keeps failing, check the underlying
node for resource pressure (`kubectl top node`).

---

## Scenario 2: Gradual DNS Degradation

**Symptoms:**
- DNS success rate slowly declining over 15–30 minutes
- No immediate hard failure, but trend is clearly downward
- `ClusterDNSDegrading` condition appears before failure threshold is breached
- `DNSPredictedDegradation` may appear even earlier if predictive alerting is enabled

**What Node Doctor sees:**

Trend detection catches this pattern via the OLS slope:

```
Condition: DNSDegrading
Reason:    ClusterDNSDegrading
Message:   Cluster DNS success rate is trending downward
           (slope=-0.0621/sample, threshold=-0.0500/sample)
```

If predictive alerting is enabled, you may see this first:

```
Condition: DNSPredictedDegradation
Reason:    ClusterDNSBreach
Message:   Cluster DNS success rate predicted to breach failure threshold in 18m42s
           (R²=0.91, current rate=91.2%, projected breach at 14:47:22)
```

**Investigation steps:**

```bash
# Check cluster-wide DNS success rate trend
kubectl get nodes -o custom-columns='NODE:.metadata.name,DNS_CONDITION:.status.conditions[?(@.type=="ClusterDNSDegrading")].status'

# Is this node-specific or cluster-wide?
# If multiple nodes show DNSDegrading, the issue is upstream (CoreDNS, upstream resolver)
# If only one node shows it, the issue may be the node's network stack or iptables rules

# Check iptables rules for DNS (kube-proxy writes these)
iptables -t nat -L KUBE-SERVICES | grep dns

# Check if CoreDNS is under load
kubectl top pods -n kube-system -l k8s-app=kube-dns
```

**Common causes:**
- CoreDNS memory pressure (OOMKilled, slow GC)
- Upstream resolver latency increase (external DNS path)
- iptables rules corruption on the node
- High pod churn causing DNS resolution bursts

---

## Scenario 3: Intermittent Flapping

**Symptoms:**
- DNS sometimes works, sometimes fails, with no consistent pattern
- Average success rate looks acceptable (e.g., 75%), but pods are unreliable
- `ClusterDNSFlapping` or `ExternalDNSFlapping` condition present

**What Node Doctor sees:**

```
Condition: DNSFlapping
Reason:    ClusterDNSFlapping
Message:   Cluster DNS is flapping: 5 healthy↔unhealthy transitions in the detection window
```

The z-score anomaly detector may also fire because high variance in success rates pushes the
current rate away from the long-term mean:

```
Condition: DNSAnomalous
Reason:    ClusterDNSAnomalous
Message:   Cluster DNS success rate is anomalous: z-score=-2.71
           (current 62.0% is below the 88.5% long-term mean)
```

**Investigation steps:**

```bash
# Check for sporadic CoreDNS restarts
kubectl get events -n kube-system --field-selector=involvedObject.name=kube-dns --sort-by='.lastTimestamp'

# Look for DNS cache thrashing
kubectl logs -n kube-system -l k8s-app=kube-dns --since=30m | grep -c "SERVFAIL\|refused\|timeout"

# Check conntrack table fullness (common cause of intermittent DNS failures)
cat /proc/sys/net/netfilter/nf_conntrack_count
cat /proc/sys/net/netfilter/nf_conntrack_max
```

**Common causes:**
- Conntrack table exhaustion causing random connection drops
- Race condition in iptables rules during pod churn
- CoreDNS eviction and restart cycles
- Network policy changes with incomplete rollout

---

## Scenario 4: Cluster-Wide DNS Outage

**Symptoms:**
- All DNS resolution fails simultaneously across many domains
- Both cluster and external DNS conditions firing on multiple nodes
- Correlation analysis reports `DNSNameserverCorrelation` with high confidence

**What Node Doctor sees:**

```
Condition: DNSNameserverCorrelation
Reason:    NameserverCorrelation
Message:   Correlated DNS failures detected (confidence=0.97): nameservers [10.96.0.10, 10.96.0.11]
           — RootCause: Multiple nameservers failing simultaneously.
             Possible upstream infrastructure issue.
           — Evidence: nameserver 10.96.0.10 success_rate=0.08, nameserver 10.96.0.11 success_rate=0.11
```

**Investigation steps:**

```bash
# Are all CoreDNS pods down?
kubectl get pods -n kube-system -l k8s-app=kube-dns

# Check if kube-dns service has endpoints
kubectl get endpoints -n kube-system kube-dns

# Can any pod reach the DNS service IP?
kubectl run -it dns-debug --rm --image=busybox --restart=Never -- nslookup kubernetes

# Check cluster networking
kubectl get nodes -o wide | head -5

# Has there been a recent kube-proxy or CNI change?
kubectl get events --all-namespaces --sort-by='.lastTimestamp' | tail -30
```

**Resolution:** Check CoreDNS deployment status. If all replicas are down, force-delete and let
the deployment controller recreate them. Check the kube-dns service IP and ClusterIP allocation.

---

## Scenario 5: Split-Brain DNS (External vs. Cluster)

**Symptoms:**
- Cluster DNS resolves service names correctly
- External DNS (google.com, etc.) fails
- Or vice versa: external DNS fine, cluster DNS broken

**What Node Doctor sees:**

With separate tracking for cluster and external domains:

```
# ClusterDNSDegraded check produces no active condition (status: False)
# ExternalDNSDegraded check fires (status: True)
```

The trend detector distinguishes between cluster and external paths:

```
Condition: DNSDegrading
Reason:    ExternalDNSDegrading
Message:   External DNS success rate is trending downward (slope=-0.08/sample, threshold=-0.05/sample)
```

**Investigation steps:**

```bash
# Test cluster DNS directly
dig @10.96.0.10 kubernetes.default.svc.cluster.local

# Test external DNS with the upstream resolver CoreDNS uses
dig @8.8.8.8 google.com

# Check CoreDNS forwarding configuration
kubectl get configmap -n kube-system coredns -o yaml

# Check if the node can reach external resolvers
curl -I --connect-timeout 5 https://8.8.8.8

# Check network policies that might block egress DNS
kubectl get networkpolicies --all-namespaces
```

**Common causes:**
- Egress firewall rule blocking port 53/UDP to external resolvers
- CoreDNS forward plugin misconfiguration
- Node's upstream resolver in `/etc/resolv.conf` is unreachable
- Split-horizon DNS configuration issue

---

## Scenario 6: CoreDNS Restart Loop

**Symptoms:**
- DNS works briefly, then fails, then recovers — in a repeating cycle
- `ClusterDNSFlapping` with regularly-timed oscillations
- Correlation analysis may show temporal correlation

**What Node Doctor sees:**

```
Condition: DNSFlapping
Reason:    ClusterDNSFlapping
Message:   Cluster DNS is flapping: 8 healthy↔unhealthy transitions in the detection window

Condition: DNSTemporalCorrelation
Reason:    TemporalCorrelation
Message:   Temporal DNS failure burst detected (confidence=0.88):
           12 failures in 5m window vs. 3 failures outside window
```

**Investigation steps:**

```bash
# Check restart count on CoreDNS pods
kubectl get pods -n kube-system -l k8s-app=kube-dns -o wide
kubectl describe pod -n kube-system <coredns-pod> | grep -A5 "Last State"

# What is causing the OOM/crash?
kubectl top pods -n kube-system -l k8s-app=kube-dns
kubectl describe pod -n kube-system <coredns-pod> | grep -A10 "Events:"

# Check CoreDNS memory limits
kubectl get deployment -n kube-system coredns -o jsonpath='{.spec.template.spec.containers[*].resources}'
```

**Resolution:** Increase CoreDNS memory limits or add a `cache` plugin directive with a shorter
TTL to reduce cache memory usage. Check for DNS amplification patterns in CoreDNS logs.

---

## Debugging Tips

### Enable verbose DNS check logging

Set the monitor log level to debug to see per-query details:

```yaml
monitors:
  - name: dns-health
    type: network-dns-check
    config:
      logLevel: debug   # Shows each query result
```

### Inspect current trend state

The trend detector's internal state can be inferred from condition messages. The `slope`,
`threshold`, `z-score`, and `oscillations` values are all included in the condition message text.

### Adjust detection sensitivity

| Too many false positives | Too many missed failures |
|--------------------------|--------------------------|
| Increase `anomalyZScore` | Decrease `anomalyZScore` |
| Increase `windowSize` (smoother slope) | Decrease `windowSize` |
| Increase `degradationThreshold` closer to 0 | Decrease `degradationThreshold` further negative |
| Increase `minOscillations` | Decrease `minOscillations` |
| Increase `confidenceThreshold` (predictive) | Decrease `confidenceThreshold` |

### Run a quick connectivity test from the node

```bash
# On the node itself
nsenter -t $(pidof kubelet) -n -- bash -c "
  for i in 1 2 3 4 5; do
    nslookup kubernetes.default.svc.cluster.local 10.96.0.10 2>&1 | grep -E 'Address|SERVFAIL'
    sleep 1
  done
"
```

---

## Condition Quick Reference

| Condition | Layer | Check First |
|-----------|-------|------------|
| `DNSNameserverUnhealthy` | Health Scoring | `kubectl logs -n kube-system <coredns-pod>` |
| `DNSNameserverDegraded` | Health Scoring | `kubectl top pods -n kube-system -l k8s-app=kube-dns` |
| `DNSNameserverCorrelation` | Correlation | All CoreDNS pods status, network connectivity |
| `DNSDomainPatternCorrelation` | Correlation | Specific failing domain DNS record, CoreDNS configmap |
| `DNSTemporalCorrelation` | Correlation | Recent configuration changes, network events in the time window |
| `DNSPredictedDegradation` | Predictive | CoreDNS resource usage trend, upstream resolver latency |
| `DNSDegrading` (Reason: `ClusterDNSDegrading`) | Trend | CoreDNS pod health, iptables rules |
| `DNSDegrading` (Reason: `ExternalDNSDegrading`) | Trend | Egress firewall rules, upstream resolver reachability |
| `DNSAnomalous` (Reason: `ClusterDNSAnomalous`) | Trend | Recent configuration changes, burst traffic patterns |
| `DNSFlapping` (Reason: `ClusterDNSFlapping`) | Trend | CoreDNS restart count, conntrack table usage |
| `DNSFlapping` (Reason: `ExternalDNSFlapping`) | Trend | Upstream resolver stability, network link quality |
