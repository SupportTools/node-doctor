# Monitors

A monitor runs one check on a fixed interval and reports events and conditions for the node. This page lists every registered monitor type, its config keys with their defaults, and what it reports.

Fields shared by all monitors (`name`, `type`, `enabled`, `interval`, `timeout`, `dependsOn`, `remediation`) are in [configuration.md](configuration.md). Remediation is in [remediation.md](remediation.md). Metrics are in [metrics.md](metrics.md).

## How monitors report

| Output | Where it goes |
|---|---|
| Condition `X` | Node condition `NodeDoctorX`. The exporter drops non-alphanumeric characters and capitalizes each word, so `oom-killer` becomes `NodeDoctorOomKiller`. |
| Event with reason `R` | Kubernetes Event on the node with reason `R` |
| Latency and check data | Prometheus metrics, see [metrics.md](metrics.md) |

When a monitor stops reporting a condition, the exporter removes it after `exporters.kubernetes.conditionStaleTTL` (default `6m`). Conditions marked "True only" below rely on this to clear.

### Monitors added by default

When the config has no monitor of a given type, the agent adds that type's built-in monitor, enabled. This applies to:

`system-cpu`, `system-memory`, `system-disk`, `network-dns-check`, `network-gateway-check`, `network-ip-forwarding`, `network-ipv6-sysctl`, `network-ipv6-route`, `network-ipv6-neighbor`, `network-ipv6-firewall`.

To turn one off, list it with `enabled: false`. All other types run only when you configure them.

## Monitor types

