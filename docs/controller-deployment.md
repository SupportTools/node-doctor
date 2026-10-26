# Controller

The controller is an optional single-replica Deployment. Agents send it a report every 30 seconds. It stores the reports in SQLite, looks for problems that affect many nodes at once, and hands out remediation leases so only a few nodes remediate at the same time.

**No controller image is published.** `supporttools/node-doctor-controller` does not exist on Docker Hub, and nothing in this repository builds it yet. Enabling the controller with the default image ends in ImagePullBackOff. To use it, build and push your own image first.

## Build an image

The controller is a static Go binary in `cmd/node-doctor-controller`. Its SQLite driver is pure Go, so `CGO_ENABLED=0` works. Save this as `Dockerfile.controller` in the repository root:

```dockerfile
FROM golang:1.25 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /node-doctor-controller ./cmd/node-doctor-controller

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /node-doctor-controller /node-doctor-controller
ENTRYPOINT ["/node-doctor-controller"]
```

Build it and push it to a registry your cluster can pull from:

```bash
docker build -f Dockerfile.controller -t registry.example.com/node-doctor-controller:1.10.0 .
```

```bash
docker push registry.example.com/node-doctor-controller:1.10.0
```

## Install with Helm

Set the image explicitly. When `controller.image.tag` is empty the chart uses `appVersion`, which has a `v` prefix.

```bash
helm upgrade --install node-doctor supporttools/node-doctor --namespace node-doctor --create-namespace --set controller.enabled=true --set controller.image.repository=registry.example.com/node-doctor-controller --set controller.image.tag=1.10.0
```

`controller.enabled: true` creates:

- Deployment and Service `node-doctor-controller` on port 8080. The Deployment uses the `Recreate` strategy because the PVC is ReadWriteOnce.
- ConfigMap `node-doctor-controller-config`, with the controller config file
- PVC `node-doctor-controller-data`, 1Gi, mounted at `/data`
- a ServiceAccount, plus a ClusterRole, a Role and their bindings

It also changes every agent's config:

- the report webhook is turned on and posts to `http://node-doctor-controller.node-doctor:8080/api/v1/reports`
- remediation coordination is turned on, so agents ask the controller for a lease before they remediate

With the default `controller.config.coordination.fallbackOnUnreachable: false`, agents do not remediate while the controller is unreachable. That includes the time it spends in ImagePullBackOff or Pending on an unbound PVC.

All controller values are in the [Helm chart reference](../helm/node-doctor/README.md#controller).

## Install with Kustomize

`deploy/controller` holds plain manifests for the controller alone. They set the image to `supporttools/node-doctor-controller:latest`, which does not exist, so override it:

```bash
cd deploy/controller && kustomize edit set image supporttools/node-doctor-controller=registry.example.com/node-doctor-controller:1.10.0
```

```bash
kubectl apply -k deploy/controller
```

These manifests do not change the agents. Without the Helm chart you must add the report webhook and the coordination block to the agent config yourself; see [Configuration](configuration.md).

## Configuration

The controller reads `/etc/node-doctor/controller.yaml`. In Helm it comes from `controller.config.*`. These environment variables override the file; the chart sets none of them.

| Variable | Overrides |
|---|---|
| `CONTROLLER_BIND_ADDRESS` | `server.bindAddress` |
| `CONTROLLER_PORT` | `server.port` |
| `CONTROLLER_STORAGE_PATH` | `storage.path` |
| `CONTROLLER_STORAGE_RETENTION` | `storage.retention` |
| `CONTROLLER_MAX_CONCURRENT_REMEDIATIONS` | `coordination.maxConcurrentRemediations` |
| `CONTROLLER_NAMESPACE` | `kubernetes.namespace` |

## Endpoints

Everything is served on the main port, 8080 by default. `prometheus.port` in the config file is not used: `/metrics` is always on the main port.

| Path | Purpose |
|---|---|
| `/healthz` | Liveness |
| `/readyz` | Readiness |
| `/metrics` | Prometheus metrics |
| `/api/v1/reports` | Agents post reports here |
| `/api/v1/cluster/status` | Node counts by health |
| `/api/v1/cluster/problems` | Active problems across the cluster |
| `/api/v1/nodes`, `/api/v1/nodes/<node>`, `/api/v1/nodes/<node>/history` | Per-node state and report history |
| `/api/v1/correlations`, `/api/v1/correlations/<id>` | Detected cluster-wide patterns |
| `/api/v1/leases`, `/api/v1/leases/<id>` | Remediation leases |

Reach them through the API server proxy:

```bash
kubectl get --raw /api/v1/namespaces/node-doctor/services/node-doctor-controller:8080/proxy/api/v1/cluster/status
```

## Metrics

The pod has `prometheus.io/*` annotations for port 8080. There is no ServiceMonitor for the controller.

| Area | Metrics |
|---|---|
| Nodes | `node_doctor_cluster_nodes_total`, `node_doctor_cluster_nodes_healthy`, `node_doctor_cluster_nodes_degraded`, `node_doctor_cluster_nodes_critical`, `node_doctor_cluster_nodes_unknown` |
| Problems | `node_doctor_cluster_problems_active`, `node_doctor_cluster_problem_nodes`, `node_doctor_cluster_problem_active` |
| Correlation | `node_doctor_correlation_active_total`, `node_doctor_correlation_detected_total` |
| Leases | `node_doctor_leases_active_total`, `node_doctor_leases_granted_total`, `node_doctor_leases_denied_total` |
| Reports | `node_doctor_reports_received_total`, `node_doctor_reports_errors_total` |
| Storage | `node_doctor_storage_operations_total`, `node_doctor_storage_errors_total` |
| HTTP | `node_doctor_http_requests_total`, `node_doctor_http_request_duration_seconds` |

## Troubleshooting

| Symptom | Check |
|---|---|
| ImagePullBackOff | The image is not published; build your own and set `controller.image.repository` and `controller.image.tag` |
| Pending | The PVC is not bound. Check `kubectl get pvc --namespace node-doctor` and set `controller.persistence.storageClassName` |
| `node_doctor_reports_received_total` stays at 0 | Agents are not reporting. Look for webhook errors in the agent logs |
| Remediation stopped on all nodes | The controller is down and `fallbackOnUnreachable` is false |

```bash
kubectl logs --namespace node-doctor deployment/node-doctor-controller
```

## Uninstall

Set `controller.enabled=false` and upgrade. Helm deletes the PVC and its data along with the other controller objects.
