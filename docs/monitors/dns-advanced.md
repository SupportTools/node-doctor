# DNS monitor: advanced features

The `network-dns-check` monitor has seven opt-in blocks that add analysis on top of the basic pass/fail checks. Each block is off until you set `enabled: true` in it. The core keys and core conditions are in [monitors.md](../monitors.md#network-dns-check).

| Block | Detects | Conditions | Needs |
|---|---|---|---|
| [`successRateTracking`](#successratetracking) | Failure rate over a sliding window | `ClusterDNSDegraded`, `ClusterDNSIntermittent`, `ExternalDNSDegraded`, `ExternalDNSIntermittent` | |
| [`consistencyChecking`](#consistencychecking) | Flaky or varying answers to rapid repeated queries | `DNSResolutionDown`, `DNSResolutionIntermittent`, `DNSResolutionInconsistent` | `customQueries` with `consistencyCheck: true` |
| [`healthScoring`](#healthscoring) | A 0-100 score per nameserver | `DNSNameserverDegraded`, `DNSNameserverUnhealthy` | |
| [`correlation`](#correlation) | Failures shared across nameservers or domains | `DNSNameserverCorrelation`, `DNSDomainPatternCorrelation`, `DNSTemporalCorrelation` | `healthScoring`, or `testEachNameserver` queries |
| [`predictiveAlerting`](#predictivealerting) | A failure-rate breach before it happens | `DNSPredictedDegradation` | |
| [`trendDetection`](#trenddetection) | Slow decline, outliers, flapping | `DNSDegrading`, `DNSAnomalous`, `DNSFlapping` | |
| [`historicalMetrics`](#historicalmetrics) | Nothing. Stores samples on disk. | | `nameserverCheckEnabled` or `healthScoring` |

Node conditions carry the `NodeDoctor` prefix, for example `NodeDoctorDNSFlapping`. Metrics for these blocks are in [metrics.md](../metrics.md).

Some decimal keys are read only when YAML parses them as decimals. Write `60.0`, not `60`, for `healthScoring.degradedThreshold`, `healthScoring.unhealthyThreshold`, the four `healthScoring` weights, `correlation.minConfidence`, `correlation.nameserverFailureThreshold`, `predictiveAlerting.confidenceThreshold`, `trendDetection.degradationThreshold` and `trendDetection.anomalyZScore`. A whole number is ignored and the default applies.

[examples/dns-monitoring/](../../examples/dns-monitoring/) has a basic and an advanced monitor block. They are snippets: copy the monitor entry into the `monitors` list of a full config.

## successRateTracking

Keeps the last `windowSize` results for cluster DNS and for external DNS, and compares the failure rate with a threshold. `predictiveAlerting` and `trendDetection` read these same windows, whether or not this block is enabled.

| Key | Default | Notes |
|---|---|---|
| `enabled` | `false` | |
| `windowSize` | `10` | 1 to 10000 |
| `failureRateThreshold` | `0.3` | A decimal, or a percent above 1 (`30` means 0.3) |
| `minSamplesRequired` | `5` | No conditions until the window holds this many results. At most `windowSize`. |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `ClusterDNSDegraded` | `HighClusterDNSFailureRate`, failure rate at or above the threshold | `ClusterDNSDegradedClear` |
| `ClusterDNSIntermittent` | `IntermittentClusterDNSFailures`, some failures but below the threshold | `ClusterDNSIntermittentClear` |
| `ExternalDNSDegraded` | `HighExternalDNSFailureRate` | `ExternalDNSDegradedClear` |
| `ExternalDNSIntermittent` | `IntermittentExternalDNSFailures` | `ExternalDNSIntermittentClear` |

At most one of each Degraded/Intermittent pair is True.

## consistencyChecking

For each `customQueries` entry with `consistencyCheck: true` and `recordType: A`, sends several queries in a row and compares the results.

| Key | Default | Notes |
|---|---|---|
| `enabled` | `false` | |
| `queriesPerCheck` | `5` | 2 to 20 |
| `intervalBetweenQueries` | `200ms` | 10ms to 5s. A number means milliseconds. |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `DNSResolutionDown` | `AllConsistencyQueriesFailed` | `DNSResolutionDownClear` |
| `DNSResolutionIntermittent` | `IntermittentConsistencyFailures`, some queries failed | `DNSResolutionIntermittentClear` |
| `DNSResolutionInconsistent` | `InconsistentIPAddresses`, the answers had different IPs | `DNSResolutionInconsistentClear` |

At most one of the three is True. A CDN or load-balanced name returns different IPs on purpose, so `DNSResolutionInconsistent` on such a name is expected.

Events: `ConsistencyCheckAllFailed`, `ConsistencyCheckIntermittent`, `ConsistencyCheckIPVariation`, `ConsistencyCheckHighLatency`, `ConsistencyCheckCancelled`.

## healthScoring

Scores each nameserver in the resolver file from 0 to 100 on every check. Enabling it also turns on the per-nameserver queries (`google.com`, 2s timeout) that feed the score.

| Key | Default | Notes |
|---|---|---|
| `enabled` | `false` | |
| `degradedThreshold` | `70.0` | A score below this is degraded |
| `unhealthyThreshold` | `40.0` | A score below this is unhealthy. Must be below `degradedThreshold`. |
| `windowSize` | `20` | Results kept per nameserver, 3 to 1000. Scoring starts at 3 results. |
| `latencyBaseline` | `50ms` | p95 at or below this scores 100 |
| `latencyMax` | `2s` | p95 at or above this scores 0 |
| `successRateWeight` | `0.40` | The four weights must sum to 1.0. If all four are unset, the defaults apply. |
| `latencyWeight` | `0.25` | |
| `errorDiversityWeight` | `0.15` | |
| `consistencyWeight` | `0.20` | |

The four parts of the score:

| Part | Scores 100 when | Scores 0 when |
|---|---|---|
| Success rate | Every query succeeded | Every query failed |
| Latency | p95 of successful queries is at or below `latencyBaseline` (linear in between) | p95 is at or above `latencyMax`, or fewer than 2 queries succeeded |
| Errors | No errors. Each failure costs by type: Timeout 3, SERVFAIL/Refused/NXDOMAIN 2, other 1. | Every query timed out |
| Consistency | Latency coefficient of variation at or below 0.1 | Coefficient of variation at or above 2.0, or no successful queries |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `DNSNameserverUnhealthy` | `LowHealthScore`. The message lists `nameserver(score)`. | `NoUnhealthyNameservers` |
| `DNSNameserverDegraded` | `DegradedHealthScore` | `NoDegradedNameservers` |

The two conditions are independent: an unhealthy nameserver does not also count as degraded.

## correlation

Looks for failure patterns across nameservers and domains on every check. A pattern is reported when its confidence is at least `minConfidence`.

| Key | Default | Notes |
|---|---|---|
| `enabled` | `false` | |
| `minConfidence` | `0.7` | 0 to 1 |
| `windowMinutes` | `5` | Look-back for the temporal detector |
| `nameserverFailureThreshold` | `0.5` | A nameserver with a success rate below this is failing |
| `minNameserversForDomainCorrelation` | `2` | Nameservers that must fail a domain before it is reported |

| Detector | Data it uses | Reports when |
|---|---|---|
| Nameserver | `healthScoring` results | A nameserver's success rate is below `nameserverFailureThreshold` |
| Domain pattern | `testEachNameserver` results | A domain fails on enough nameservers, or several domains under one TLD fail |
| Temporal | `healthScoring` results | Two or more nameservers have 30% or more failures within `windowMinutes` |

| Condition | True (reason) |
|---|---|
| `DNSNameserverCorrelation` | `CorrelatedDNSFailure` |
| `DNSDomainPatternCorrelation` | `CorrelatedDNSFailure` |
| `DNSTemporalCorrelation` | `CorrelatedDNSFailure` |

The message holds the root-cause guess, for example "Domain example.com is failing on 2 of 3 nameservers". These conditions are only set True. They clear when the exporter's `conditionStaleTTL` expires.

## predictiveAlerting

Fits a straight line to the cluster and external success-rate windows from `successRateTracking` and estimates when the rate will cross `1 - failureRateThreshold`.

| Key | Default | Notes |
|---|---|---|
| `enabled` | `false` | |
| `predictionWindow` | `30m` | Breaches further out are ignored |
| `warningLeadTime` | `15m` | The condition goes True only when the breach is this close |
| `minDataPoints` | `10` | Results needed before predicting. Keep it at or below `successRateTracking.windowSize`, or it never predicts. |
| `confidenceThreshold` | `0.8` | Minimum R² of the fit |

The monitor does not predict when the rate is flat or rising, or is already below the threshold.

| Condition | True (reason) | False (reason) |
|---|---|---|
| `DNSPredictedDegradation` | `ClusterDNSBreachPredicted`, `ExternalDNSBreachPredicted` | `NoDNSBreachPredicted` |

When no prediction can be made, the monitor leaves the condition as it was.

## trendDetection

Watches the success rates from the `successRateTracking` windows over time.

| Key | Default | Notes |
|---|---|---|
| `enabled` | `false` | |
| `windowSize` | `20` | Samples used for the slope and flap count |
| `degradationThreshold` | `-0.05` | Slope per sample. A steeper drop is `DNSDegrading`. |
| `anomalyZScore` | `2.5` | Distance from the long-term mean in standard deviations. The check uses the absolute value, so a sudden improvement also counts. |
| `flapDetection.enabled` | `true` | Any `flapDetection` block turns it on, even with `enabled: false` |
| `flapDetection.minOscillations` | `3` | Crossings of the 50% success line needed for `DNSFlapping` |
| `flapDetection.windowMinutes` | `10` | |

| Condition | True (reason) |
|---|---|
| `DNSDegrading` | `ClusterDNSDegrading`, `ExternalDNSDegrading` |
| `DNSAnomalous` | `ClusterDNSAnomalous`, `ExternalDNSAnomalous` |
| `DNSFlapping` | `ClusterDNSFlapping`, `ExternalDNSFlapping` |

These conditions are only set True. They clear when the exporter's `conditionStaleTTL` expires.

## historicalMetrics

Writes every per-nameserver query result to a local SQLite file. The per-nameserver queries run only when `nameserverCheckEnabled` or `healthScoring.enabled` is true.

| Key | Default | Notes |
|---|---|---|
| `enabled` | `false` | |
| `storagePath` | `/var/lib/node-doctor/dns-metrics.db` | Must be on a writable volume |
| `retentionDays` | `7` | |

## Reading the conditions

List the DNS conditions on a node:

```bash
kubectl get node <node> -o json \
  | jq '.status.conditions[] | select(.type | test("^NodeDoctor.*DNS")) | {type, status, reason, message}'
```

The agent image has no `dig` or `nslookup`. Run DNS tests from a debug pod or from the node itself.

## Troubleshooting by condition

| Condition | Likely cause | Check first |
|---|---|---|
| `DNSNameserverUnhealthy`, `DNSNameserverDegraded` | One CoreDNS pod or upstream resolver is failing | `kubectl -n kube-system get pods -l k8s-app=kube-dns -o wide`, then the logs of the pod behind the listed IP |
| `DNSNameserverCorrelation`, `DNSTemporalCorrelation` | Several nameservers fail together: a shared upstream, or a cluster-wide outage | `kubectl -n kube-system get endpoints kube-dns`, recent CNI or kube-proxy changes |
| `DNSDomainPatternCorrelation` | One record or zone is broken | The record itself, the CoreDNS ConfigMap |
| `DNSDegrading` with reason `Cluster...` | CoreDNS under load, conntrack or iptables problems on the node | `kubectl top pods -n kube-system -l k8s-app=kube-dns`, `nf_conntrack_count` against `nf_conntrack_max` |
| `DNSDegrading` with reason `External...` | Upstream resolver or egress firewall | Port 53 egress, the CoreDNS `forward` target |
| `DNSFlapping` | CoreDNS restart loop, conntrack exhaustion | CoreDNS restart count and memory limits, conntrack usage |
| `DNSAnomalous` | A sudden change from the long-term rate | Recent config changes, traffic bursts |
| `DNSPredictedDegradation` | A steady decline that will cross the threshold | Same as `DNSDegrading` |
| `ClusterDNSDegraded` only, `ExternalDNSDegraded` clear (or the reverse) | Cluster and external paths fail separately | CoreDNS for cluster names, upstream and egress for external names |

To make the trend and prediction conditions less sensitive, raise `anomalyZScore`, `windowSize`, `minOscillations` or `confidenceThreshold`, or move `degradationThreshold` closer to 0. Lower them to catch problems earlier.