| Type | Checks |
|---|---|
| [`system-cpu`](#system-cpu) | Load average and thermal throttling |
| [`system-memory`](#system-memory) | Memory and swap usage, OOM kills |
| [`system-disk`](#system-disk) | Space, inodes, read-only mounts, write probes |
| [`network-dns-check`](#network-dns-check) | DNS resolution from the node |
| [`network-cluster-dns-pod`](#network-cluster-dns-pod) | Cluster DNS as seen from overlay-test pods |
| [`network-gateway-check`](#network-gateway-check) | Default gateway reachability over ICMP |
| [`network-connectivity-check`](#network-connectivity-check) | HTTP(S) endpoint reachability |
| [`network-cni-check`](#network-cni-check) | Peer-to-peer reachability and CNI config |
| [`network-ip-forwarding`](#network-ip-forwarding) | IP forwarding sysctls |
| [`network-ipv6-sysctl`](#network-ipv6-sysctl) | `disable_ipv6` sysctls |
| [`network-ipv6-route`](#network-ipv6-route) | IPv6 default route |
| [`network-ipv6-neighbor`](#network-ipv6-neighbor) | IPv6 addresses and RA/SLAAC sysctls |
| [`network-ipv6-firewall`](#network-ipv6-firewall) | An IPv6 firewall that drops everything |
| [`kubernetes-kubelet-check`](#kubernetes-kubelet-check) | Kubelet `/healthz`, systemd unit, PLEG |
| [`kubernetes-apiserver-check`](#kubernetes-apiserver-check) | API server reachability and latency |
| [`kubernetes-runtime-check`](#kubernetes-runtime-check) | Container runtime socket and service |
| [`kubernetes-capacity-check`](#kubernetes-capacity-check) | Running pods against pod capacity |
| [`custom-plugin`](#custom-plugin) | Your own script or binary |
| [`custom-logpattern`](#custom-logpattern) | Regex matches in kernel and journal logs |
| `noop` | Nothing. It is the template for [writing a monitor](#writing-a-monitor). |

`node-doctor -list-monitors` prints the types in your binary.

---

## System monitors

### system-cpu

Divides the 1-minute load average by the CPU count and compares it with two factors. It also watches `thermal_throttle/core_throttle_count` for increases.

| Key | Default | Notes |
|---|---|---|
| `warningLoadFactor` | `0.8` | Must be below `criticalLoadFactor` |
| `criticalLoadFactor` | `1.5` | |
| `sustainedHighLoadChecks` | `3` | Consecutive critical checks before `CPUPressure` is True |
| `checkLoadAverage` | `true` | |
| `checkThermalThrottle` | `true` | |
| `loadAvgPath` | `/proc/loadavg` | |
| `cpuInfoPath` | `/proc/cpuinfo` | Falls back to the Go runtime CPU count |
| `thermalBasePath` | `/sys/devices/system/cpu` | |

If both `check*` keys end up false, the monitor turns both on. To disable one check, set the other to `true` explicitly.

| Condition | True (reason) | False (reason) |
|---|---|---|
| `CPUPressure` | `SustainedHighLoad` | `ElevatedLoad`, `NormalLoad` |
| `CPUThermalHealthy` | `NoThermalThrottle` | `ThermalThrottle` |
| `CPUHealthy` | `CPUHealthy`, when the check produced no events. True only. | |

Events: `HighCPULoad`, `ElevatedCPULoad`, `CPUThermalThrottle`, `LoadAverageCheckFailed`, `ThermalThrottleCheckFailed`.

### system-memory

Computes usage as `1 - MemAvailable/MemTotal` (`MemFree` when `MemAvailable` is missing) and swap usage, and scans `/dev/kmsg` for OOM kills.

| Key | Default | Notes |
|---|---|---|
| `warningThreshold` | `85.0` | Percent. Must be below `criticalThreshold` |
| `criticalThreshold` | `95.0` | |
| `swapWarningThreshold` | `50.0` | Skipped when the node has no swap |
| `swapCriticalThreshold` | `80.0` | |
| `sustainedHighMemoryChecks` | `3` | Consecutive critical checks before `MemoryPressure` is True |
| `checkMemoryUsage` | `true` | |
| `checkSwapUsage` | `true` | |
| `checkOOMKills` | `true` | Reports at most one OOM kill per minute |
| `memInfoPath` | `/proc/meminfo` | |
| `kmsgPath` | `/dev/kmsg` | |

If all three `check*` keys end up false, the monitor turns all of them on.

| Condition | True (reason) | False (reason) |
|---|---|---|
| `MemoryPressure` | `SustainedHighMemory` | `ElevatedMemory`, `NormalMemory` |
| `MemoryHealthy` | `MemoryHealthy`, when the check produced no events. True only. | |

Events: `HighMemoryUsage`, `ElevatedMemoryUsage`, `HighSwapUsage`, `ElevatedSwapUsage`, `OOMKillDetected`, `OOMCheckPermissionDenied`, `OOMCheckNotSupported`, `OOMCheckNotAvailable`, `MemoryCheckFailed`, `SwapCheckFailed`, `OOMCheckFailed`.

### system-disk

Runs `statfs` on each mount point to check space, inodes and the read-only flag. Optional write probes create, write, fsync and delete a file. They catch a filesystem that is still mounted read-write but no longer completes writes, which the read-only flag misses.

| Key | Default | Notes |
|---|---|---|
| `mountPoints` | See below | List of `{path, warningThreshold, criticalThreshold, inodeWarningThreshold, inodeCriticalThreshold}` |
| `sustainedHighDiskChecks` | `3` | Consecutive critical checks per mount before `DiskPressure` is True |
| `checkDiskSpace` | `true` | |
| `checkInodes` | `true` | |
| `checkReadonly` | `true` | |
| `checkIOHealth` | `true` | Reads `/proc/diskstats`. It never emits `HighIOWait` today because the check needs a timestamp the agent does not pass. |
| `diskStatsPath` | `/proc/diskstats` | |
| `writeProbePaths` | none | Directories to probe. Each must be a writable mount of a host directory on the filesystem to test. |
| `writeProbeTimeout` | `10s` | A probe that does not finish in time fails. A probe still stuck from an earlier check also fails. |

Mount point thresholds default to 85 and 95 percent for both space and inodes. When `mountPoints` is omitted, the monitor checks `/`, `/var/lib/docker` and `/var/lib/containerd`. The monitor that is added by default checks `/` and `/var/lib/kubelet` at 85/95 and `/var/lib/containerd` at 80/90.

If all four `check*` keys end up false, the monitor turns all of them on.

| Condition | True (reason) | False (reason) |
|---|---|---|
| `DiskPressure` | `SustainedHighDisk` | `HighDisk` (critical, not yet sustained), `ElevatedDisk`, `NormalDisk` |
| `InodePressure` | `CriticalInode` | `ElevatedInode`, `NormalInode` |
| `ReadonlyFilesystem` | `ReadonlyFilesystem` (statfs flag), `WriteProbeFailed` | `WriteProbeOK`, unless the statfs check reports True |
| `DiskHealthy` | `DiskHealthy`, when the check produced no events. True only. | |

Events: `HighDiskUsage`, `ElevatedDiskUsage`, `HighInodeUsage`, `ElevatedInodeUsage`, `ReadonlyFilesystem`, `WriteProbeFailed`, `DiskCheckFailed`, `IOHealthCheckFailed`.

---

## Network monitors

### network-dns-check

Resolves cluster and external domains with the node resolver. It can also query each nameserver in the resolver file directly.

| Key | Default | Notes |
|---|---|---|
| `clusterDomains` | `kubernetes.default.svc.<cluster-domain>`, with the domain taken from the resolver `search` list (`cluster.local` if none) | `[]` disables the cluster check. The Helm chart sets `[]` because the hostNetwork agent cannot resolve cluster records through Cilium. |
| `externalDomains` | `google.com`, `cloudflare.com` | `[]` disables the external check |
| `customQueries` | none | List, fields below |
| `latencyThreshold` | `1s` | Duration string, or a number of seconds |
| `nameserverCheckEnabled` | `false` (`true` in the default-added monitor) | Resolves `google.com` against each nameserver with a 2s timeout |
| `resolverPath` | `/etc/resolv.conf` | |
| `failureCountThreshold` | `3` | Consecutive failed checks before `ClusterDNSDown` or `ExternalDNSDown` is True |

At least one of `clusterDomains`, `externalDomains` or `customQueries` must be non-empty.

| `customQueries` field | Default | Notes |
|---|---|---|
| `domain` | required | |
| `recordType` | `A` | `A` or `AAAA`. Other types emit `UnsupportedQueryType`. |
| `testEachNameserver` | `false` | Query every nameserver for this domain. `A` only. |
| `consistencyCheck` | `false` | Run rapid repeated queries. `A` only, and needs `consistencyChecking.enabled`. |

The opt-in analysis blocks (`successRateTracking`, `consistencyChecking`, `healthScoring`, `correlation`, `predictiveAlerting`, `trendDetection`, `historicalMetrics`) and their conditions are in [DNS monitor: advanced features](monitors/dns-advanced.md).

| Condition | True (reason) | False (reason) |
|---|---|---|
| `ClusterDNSDown` | `RepeatedClusterDNSFailures` | `ClusterDNSResolved`. Not reported when `clusterDomains` is empty. |
| `ExternalDNSDown` | `RepeatedExternalDNSFailures` | `ExternalDNSResolved` |
| `CustomDNSDown` | `AllNameserversFailed`, from `testEachNameserver` | `CustomDNSDownClear` |
| `DNSResolutionDegraded` | `PartialNameserverFailure`, from `testEachNameserver` | `DNSResolutionDegradedClear` |

Between the first failure and the threshold, the monitor leaves `ClusterDNSDown` and `ExternalDNSDown` as they were.

Events: `ClusterDNSResolutionFailed`, `ExternalDNSResolutionFailed`, `ClusterDNSNoRecords`, `ExternalDNSNoRecords`, `HighClusterDNSLatency`, `HighExternalDNSLatency`, `CustomDNSQueryFailed`, `CustomDNSNoRecords`, `HighCustomDNSLatency`, `UnsupportedQueryType`, `AAAAFeatureUnsupported`, `NameserverUnreachable`, `NameserverDomainResolutionFailed`, `NameserverDomainLatencyHigh`, `ResolverConfigParseError`, `NoNameserversConfigured`. Failure messages start with the error class: `[Timeout]`, `[NXDOMAIN]`, `[SERVFAIL]`, `[Refused]`, `[Temporary]` or `[Unknown]`.

### network-cluster-dns-pod

Asks overlay-test pods, which run on the pod network, whether they can resolve cluster DNS, and sets `ClusterDNSDown` from their answers. Use it when the agent runs hostNetwork and `network-dns-check` has `clusterDomains: []`.

The type has no built-in default, so it runs only when you configure it. In Helm, set `clusterDnsPodProbe.enabled: true`. It needs an overlay-test image that serves `/clusterdns` (v1.8.3 or later). With an older image every probe gets a 404 and `ClusterDNSDown` goes True.

Each check sends `GET http://<podIP>:<probePort><probePath>` to at most 5 overlay-test pods, sorted by name.

| Key | Default | Notes |
|---|---|---|
| `labelSelector` | `app=node-doctor-overlay-test` | |
| `namespace` | `POD_NAMESPACE`, then the service account namespace, then `node-doctor` | |
| `probePort` | `8023` | |
| `probePath` | `/clusterdns` | |
| `timeout` | `5s` | Per-pod probe timeout |
| `minSuccessPods` | `1` | Pods that must answer `resolved: true` for the check to pass |
| `failureCountThreshold` | `3` | Consecutive failed checks before `ClusterDNSDown` is True |
| `enabled` | `false` | Parsed but not used. Use the monitor-level `enabled`. |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `ClusterDNSDown` | `RepeatedClusterDNSPodProbeFailures` | `ClusterDNSResolvedViaPods` |

When it finds no overlay-test pods, the monitor emits `NoOverlayTestPodsFound` and does not count a failure. Every check also emits `ClusterDNSPodProbeSummary`.

### network-gateway-check

Finds the default gateway in the kernel route table and pings it over ICMP. A check fails when half or more of the pings fail. The agent needs `CAP_NET_RAW`.

| Key | Default | Notes |
|---|---|---|
| `pingCount` | `3` | At least 1 |
| `pingTimeout` | `1s` | Per ping |
| `latencyThreshold` | `100ms` | An average RTT above this emits `HighGatewayLatency` |
| `autoDetectGateway` | `true` | |
| `manualGateway` | none | IP address. Overrides auto-detection. |
| `addressFamily` | `ipv4` | `ipv4`, `ipv6`, or `auto` (IPv4 first, then IPv6) |
| `failureCountThreshold` | `3` | Consecutive failed checks before `NetworkUnreachable` is True |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `NetworkUnreachable` | `GatewayUnreachable` | `GatewayReachable` |

`network-connectivity-check` writes the same condition type.

Events: `GatewayHealthy`, `GatewayUnreachable`, `HighGatewayLatency`, `GatewayRecovered`, `GatewayDetectionFailed`, `GatewayPingError`.

### network-connectivity-check

Sends HTTP requests to a list of endpoints. The type has no default, so you must list at least one endpoint (at most 50).

| Key | Default | Notes |
|---|---|---|
| `endpoints` | required | List, fields below |
| `failureThreshold` | `3` | Consecutive failures of one endpoint before `NetworkUnreachable` is True |

| Endpoint field | Default | Notes |
|---|---|---|
| `url` | required | `http` or `https`, with a host |
| `name` | the URL | |
| `method` | `HEAD` | `GET`, `HEAD` or `OPTIONS` |
| `expectedStatusCode` | `200` | |
| `timeout` | `10s` | |
| `followRedirects` | `false` | |
| `headers` | none | Map of strings |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `NetworkUnreachable` | `EndpointUnreachable`, per endpoint | `EndpointReachable`, when an endpoint recovers |

Events: `EndpointReachable`, `EndpointUnreachable`, `EndpointDNSFailure`, `EndpointRecovered`, `EndpointCheckError`, `ConnectivityHealthy`, `PartialConnectivity`, `ConnectivityFailure`, `DNSResolutionFailure`, `ExternalConnectivityFailure`.

### network-cni-check

Discovers peer pods, probes each one, and checks the CNI config directory. By default it probes the overlay-test pods over HTTP, so it tests the pod network and not the node network.

| Key | Default | Notes |
|---|---|---|
| `discovery.method` | `kubernetes` | `kubernetes` or `static` |
| `discovery.namespace` | `node-doctor` | |
| `discovery.labelSelector` | `app=node-doctor` | Used when `overlayTestEnabled` is false |
| `discovery.overlayTestEnabled` | `true` | Probe overlay-test pod IPs |
| `discovery.overlayTestLabelSelector` | `app=node-doctor-overlay-test` | |
| `discovery.refreshInterval` | `5m` | |
| `discovery.staticPeers` | none | IPs, for `method: static` |
| `connectivity.probeMethod` | `http` if `overlayTestEnabled`, else `icmp` | |
| `connectivity.probePort` | `8023` | HTTP probe |
| `connectivity.probePath` | `/healthz` | HTTP probe |
| `connectivity.pingCount` | `3` | Probes per peer. A peer is reachable when more than half succeed. |
| `connectivity.pingTimeout` | `5s` | |
| `connectivity.warningLatency` | `200ms` | An average latency above this counts as high |
| `connectivity.highLatencyThreshold` | `3` | Consecutive high-latency checks before a peer counts toward `NetworkDegraded`. `1` turns off the debounce. |
| `connectivity.crossZoneWarningLatency` | unset | Replaces `warningLatency` for peers in a different `topology.kubernetes.io/zone` |
| `connectivity.criticalLatency` | `500ms` | Parsed but not used |
| `connectivity.crossZoneCriticalLatency` | unset | Parsed but not used |
| `connectivity.failureThreshold` | `3` | Consecutive failures before a peer appears in `PeersUnreachableSummary` |
| `connectivity.minReachablePeers` | `80` | Percent of peers that must be reachable |
| `cniHealth.enabled` | `true` | |
| `cniHealth.configPath` | `/etc/cni/net.d` | The Helm chart uses `/host/etc/cni/net.d` |
| `cniHealth.checkInterfaces` | `false` | |
| `cniHealth.expectedInterfaces` | none | |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `NetworkPartitioned` | `InsufficientPeerReachability` | `SufficientPeerReachability` |
| `NetworkDegraded` | `HighLatencyDetected` | `NormalLatency` |
| `CNIConfigValid` | `ConfigFilesPresent` | `ConfigFilesInvalid` |
| `CNIInterfacesHealthy` | `InterfacesPresent` | `InterfacesMissing` |
| `CNIHealthy` | `CNIOperational` | `CNIIssuesDetected` |

Events: `CNIConnectivitySummary`, `HighPeerLatencySummary`, `HighPeerLatencyRecovered`, `PeersUnreachableSummary`, `PeersReachableRecovered`, `NoPeersFound`, `CNIConfigError`, `CNIInterfaceWarning`. The peer summary events fire only when the list of peers changes.

### network-ip-forwarding

Reads `net.ipv4.ip_forward` and `net.ipv6.conf.all.forwarding`. Pod networking needs IPv4 forwarding, and IPv6 forwarding on dual-stack nodes.

| Key | Default | Notes |
|---|---|---|
| `checkIPv4` | `true` | |
| `checkIPv6` | `true` | An unreadable IPv6 setting emits a warning event and is not a finding |
| `checkPerInterface` | `false` | Checks `net.ipv4.conf.<iface>.forwarding`, skipping `lo`, `all` and `default` |
| `interfaces` | all | Limits the per-interface check to these names |
| `procPath` | `/proc` | Use `/host/proc` when the host `/proc` is mounted there |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `IPForwardingDisabled` | `ForwardingDisabled` | `ForwardingEnabled` |

Events: `IPv4ForwardingDisabled`, `IPv6ForwardingDisabled`, `InterfaceForwardingDisabled`, `IPForwardingHealthy`, `IPForwardingReadError`, `IPForwardingGlobError`.

---

## IPv6 / Dual-Stack Monitors

These four monitors only read state. They never change sysctls, routes or firewall rules. They are added by default with `expectIPv6Enabled: true` (or `expectDefaultRoute: true`), so on IPv4-only nodes set those keys to `false` or disable the monitors.

### network-ipv6-sysctl

Reads `net.ipv6.conf.{all,default}.disable_ipv6`, and optionally the per-interface files.

| Key | Default | Notes |
|---|---|---|
| `expectIPv6Enabled` | `true` | When false, a disabled setting is only an info event |
| `checkPerInterface` | `false` | |
| `interfaces` | all | Limits the per-interface check |
| `skipInterfaces` | `all`, `default`, `lo` | Setting it replaces the list |
| `procPath` | `/proc` | |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `IPv6SysctlMisconfigured` | `DisableIPv6Set` | `IPv6SysctlsHealthy` |

An unreadable `all` or `default` file also sets the condition True, whatever `expectIPv6Enabled` says.

Events: `IPv6Disabled`, `IPv6DisabledExpected`, `InterfaceIPv6Disabled`, `InterfaceIPv6DisabledExpected`, `IPv6SysctlsHealthy`, `IPv6SysctlReadError`, `IPv6SysctlGlobError`.

### network-ipv6-route

Looks for a default route in `/proc/net/ipv6_route`.

| Key | Default | Notes |
|---|---|---|
| `expectDefaultRoute` | `true` | When false, a missing route is informational |
| `procPath` | `/proc` | |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `IPv6DefaultRouteMissing` | `NoIPv6DefaultRoute`, `IPv6RouteTableUnreadable` | `IPv6DefaultRoutePresent`, `IPv6DefaultRouteNotExpected` |

Events: `IPv6DefaultRoutePresent`, `IPv6DefaultRouteNotExpected`, `NoIPv6DefaultRoute`, `IPv6RouteTableUnreadable`, `IPv6RouteReadError`.

### network-ipv6-neighbor

Checks that interfaces have an IPv6 link-local address (and optionally a global one) in `/proc/net/if_inet6`, and that `accept_ra` and `autoconf` are not 0. It does not read the neighbor table.

| Key | Default | Notes |
|---|---|---|
| `expectIPv6Enabled` | `true` | When false, findings are informational |
| `checkPerInterface` | `true` | Runs the `accept_ra`/`autoconf` check |
| `requireGlobalAddress` | `false` | Flags interfaces that have only a link-local address |
| `interfaces` | all | |
| `skipInterfaces` | `all`, `default`, `lo` | |
| `procPath` | `/proc` | |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `IPv6LinkLocalMissing` | `IPv6LinkLocalMissing` | `IPv6LinkLocalPresent`, `IPv6LinkLocalMissingNotExpected`, `IPv6NoInterfacesObserved`, `IPv6AddressTableUnreadable` |
| `IPv6GlobalAddressMissing` | `IPv6GlobalAddressMissing`, only with `requireGlobalAddress` | `IPv6GlobalAddressPresent`, `IPv6GlobalAddressMissingNotRequired`, `IPv6GlobalAddressMissingNotExpected`, `IPv6NoInterfacesObserved`, `IPv6AddressTableUnreadable` |
| `IPv6RouterAdvertisementDisabled` | `IPv6RouterAdvertisementDisabled` | `IPv6RouterAdvertisementEnabled`, `IPv6RADisabledCheckSkipped`, `IPv6AcceptRAUnreadable` |

Events: `IPv6LinkLocalMissing`, `IPv6GlobalAddressMissing`, `IPv6RouterAdvertisementDisabled`, `IPv6RouterAdvertisementDisabledExpected`, `IPv6IfInet6ReadError`, `IPv6IfInet6ParseError`, `IPv6AcceptRAGlobError`, `IPv6AcceptRAReadError`.

### network-ipv6-firewall

Runs `nft list ruleset` or `ip6tables -S`. It flags a ruleset where INPUT, FORWARD and OUTPUT all default to DROP or REJECT and no rule accepts anything.

The published image contains neither `nft` nor `ip6tables`, so in the DaemonSet the monitor emits `IPv6FirewallToolNotFound` and keeps the condition False.

| Key | Default | Notes |
|---|---|---|
| `expectIPv6Enabled` | `true` | When false, a black hole is informational |
| `backend` | `auto` | `auto` (nft if present, else ip6tables), `nft` or `ip6tables` |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `IPv6FirewallBlackhole` | `IPv6FirewallBlackhole` | `IPv6FirewallHealthy`, `IPv6FirewallBlackholeNotExpected`, `IPv6FirewallToolNotFound`, `IPv6FirewallReadError` |

Events: `IPv6FirewallHealthy`, `IPv6FirewallBlackhole`, `IPv6FirewallBlackholeNotExpected`, `IPv6FirewallToolNotFound`, `IPv6FirewallReadError`.

---

## Kubernetes monitors

### kubernetes-kubelet-check

Calls the kubelet `/healthz` endpoint on every check. It can also check the systemd unit and the PLEG relist duration.

| Key | Default | Notes |
|---|---|---|
| `healthzURL` | `http://127.0.0.1:10248/healthz` | On a connection error to a loopback host, the monitor retries once on the other loopback family (`[::1]` or `127.0.0.1`) |
| `metricsURL` | `http://127.0.0.1:10250/metrics` | Used only by the PLEG check |
| `checkSystemdStatus` | `false` | Checks the `kubelet` unit, or a `kubelet`/`hyperkube` process when systemctl is missing |
| `checkPLEG` | `false` | |
| `plegThreshold` | `5s` | A slower relist emits `PLEGSlow` but does not fail the check |
| `failureThreshold` | `3` | Consecutive failed checks before `KubeletUnhealthy` is True |
| `httpTimeout` | `5s` | |
| `auth.type` | none | `none`, `serviceaccount`, `bearer` or `certificate` |
| `auth.tokenFile` | `/var/run/secrets/kubernetes.io/serviceaccount/token` | For `serviceaccount` |
| `auth.bearerToken` | none | Required for `bearer` |
| `auth.certFile`, `auth.keyFile` | none | Required for `certificate` |
| `auth.insecureSkipVerify` | `false` | |
| `circuitBreaker.enabled` | `false` | While open, the monitor skips checks and reports `KubeletUnhealthy` True |
| `circuitBreaker.failureThreshold` | `5` | Failures before the breaker opens |
| `circuitBreaker.openTimeout` | `30s` | |
| `circuitBreaker.halfOpenMaxRequests` | `3` | Successes in half-open before it closes |
| `circuitBreaker.useExponentialBackoff` | `false` | |
| `circuitBreaker.maxBackoffTimeout` | `5m` | |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `KubeletUnhealthy` | `RepeatedHealthCheckFailures`, `CircuitBreakerOpen` | `HealthCheckPassed` |
| `KubeletDown` | `SystemdServiceInactive` | `KubeletUp`, on a passing check |
| `KubeletHealthy` | `HealthCheckPassed`. True only. | |

To restart kubelet automatically, list `KubeletUnhealthy` in the monitor's `remediation.triggerConditions`. [remediation.md](remediation.md) covers the policy fields. The Helm chart sets this up under `monitors.kubelet.remediation`.

Events: `KubeletHealthy`, `KubeletHealthCheckFailed`, `KubeletRecovered`, `KubeletSystemdActive`, `KubeletSystemdInactive`, `SystemdCheckFailed`, `PLEGHealthy`, `PLEGSlow`, `PLEGMetricsFailed`, `CircuitBreakerOpen`, `CircuitBreakerHalfOpen`, `CircuitBreakerRecovered`, `CircuitBreakerMetrics`.

### kubernetes-apiserver-check

Calls the API server `/version` endpoint with the in-cluster client and measures the latency. The monitor-level `timeout` must be shorter than `interval`.

| Key | Default | Notes |
|---|---|---|
| `endpoint` | in-cluster config | Set it to use a different API server host |
| `latencyThreshold` | `2s` | |
| `httpTimeout` | `10s` | At most 5 times `latencyThreshold` |
| `failureThreshold` | `3` | Consecutive failures before `APIServerUnreachable` is True |
| `checkVersion` | `true` | Parsed but not used |
| `checkAuth` | `true` | Parsed but not used |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `APIServerReachable` | `APIServerHealthy` | `APIServerUnreachable`, after `failureThreshold` failures |
| `APIServerUnreachable` | `APIServerUnreachable` | `APIServerHealthy`, on recovery |
| `APIServerLatencyHigh` | `LatencyExceeded`. True only. | |

Events: `APIServerCheckFailed`, `APIServerAuthFailure`, `APIServerRateLimited`, `APIServerRecovered`, `APIServerSlow`.

### kubernetes-runtime-check

Finds the container runtime socket, connects to it, and checks the runtime's systemd unit.

| Key | Default | Notes |
|---|---|---|
| `runtimeType` | `auto` | `auto`, `docker`, `containerd` or `crio` |
| `dockerSocket` | `/var/run/docker.sock` | |
| `containerdSocket` | `/run/containerd/containerd.sock` | |
| `crioSocket` | `/var/run/crio/crio.sock` | |
| `checkSocketConnectivity` | `true` | At least one check must stay on |
| `checkSystemdStatus` | `true` | |
| `checkRuntimeInfo` | `true` | Connects to the socket a second time. It does not read a version. |
| `failureThreshold` | `3` | |
| `timeout` | `5s` | |

With `auto`, the monitor uses the first socket that exists, in this order:

1. `/run/k3s/containerd/containerd.sock`, `/var/run/k3s/containerd/containerd.sock` (unit `k3s`, also on RKE2)
2. `/var/run/cri-dockerd.sock`, `/run/cri-dockerd.sock` (unit `docker`)
3. `containerdSocket`, `/var/run/containerd/containerd.sock` (unit `containerd`)
4. `dockerSocket` (unit `docker`)
5. `crioSocket` (unit `crio`)

| Condition | True (reason) | False (reason) |
|---|---|---|
| `ContainerRuntimeUnhealthy` | `HealthCheckFailed` | `HealthCheckPassed` |

Events: `RuntimeHealthy`, `RuntimeRecovered`, `RuntimeSocketUnreachable`, `RuntimeSystemdInactive`, `RuntimeSystemdCheckFailed`, `RuntimeInfoFailed`.

### kubernetes-capacity-check

Counts the Running pods on the node and compares them with the node's allocatable (or capacity) pod count.

| Key | Default | Notes |
|---|---|---|
| `nodeName` | `NODE_NAME` env | Validation fails if neither is set |
| `warningThreshold` | `90.0` | Percent. Must be below `criticalThreshold` |
| `criticalThreshold` | `95.0` | |
| `failureThreshold` | `3` | Consecutive API errors before `PodCapacityUnhealthy` is True |
| `apiTimeout` | `10s` | |
| `checkAllocatable` | `true` | `false` uses capacity instead of allocatable |

| Condition | True (reason) | False (reason) |
|---|---|---|
| `PodCapacityPressure` | `HighPodUtilization` | `NormalPodUtilization` |
| `PodCapacityUnhealthy` | `RepeatedCheckFailures`. True only. | |

Events: `PodCapacityPressure`, `PodCapacityWarning`, `PodCapacityRecovered`, `CapacityCheckFailed`.

---

## Custom monitors

### custom-plugin

Runs an executable and turns its result into a status.

| Key | Default | Notes |
|---|---|---|
| `pluginPath` | required | Absolute, clean path without `..` |
| `args` | none | List of strings |
| `env` | none | Extra environment variables |
| `outputFormat` | `simple` | `simple` or `json` |
| `apiTimeout` | `10s` | Execution timeout |
| `failureThreshold` | `3` | Consecutive critical or failed runs before `PluginUnhealthy` is True |

How the result is read:

- Exit code `0` is healthy, `1` warning, `2` critical, anything else unknown. Unknown counts as a failure.
- `simple`: a stdout prefix of `OK:`, `WARNING:`, `CRITICAL:`, `ERROR:` or `UNKNOWN:` wins over the exit code.
- `json`: stdout is `{"status": "...", "message": "...", "metrics": {...}}`. `status` accepts `healthy`, `ok`, `pass`, `warning`, `warn`, `critical`, `error` and `fail`. Invalid JSON falls back to the exit code.

| Condition | True (reason) | False (reason) |
|---|---|---|
| `PluginHealthy` | `PluginCheckPassed`. True only. | |
| `PluginWarning` | `WarningThresholdExceeded`. True only. | |
| `PluginCritical` | `CriticalThresholdExceeded`, `CriticalThresholdNotYetExceeded`. True only. | |
| `PluginCheckFailed` | `CheckError`. True only. | |
| `PluginUnhealthy` | `PluginCritical`, `PluginCheckFailed` | `PluginHealthy`, on recovery |

Events: `PluginWarning`, `PluginCritical`, `PluginCheckFailed`, `PluginRecovered`.

### custom-logpattern

Matches regexes against new kernel and journal log lines.

| Key | Default | Notes |
|---|---|---|
| `patterns` | none | List, fields below. At least one pattern is required, counting defaults. |
| `useDefaults` | `false` | Adds the built-in patterns. A user pattern with the same `name` replaces the built-in one. |
| `checkKernelJournal` | `false` | Reads `journalctl -k` |
| `checkKmsg` | `false` | Reads `kmsgPath` directly |
| `kmsgPath` | `/dev/kmsg` | |
| `checkJournal` | `false` | Reads `journalctl -u` for each unit. Needs `journalUnits`. |
| `journalUnits` | none | At most 20 |
| `maxEventsPerPattern` | `10` | 1 to 1000. Events per pattern per `dedupWindow`. |
| `dedupWindow` | `5m` | 1s to 1h |

At least one of the three sources must be on. The monitor accepts at most 120 patterns. Before compiling a regex it rejects patterns over 1000 characters and patterns prone to catastrophic backtracking.

| Pattern field | Default | Notes |
|---|---|---|
| `name` | required | The event reason and, for `error` patterns, the condition type |
| `regex` | required | Go RE2 syntax |
| `severity` | `warning` | `info`, `warning` or `error` |
| `description` | none | Starts the event message |
| `source` | `both` | `kmsg`, `journal` or `both`. `kmsg` patterns also match `journalctl -k` lines. |

Every match emits an event with the pattern `name` as its reason. For `error` patterns the monitor also sets a condition named after the pattern: True with reason `LogPatternMatched` on a match, and False with `LogPatternNotMatched` on a check without one.

The built-in patterns live in `pkg/monitors/custom/patterns.go`. They cover OOM kills, disk and NIC errors, kernel panics, MCE and EDAC memory errors, NFS, conntrack, kubelet, container runtimes, CNI plugins and kube-proxy.

---

## Writing a monitor

A monitor package registers its type in `init()` with a factory, a validator and an optional default config. Start from `pkg/monitors/example/noop.go` and the package notes in `pkg/monitors/doc.go`, then add a blank import of the package to `cmd/node-doctor/main.go`.
