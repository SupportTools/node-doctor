# Metrics Reference

This page lists every Prometheus metric that node-doctor exports. The agent metrics are defined in `pkg/exporters/prometheus/metrics.go` and the controller metrics in `pkg/controller/metrics.go`.

## Agent

The agent serves metrics when `exporters.prometheus.enabled` is `true` (see [configuration.md](configuration.md#exportersprometheus)).

| Setting | Code default | Helm chart and `deployment/` manifests |
|---------|--------------|----------------------------------------|
| Port | `9100` | `9101` |
| Path | `/metrics` | `/metrics` |

The same port also answers `/health` with a fixed JSON body.

Names below use the default prefix `node_doctor_`. The prefix is `exporters.prometheus.namespace`, followed by `subsystem` when set. Every agent metric has a `node` label with `settings.nodeName`, plus any constant labels from `exporters.prometheus.labels`. The endpoint also serves the standard Go runtime (`go_*`) and process (`process_*`) metrics.

### Monitors and status

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `node_doctor_status_updates_total` | counter | `source` | Status reports received from each monitor. `source` is the monitor name. |
| `node_doctor_monitor_cycles_total` | counter | `monitor_name`, `result` | Status reports per monitor. `result` is `error` when any condition in the report has status `False`, otherwise `success`. |
| `node_doctor_monitor_cycle_last_timestamp_seconds` | gauge | `monitor_name` | Unix time of the monitor's latest report. |
| `node_doctor_monitor_check_duration_seconds` | histogram | `monitor_name` | Time the Prometheus exporter spent processing the report. It does not measure the monitor's check. Default buckets. |
| `node_doctor_conditions_total` | counter | `condition_type`, `status` | Conditions seen, counted once per report. |
| `node_doctor_condition_status` | gauge | `condition_type` | Latest status of each condition: `1` for `True`, `0` for `False` or `Unknown`. `condition_type` is the monitor's type without the `NodeDoctor` prefix. |
| `node_doctor_events_total` | counter | `source`, `severity` | Events from status reports and problems. For problems, `source` is `unknown`. |
| `node_doctor_problems_total` | counter | `problem_type`, `severity`, `source` | Problems reported. `source` is always `unknown`. |
| `node_doctor_problems_active` | gauge | `problem_type`, `severity` | Distinct problems reported since the exporter started. The count only resets when the Prometheus exporter is reconfigured by a reload. |
| `node_doctor_monitor_up` | gauge | `monitor_name`, `monitor_type` | Registered but never set, so it has no series. |

### Exporter health

These only cover the Prometheus exporter itself (`exporter="prometheus"`).

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `node_doctor_export_operations_total` | counter | `exporter`, `operation`, `result` | Successful exports. `operation` is `status` or `problem`. `result` is always `success`. |
| `node_doctor_export_errors_total` | counter | `exporter`, `error_type` | Exports rejected because the status or problem failed validation (`error_type="validation"`). |
| `node_doctor_export_duration_seconds` | histogram | `exporter`, `operation` | Export time. Default buckets. |
| `node_doctor_exporter_healthy` | gauge | `exporter` | `1` if the last export succeeded, `0` if it failed. |
| `node_doctor_exporter_last_success_timestamp_seconds` | gauge | `exporter` | Unix time of the last successful export. |
| `node_doctor_exporter_consecutive_failures` | gauge | `exporter` | Failed exports since the last success. |

### Process

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `node_doctor_info` | gauge | `version`, `git_commit`, `go_version`, `build_time` | Always `1`. `version` and `git_commit` are always `unknown`, and `build_time` holds the exporter start time. |
| `node_doctor_start_time_seconds` | gauge | none | Unix time the exporter started. |
| `node_doctor_uptime_seconds` | gauge | none | Seconds since start, updated on each export. |

### Config reload

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `node_doctor_config_reloads_total` | counter | `result` | Reload attempts. `result` is `success` or `failure`. |
| `node_doctor_config_reload_last_timestamp_seconds` | gauge | none | Unix time of the last reload attempt. |
| `node_doctor_config_reload_last_success` | gauge | none | `1` if the last reload succeeded, `0` if it failed. |
| `node_doctor_config_reload_duration_seconds` | histogram | none | Reload duration. Default buckets. |

### Remediation

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `node_doctor_remediator_circuit_breaker_state` | gauge | none | `0` closed, `1` open, `2` half-open. Only present when `remediation.enabled` is `true`. |

### Network latency

These come from the latency data that monitors attach to their status. The source monitor type is in each heading.

**Gateway** (`network-gateway-check`):

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `node_doctor_gateway_latency_seconds` | gauge | `gateway_ip`, `address_family` | Latest latency to the default gateway. |
| `node_doctor_gateway_latency_histogram_seconds` | histogram | `gateway_ip` | Buckets 1ms to 1s. |

**Peers** (`network-cni-check`):

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `node_doctor_peer_latency_seconds` | gauge | `peer_node`, `peer_ip`, `address_family` | Latest latency to each peer. |
| `node_doctor_peer_latency_avg_seconds` | gauge | `peer_node`, `peer_ip`, `address_family` | Average latency to each peer. |
| `node_doctor_peer_reachable` | gauge | `peer_node`, `peer_ip`, `address_family` | `1` reachable, `0` unreachable. |
| `node_doctor_peer_latency_histogram_seconds` | histogram | `peer_node` | Buckets 1ms to 1s. |
| `node_doctor_peers_total` | gauge | none | Peers in the latest report. |
| `node_doctor_peers_reachable_total` | gauge | none | Reachable peers in the latest report. |
| `node_doctor_peer_set_size` | gauge | none | Peers in the current peer set. Expect cluster nodes minus 1. A higher value means the node holds stale peers. |
| `node_doctor_peer_set_churn_total` | counter | `change` | Peers that joined (`added`) or left (`removed`) the set. A rename or new IP counts as one of each. |

When a peer leaves the set, the agent deletes its per-peer series.

**DNS** (`network-dns-check`):

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `node_doctor_dns_latency_seconds` | gauge | `dns_server`, `domain`, `record_type`, `address_family` | Latest resolution latency. |
| `node_doctor_dns_latency_histogram_seconds` | histogram | `domain_type` | Buckets 1ms to 2s. `domain_type` is `cluster`, `external` or `custom`. |
| `node_doctor_dns_nameserver_health_score` | gauge | `nameserver` | Composite score 0 to 100. 100 is healthy. |
| `node_doctor_dns_nameserver_success_score` | gauge | `nameserver` | Success-rate sub-score, 0 to 100. |
| `node_doctor_dns_nameserver_latency_score` | gauge | `nameserver` | P95 latency sub-score, 0 to 100. |
| `node_doctor_dns_nameserver_error_score` | gauge | `nameserver` | Error-diversity sub-score, 0 to 100. |
| `node_doctor_dns_nameserver_consistency_score` | gauge | `nameserver` | Latency-variance sub-score, 0 to 100. |
| `node_doctor_dns_prediction_confidence` | gauge | `domain_type` | R² (0 to 1) of the success-rate trend fit. |
| `node_doctor_dns_predicted_breach_seconds` | gauge | `domain_type` | Seconds until the success rate is predicted to cross the failure threshold. The series only exists while a breach is predicted. |

**API server** (`kubernetes-apiserver-check`):

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `node_doctor_apiserver_latency_seconds` | gauge | none | Latest API server response latency. |
| `node_doctor_apiserver_latency_histogram_seconds` | histogram | none | Buckets 10ms to 10s. |

`address_family` is `ipv4`, `ipv6` or `unknown`.

## Controller

The controller serves its metrics on its main HTTP port (`server.port`, default `8080`) at `prometheus.path` (default `/metrics`) when `prometheus.enabled` is `true` (the default). The controller config's `prometheus.port` and the `CONTROLLER_PROMETHEUS_PORT` variable are parsed but not used. Controller metrics have no `node` label and no Go or process metrics.

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `node_doctor_cluster_nodes_total` | gauge | none | Nodes reporting to the controller. |
| `node_doctor_cluster_nodes_healthy` | gauge | none | Nodes with healthy status. |
| `node_doctor_cluster_nodes_degraded` | gauge | none | Nodes with degraded status. |
| `node_doctor_cluster_nodes_critical` | gauge | none | Nodes with critical status. |
| `node_doctor_cluster_nodes_unknown` | gauge | none | Nodes with unknown status. |
| `node_doctor_cluster_problems_active` | gauge | none | Active problems across all nodes. |
| `node_doctor_cluster_problem_nodes` | gauge | `problem_type`, `severity` | Nodes affected by each problem type. |
| `node_doctor_cluster_problem_active` | gauge | `problem_type` | `1` while the problem type is active on any node. |
| `node_doctor_correlation_active_total` | gauge | none | Active cross-node correlations. |
| `node_doctor_correlation_detected_total` | counter | `type` | Correlations detected. |
| `node_doctor_leases_active_total` | gauge | none | Active remediation leases. |
| `node_doctor_leases_granted_total` | counter | `remediation_type` | Leases granted. |
| `node_doctor_leases_denied_total` | counter | `reason` | Leases denied. `reason` is `max_concurrent`, `node_has_lease` or `cooldown`. |
| `node_doctor_reports_received_total` | counter | none | Node reports accepted. |
| `node_doctor_reports_errors_total` | counter | `error_type` | Rejected reports. `error_type` is `invalid_json` or `missing_node_name`. |
| `node_doctor_storage_operations_total` | counter | `operation` | Registered but never incremented. |
| `node_doctor_storage_errors_total` | counter | `operation` | Registered but never incremented. |
| `node_doctor_http_request_duration_seconds` | histogram | `method`, `path`, `status` | Registered but never observed. |
| `node_doctor_http_requests_total` | counter | `method`, `path`, `status` | Registered but never incremented. |
