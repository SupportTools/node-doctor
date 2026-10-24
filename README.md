# Node Doctor

[![CI](https://github.com/supporttools/node-doctor/actions/workflows/ci.yml/badge.svg)](https://github.com/supporttools/node-doctor/actions/workflows/ci.yml)
[![Coverage](https://codecov.io/gh/supporttools/node-doctor/branch/main/graph/badge.svg)](https://codecov.io/gh/supporttools/node-doctor)
[![Release](https://img.shields.io/github/v/release/supporttools/node-doctor?include_prereleases)](https://github.com/supporttools/node-doctor/releases)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

Node Doctor checks the health of every Kubernetes node and can fix some problems on its own. It runs as a DaemonSet, writes what it finds as node conditions and events, and exports Prometheus metrics. It is modeled on [Node Problem Detector](https://github.com/kubernetes/node-problem-detector) and adds remediation.

## What it checks

- **System:** CPU load, memory and swap, OOM kills, disk space and inodes, read-only filesystems, and a disk write probe
- **Network:** DNS, default gateway, node-to-node connectivity over the CNI, IP forwarding, and IPv6 setup
- **Kubernetes:** kubelet health, API server latency, container runtime and pod capacity
- **Custom:** your own scripts, and regex matches on logs

Each finding becomes a node condition such as `NodeDoctorKubeletUnhealthy`. Remediation is opt-in per monitor and guarded by cooldowns, attempt limits, rate limits, a circuit breaker and dry-run mode. The optional controller collects reports from all nodes and limits how many nodes remediate at once.

## Install

```bash
helm repo add supporttools https://charts.support.tools
```

```bash
helm repo update
```

```bash
helm install node-doctor supporttools/node-doctor --namespace node-doctor --create-namespace
```

The image is `supporttools/node-doctor` on Docker Hub, for linux/amd64 and linux/arm64. Tags have no `v` prefix: release `v1.10.0` is image `1.10.0`.

Next, follow [Quick Start](docs/quick-start.md) to check that it works.

## Documentation

- [Quick Start](docs/quick-start.md): install and check that it works
- [Helm chart](helm/node-doctor/README.md): every chart value
- [Raw manifests](deployment/README.md): install without Helm
- [Controller](docs/controller-deployment.md): the optional cluster-wide controller
- [Dashboards](dashboards/README.md): Grafana dashboards
- [Troubleshooting](docs/troubleshooting.md): debug the agent or a node
- [All docs](docs/README.md): configuration, monitors, remediation, architecture, development

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Report security issues as described in [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).
