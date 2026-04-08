# DNS Monitor — Advanced Features Guide

This guide covers the five opt-in analytical layers built on top of Node Doctor's core DNS monitor.
Each layer is independently toggled via its own `enabled` flag; none are required for basic DNS
health checking.

## Table of Contents

- [Overview](#overview)
- [1. DNS Health Scoring](#1-dns-health-scoring)
- [2. Correlation Analysis](#2-correlation-analysis)
- [3. Predictive Alerting](#3-predictive-alerting)
- [4. Statistical Trend Detection](#4-statistical-trend-detection)
- [5. Enabling All Layers Together](#5-enabling-all-layers-together)
- [Conditions Reference](#conditions-reference)
- [Prometheus Metrics](#prometheus-metrics)
- [Configuration Reference](#configuration-reference)

---

## Overview

The DNS monitor emits conditions to the node's status on every check cycle. Basic monitoring
detects binary pass/fail and failure-rate thresholds. The advanced layers add:

| Layer | What it detects | Condition names emitted |
|-------|----------------|------------------------|
| Health Scoring | Per-nameserver composite score (success rate, latency, error diversity, consistency) | `DNSNameserverUnhealthy`, `DNSNameserverDegraded` |
| Correlation Analysis | Shared failure patterns across nameservers and domains | `DNSNameserverCorrelation`, `DNSDomainPatternCorrelation`, `DNSTemporalCorrelation` |
| Predictive Alerting | Projected breach of failure threshold within a configurable window | `DNSPredictedDegradation` |
| Trend Detection | Gradual decline (slope), statistical anomaly (z-score), and intermittent flapping | `DNSDegrading`, `DNSAnomalous`, `DNSFlapping` |

All layers operate on the same check cycle and write into the same `Status` object, so their
conditions naturally aggregate in Kubernetes node events, Prometheus, and alert routing.

---

## 1. DNS Health Scoring

### What It Does

Instead of a binary healthy/unhealthy verdict per nameserver, health scoring computes a composite
score from 0–100 using four weighted dimensions:

| Dimension | Default Weight | What It Captures |
|-----------|---------------|-----------------|
| Success rate | 40% | Percentage of checks that resolved successfully |
| Latency | 25% | p95 latency vs. configured baseline and maximum |
| Error diversity | 15% | Spread across error types (NXDOMAIN, SERVFAIL, Timeout…) |
| Consistency | 20% | Variance in success rate (low variance = high consistency) |

Scores below the `degradedThreshold` (default 70) emit `DNSNameserverDegraded`.
Scores below the `unhealthyThreshold` (default 40) emit `DNSNameserverUnhealthy`.

### Scoring Algorithm

**Latency component** — clamped linear interpolation between baseline and maximum:

```
latencyScore = 100 × clamp((latencyMax - p95) / (latencyMax - latencyBaseline), 0, 1)
```

- At or below `latencyBaseline` (default 50 ms) → score 100
- At or above `latencyMax` (default 2 s) → score 0

**Error diversity component** — penalises spreading failures across multiple error types:

```
diversityScore = 100 × (1 - (uniqueErrorTypes / maxErrorTypes))
```

**Composite score** — weighted sum of the four components:

```
score = successWeight×successScore + latencyWeight×latencyScore
      + diversityWeight×diversityScore + consistencyWeight×consistencyScore
```

### Configuration

```yaml
monitors:
  - name: dns-health
    type: network-dns-check
    config:
      healthScoring:
        enabled: true
        degradedThreshold: 70       # Score below this → DNSNameserverDegraded
        unhealthyThreshold: 40      # Score below this → DNSNameserverUnhealthy
        windowSize: 20              # Number of checks to retain per nameserver
        latencyBaseline: 50ms       # Healthy p95 latency target
        latencyMax: 2s              # Latency at which latency score = 0
        successRateWeight: 0.40     # Must sum to 1.0 across all four weights
        latencyWeight: 0.25
        errorDiversityWeight: 0.15
        consistencyWeight: 0.20
```

### Prometheus Metrics

When health scoring is enabled, per-nameserver scores are exported as Prometheus gauges via the
`latencyMetrics.NameserverHealthScores` field that the DNS monitor attaches to each status update.

The score can be used in Grafana dashboards and alerting rules:

```promql
# Alert when any nameserver score drops below 50
node_doctor_dns_nameserver_health_score{node=~".*"} < 50
```

---

## 2. Correlation Analysis

### What It Does

The correlation engine examines all nameservers and domains collectively each check cycle to detect
shared failure patterns. It runs three independent detectors:

| Detector | Hypothesis | Confidence basis |
|----------|-----------|-----------------|
| **Nameserver correlation** | Multiple nameservers failing simultaneously → shared upstream infrastructure issue | Fraction of nameservers below failure threshold |
| **Domain-pattern correlation** | Same domain failing across multiple nameservers → DNS record issue (NXDOMAIN, stale TTL) | Number of nameservers that failed the domain |
| **Temporal correlation** | Failures cluster within a short time window → transient infrastructure event | Ratio of failures within the window vs. total |

Results above the configured `minConfidence` threshold are emitted as status conditions with a
human-readable `RootCause` hypothesis and supporting `Evidence`.

### Configuration

```yaml
monitors:
  - name: dns-health
    type: network-dns-check
    config:
      correlation:
        enabled: true
        minConfidence: 0.7          # 0.0–1.0; only report correlations above this
        windowMinutes: 5            # Temporal detector look-back window
        nameserverFailureThreshold: 0.5   # Below this success rate → nameserver "failing"
        minNameserversForDomainCorrelation: 2  # Minimum nameservers that must fail a domain
```

### Interpreting Results

| Condition | Reason | Meaning |
|-----------|--------|---------|
| `DNSNameserverCorrelation` | `NameserverCorrelation` | Multiple nameservers failing; likely shared upstream |
| `DNSDomainPatternCorrelation` | `DomainPatternCorrelation` | Same domain failing on many nameservers; record issue |
| `DNSTemporalCorrelation` | `TemporalCorrelation` | Burst of failures in a short window; transient event |
| `DNSCorrelation` | (fallback) | Unclassified correlation type |

The condition message includes the affected items and confidence score:

```
Correlated DNS failures detected (confidence=0.85): nameservers [10.96.0.10, 10.96.0.11]
— RootCause: Multiple nameservers failing simultaneously. Possible upstream infrastructure issue.
```

### Tuning Tips

- **Lower `minConfidence`** to catch weaker correlations (more noise, earlier detection).
- **Raise `windowMinutes`** to correlate failures that span longer intervals (e.g., rolling restarts).
- **Raise `minNameserversForDomainCorrelation`** in large clusters where some nameserver failures are normal.

---

## 3. Predictive Alerting

### What It Does

Predictive alerting fits a linear regression line to the recent DNS success-rate ring buffer and
extrapolates when the rate will breach the configured failure threshold. If the projected breach
falls within the `predictionWindow`, a `DNSPredictedDegradation` condition is emitted **before**
the failure threshold is actually crossed.

The alert distinguishes two urgency levels:

| Urgency | Condition message contains | Breach timing |
|---------|--------------------------|--------------|
| Warning | "within lead time" | Breach predicted within `warningLeadTime` |
| Watch | "within prediction window" | Breach predicted within `predictionWindow` but outside `warningLeadTime` |

### Configuration

```yaml
monitors:
  - name: dns-health
    type: network-dns-check
    config:
      predictiveAlerting:
        enabled: true
        predictionWindow: 30m       # Look this far ahead for a predicted breach
        warningLeadTime: 15m        # Escalate urgency if breach is within this window
        minDataPoints: 10           # Minimum ring-buffer samples before predicting
        confidenceThreshold: 0.8    # Minimum R² required to trust the regression
```

### How the Prediction Works

1. All success-rate samples in the ring buffer are fitted with ordinary least-squares regression,
   using Unix timestamp as the x-axis (seconds since epoch) and success rate (0.0–1.0) as y.
2. R² (coefficient of determination) is computed. If R² < `confidenceThreshold`, no alert is
   generated — the trend is too noisy to act on.
3. The regression line is extrapolated to find when it crosses the `failureRateThreshold`
   configured in `successRateTracking`.
4. If the projected breach time falls within `predictionWindow`, the condition is emitted.

### Interpreting Results

A condition message looks like:

```
Cluster DNS success rate predicted to breach failure threshold in 12m30s
(R²=0.94, current rate=87.3%, slope=-0.28%/min, projected breach at 14:32:15)
```

**False positives** occur when DNS has a brief dip but quickly recovers. Raise
`confidenceThreshold` (e.g., 0.9) and `minDataPoints` (e.g., 20) to require a more sustained
trend before alerting.

---

## 4. Statistical Trend Detection

### What It Does

Trend detection analyses the stream of per-cycle DNS success rates using two independent
statistical models:

1. **Welford's online algorithm** — computes a running mean and variance over *all* observations
   ever seen (unbounded history). This gives a stable long-term baseline for detecting short-term
   anomalies via z-score, even after thousands of check cycles.

2. **OLS linear regression** — fits a slope to the most recent `windowSize` observations (circular
   buffer). The slope (success-rate change per sample) detects gradual degradation trends.

3. **Flap detection** — counts healthy↔unhealthy transitions within a rolling time window to catch
   intermittent failures that have high average success rates but erratic behaviour.

### Conditions Emitted

| Condition type | Triggered when | Reason field (in Kubernetes node condition) |
|----------------|---------------|---------------------------------------------|
| `DNSDegrading` | OLS slope more negative than `degradationThreshold` | `ClusterDNSDegrading` or `ExternalDNSDegrading` |
| `DNSAnomalous` | \|z-score\| exceeds `anomalyZScore` | `ClusterDNSAnomalous` or `ExternalDNSAnomalous` |
| `DNSFlapping` | Oscillation count ≥ `minOscillations` in the time window | `ClusterDNSFlapping` or `ExternalDNSFlapping` |

### Configuration

```yaml
monitors:
  - name: dns-health
    type: network-dns-check
    config:
      trendDetection:
        enabled: true
        windowSize: 20              # Circular buffer size for slope/flap calculations
        degradationThreshold: -0.05 # Slope more negative than this → DNSDegrading
                                    # Units: success-rate change per sample (negative = declining)
        anomalyZScore: 2.5          # |z-score| above this → DNSAnomalous
        flapDetection:
          enabled: true
          minOscillations: 3        # Transitions needed to trigger DNSFlapping
          windowMinutes: 10         # Rolling window for counting transitions
```

### Understanding the Metrics

**Slope (degradation)** — A slope of `-0.05` means the success rate drops 5 percentage points per
check cycle. With a 30-second interval that is approximately -10%/minute. The default threshold
`-0.05` flags trends that would reach 0% success in ~20 cycles from a healthy 100% baseline.

**Z-score (anomaly)** — Measures how far the current sample is from the long-term mean in standard
deviations. A z-score of -2.5 means the current rate is 2.5σ below normal. Negative z-scores
indicate the current rate is *worse* than historical average (bad). Positive z-scores indicate the
current rate is *better* than average (typically not alarming).

**Oscillations (flapping)** — Each healthy→unhealthy or unhealthy→healthy transition within the
flap window increments the counter. Three or more transitions in 10 minutes indicates an
intermittent DNS failure that may not trigger the sustained failure-rate threshold.

### Tuning Tips

| Scenario | Recommendation |
|---------|---------------|
| Too many `DNSDegrading` alerts during legitimate load | Increase `windowSize` (more samples → smoother slope) |
| Missing slow gradual degradation | Decrease `degradationThreshold` (e.g., `-0.02`) |
| High z-score alerts on noisy environments | Increase `anomalyZScore` (e.g., 3.0) |
| Flapping alerts on expected periodic restarts | Increase `windowMinutes` or `minOscillations` |

---

## 5. Enabling All Layers Together

The following configuration enables all four advanced layers with recommended production defaults:

```yaml
monitors:
  - name: dns-health
    type: network-dns-check
    interval: 30s
    timeout: 10s
    config:
      clusterDomains:
        - kubernetes.default.svc.cluster.local
        - kube-dns.kube-system.svc.cluster.local
      externalDomains:
        - google.com
        - cloudflare.com

      successRateTracking:
        enabled: true
        windowSize: 50
        failureRateThreshold: 0.1   # Alert at 10% failure rate
        minSamplesRequired: 20

      # Layer 1: Per-nameserver composite scoring
      healthScoring:
        enabled: true
        degradedThreshold: 70
        unhealthyThreshold: 40
        latencyBaseline: 50ms
        latencyMax: 2s

      # Layer 2: Cross-domain and cross-nameserver correlation
      correlation:
        enabled: true
        minConfidence: 0.75
        windowMinutes: 5
        nameserverFailureThreshold: 0.5

      # Layer 3: Predictive breach alerting
      predictiveAlerting:
        enabled: true
        predictionWindow: 30m
        warningLeadTime: 15m
        minDataPoints: 15
        confidenceThreshold: 0.85

      # Layer 4: Statistical trend detection
      trendDetection:
        enabled: true
        windowSize: 20
        degradationThreshold: -0.05
        anomalyZScore: 2.5
        flapDetection:
          enabled: true
          minOscillations: 3
          windowMinutes: 10
```

---

## Conditions Reference

| Condition Name | Layer | Severity | Description |
|----------------|-------|----------|-------------|
| `DNSNameserverUnhealthy` | Health Scoring | Critical | Nameserver composite score < unhealthyThreshold |
| `DNSNameserverDegraded` | Health Scoring | Warning | Nameserver composite score < degradedThreshold |
| `DNSNameserverCorrelation` | Correlation | Warning | Multiple nameservers failing simultaneously |
| `DNSDomainPatternCorrelation` | Correlation | Warning | Same domain failing across multiple nameservers |
| `DNSTemporalCorrelation` | Correlation | Warning | Failures clustered within the temporal detection window |
| `DNSCorrelation` | Correlation | Warning | Fallback for unclassified correlation types |
| `DNSPredictedDegradation` | Predictive | Warning | Projected breach of failure threshold |
| `DNSDegrading` | Trend Detection | Warning | Success rate trending downward (Reason: `ClusterDNSDegrading` or `ExternalDNSDegrading`) |
| `DNSAnomalous` | Trend Detection | Warning | Success rate is a statistical anomaly (Reason: `ClusterDNSAnomalous` or `ExternalDNSAnomalous`) |
| `DNSFlapping` | Trend Detection | Warning | Intermittent healthy/unhealthy transitions (Reason: `ClusterDNSFlapping` or `ExternalDNSFlapping`) |

---

## Prometheus Metrics

Node Doctor exports DNS health metrics for use in Grafana dashboards and recording rules.

### Key Metrics

| Metric | Labels | Description |
|--------|--------|-------------|
| `node_doctor_dns_health_score` | `node`, `nameserver` | Composite health score (0–100) |
| `node_doctor_dns_success_rate` | `node`, `type` (cluster/external) | Rolling success rate |
| `node_doctor_dns_latency_p95_seconds` | `node`, `nameserver` | p95 resolution latency |

### Example Grafana Queries

```promql
# DNS health score heatmap across all nodes
node_doctor_dns_health_score

# Nodes with degraded cluster DNS
node_doctor_dns_success_rate{type="cluster"} < 0.9

# Predict DNS issues before they breach threshold (predictive alerting complement)
predict_linear(node_doctor_dns_success_rate{type="cluster"}[10m], 1800) < 0.9
```

### Example AlertManager Rules

```yaml
groups:
  - name: node-doctor-dns
    rules:
      - alert: DNSNameserverUnhealthy
        expr: |
          kube_node_status_condition{
            condition=~"DNSNameserverUnhealthy",
            status="True"
          } == 1
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "DNS nameserver health score critically low on {{ $labels.node }}"

      - alert: DNSPredictedDegradation
        expr: |
          kube_node_status_condition{
            condition="DNSPredictedDegradation",
            status="True"
          } == 1
        for: 0m
        labels:
          severity: warning
        annotations:
          summary: "DNS degradation predicted on {{ $labels.node }}"

      - alert: DNSFlapping
        expr: |
          kube_node_status_condition{
            condition="DNSFlapping",
            status="True"
          } == 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "DNS flapping detected on {{ $labels.node }}"
```

---

## Configuration Reference

### `healthScoring`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Activate health scoring |
| `degradedThreshold` | float | `70` | Score below which a nameserver is degraded |
| `unhealthyThreshold` | float | `40` | Score below which a nameserver is unhealthy |
| `windowSize` | int | `20` | Sliding window size (number of checks) |
| `latencyBaseline` | duration | `50ms` | p95 latency for a score of 100 |
| `latencyMax` | duration | `2s` | p95 latency for a score of 0 |
| `successRateWeight` | float | `0.40` | Weight for success rate component |
| `latencyWeight` | float | `0.25` | Weight for latency component |
| `errorDiversityWeight` | float | `0.15` | Weight for error diversity component |
| `consistencyWeight` | float | `0.20` | Weight for consistency component |

### `correlation`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Activate correlation analysis |
| `minConfidence` | float | `0.7` | Minimum confidence (0–1) to emit a condition |
| `windowMinutes` | int | `5` | Temporal detector look-back window |
| `nameserverFailureThreshold` | float | `0.5` | Success rate below which a nameserver is "failing" |
| `minNameserversForDomainCorrelation` | int | `2` | Min nameservers that must fail a domain |

### `predictiveAlerting`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Activate predictive alerting |
| `predictionWindow` | duration | `30m` | How far ahead to look for a predicted breach |
| `warningLeadTime` | duration | `15m` | Breach within this window → escalate urgency |
| `minDataPoints` | int | `10` | Minimum samples before predicting |
| `confidenceThreshold` | float | `0.8` | Minimum R² to trust the regression |

### `trendDetection`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Activate statistical trend detection |
| `windowSize` | int | `20` | Circular buffer size for slope and flap calculations |
| `degradationThreshold` | float | `-0.05` | OLS slope more negative than this → Degrading |
| `anomalyZScore` | float | `2.5` | Absolute z-score above this → Anomalous |
| `flapDetection.enabled` | bool | `true` (when block present) | Activate flap detection |
| `flapDetection.minOscillations` | int | `3` | Transitions needed to trigger Flapping |
| `flapDetection.windowMinutes` | int | `10` | Rolling window for counting transitions |
