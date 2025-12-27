# Node Doctor

[![CI](https://github.com/supporttools/node-doctor/actions/workflows/ci.yml/badge.svg)](https://github.com/supporttools/node-doctor/actions/workflows/ci.yml)
[![Coverage](https://codecov.io/gh/supporttools/node-doctor/branch/main/graph/badge.svg)](https://codecov.io/gh/supporttools/node-doctor)
[![Go Version](https://img.shields.io/badge/go-1.21+-blue.svg)](https://golang.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/supporttools/node-doctor)](https://goreportcard.com/report/github.com/supporttools/node-doctor)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/supporttools/node-doctor?include_prereleases)](https://github.com/supporttools/node-doctor/releases)
[![Docker](https://img.shields.io/badge/docker-DockerHub-blue)](https://hub.docker.com/r/supporttools/node-doctor)
[![Helm](https://img.shields.io/badge/helm-charts.support.tools-blue)](https://charts.support.tools)

Kubernetes node health monitoring and auto-remediation system. Node Doctor runs as a DaemonSet on each node, performing comprehensive health checks and automatically fixing common problems.

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Health Checks](#health-checks)
- [Remediation](#remediation)
- [HTTP Endpoints](#http-endpoints)
- [Prometheus Metrics](#prometheus-metrics)
- [Examples](#examples)
- [Kubernetes Integration](#kubernetes-integration)
- [Development](#development)
- [Documentation](#documentation)
- [Comparison with Node Problem Detector](#comparison-with-node-problem-detector)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [Security](#security)
- [License](#license)
- [Support](#support)

## Features

- **Comprehensive Health Checks**
  - System resources (CPU, memory, disk)
  - Network connectivity (DNS, gateway, external)
  - Kubernetes components (kubelet, API server, container runtime)
  - Custom plugin execution
  - Log pattern matching

- **Auto-Remediation**
  - Automatic problem resolution with safety mechanisms
  - Cooldown periods and rate limiting
  - Circuit breaker pattern
  - Dry-run mode for testing
  - Remediation history tracking

- **Multiple Export Channels**
  - Kubernetes node conditions and events
  - Node annotations
  - HTTP health endpoints on hostPort
  - Prometheus metrics

- **Safety First**
  - Multiple layers of protection against remediation storms
  - Configurable per-problem remediation strategies
  - Audit trail for all remediation actions
  - Fail-safe defaults

## Architecture

Node Doctor is based on the proven architecture of [Node Problem Detector](https://github.com/kubernetes/node-problem-detector) but extends it with comprehensive auto-remediation capabilities.

```
┌─────────────────────────────────────────────────────────────┐
│                        Node Doctor                           │
│                                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   System     │  │   Network    │  │ Kubernetes   │      │
│  │   Monitors   │  │   Monitors   │  │   Monitors   │ ...  │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘      │
│         │                  │                  │               │
│         └──────────────────┼──────────────────┘              │
│                            ▼                                  │
│                  ┌─────────────────┐                         │
│                  │     Problem     │                         │
│                  │    Detector     │                         │
│                  │  (Orchestrator) │                         │
│                  └─────────┬───────┘                         │
│                            │                                  │
│         ┌──────────────────┼──────────────────┐             │
│         ▼                  ▼                   ▼             │
│  ┌──────────┐      ┌─────────────┐    ┌─────────────┐      │
│  │    K8s   │      │ Remediator  │    │ Prometheus  │      │
│  │ Exporter │      │   Registry  │    │  Exporter   │      │
│  └──────────┘      └─────────────┘    └─────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

See [docs/architecture.md](docs/architecture.md) for detailed architecture information.

## Quick Start

> **Detailed Guide**: For comprehensive instructions including Grafana dashboard import and configuration options, see the [Quick Start Guide](docs/quick-start.md).

### Prerequisites

- Kubernetes 1.20+
- Node OS: Linux

### Version Compatibility

| Component | Minimum Version | Recommended |
|-----------|-----------------|-------------|
| Go | 1.21 | 1.22+ |
| Kubernetes | 1.20 | 1.28+ |
| Node OS | Linux kernel 4.15+ | 5.4+ |
| Container Runtime | containerd 1.6+, Docker 20.10+ | containerd 1.7+ |

### Installation

#### Option 1: Helm Chart (Recommended)

The easiest way to deploy Node Doctor is using the Helm chart:

```bash
# Add the SupportTools Helm repository
helm repo add supporttools https://charts.support.tools
helm repo update

# Install Node Doctor
helm install node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --create-namespace

# Verify deployment
kubectl get daemonset -n node-doctor node-doctor
kubectl get pods -n node-doctor -l app.kubernetes.io/name=node-doctor
```

**Custom Installation with Values:**

```bash
# Install with custom values
helm install node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --create-namespace \
  -f custom-values.yaml

# Or override specific values
helm install node-doctor supporttools/node-doctor \
  --namespace node-doctor \
  --create-namespace \
  --set settings.logLevel=debug \
  --set settings.enableRemediation=false
```

See [helm/node-doctor/README.md](helm/node-doctor/README.md) for complete Helm chart documentation and configuration options.

#### Option 2: Kubernetes DaemonSet (Manual)

1. **Deploy Node Doctor as a DaemonSet**:

```bash
kubectl apply -f deployment/rbac.yaml
kubectl apply -f deployment/configmap.yaml
kubectl apply -f deployment/daemonset.yaml
```

2. **Verify deployment**:

```bash
kubectl get daemonset -n kube-system node-doctor
kubectl get pods -n kube-system -l app=node-doctor
```

3. **Check node health**:

```bash
# Via HTTP endpoint (from a node)
curl http://localhost:8080/health

# Via kubectl
kubectl get nodes -o json | jq '.items[].status.conditions'
```

#### Option 3: Standalone Binary (For Testing/Development)

Download pre-built binaries from the [releases page](https://github.com/supporttools/node-doctor/releases):

```bash
# Linux amd64
wget https://github.com/supporttools/node-doctor/releases/latest/download/node-doctor_linux_amd64.tar.gz
tar -xzf node-doctor_linux_amd64.tar.gz
sudo mv node-doctor /usr/local/bin/
node-doctor --version

# Linux arm64
wget https://github.com/supporttools/node-doctor/releases/latest/download/node-doctor_linux_arm64.tar.gz
tar -xzf node-doctor_linux_arm64.tar.gz
sudo mv node-doctor /usr/local/bin/

# Note: macOS builds temporarily unavailable
# For macOS testing, use Docker or build from source
```

**Verify artifact signatures** (recommended for production):

All releases are dual-signed for defense-in-depth security:
- **Cosign** (GitHub OIDC): Proves CI/CD built it
- **GPG** (Maintainer key): Proves maintainer approved it

```bash
# Layer 1: Cosign verification (GitHub Actions)
brew install cosign  # macOS / or download from https://github.com/sigstore/cosign/releases
cosign verify-blob \
  --signature node-doctor_linux_amd64.tar.gz.cosign.sig \
  --certificate node-doctor_linux_amd64.tar.gz.cosign.crt \
  --certificate-identity-regexp="https://github.com/supporttools/node-doctor" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  node-doctor_linux_amd64.tar.gz

# Layer 2: GPG verification (Maintainer approval)
brew install gnupg  # macOS / or apt-get install gnupg
gpg --keyserver keyserver.ubuntu.com --recv-keys <MAINTAINER_KEY_ID>  # See release notes
gpg --verify node-doctor_linux_amd64.tar.gz.asc node-doctor_linux_amd64.tar.gz

# Both verifications should pass for production deployments ✅
```

#### Option 4: Docker Image

Pull multi-architecture images from Docker Hub:

```bash
# Pull latest stable release
docker pull supporttools/node-doctor:latest

# Pull specific version
docker pull supporttools/node-doctor:v1.0.0

# Run locally for testing
docker run --rm supporttools/node-doctor:latest --help
```

Supported platforms: `linux/amd64`, `linux/arm64`

### Configuration

Node Doctor is configured via a ConfigMap. The default configuration provides sensible defaults for most environments.

To customize:

1. Edit `deployment/configmap.yaml`
2. Apply changes: `kubectl apply -f deployment/configmap.yaml`
3. Node Doctor will automatically reload (or restart pods if needed)

See [docs/configuration.md](docs/configuration.md) for complete configuration reference.

## Health Checks

### System Health

- **CPU**: Load average, thermal throttling
- **Memory**: Available memory, OOM conditions, swap usage
- **Disk**: Disk space, inode usage, I/O health, readonly filesystems

### Network Health

- **DNS**: Cluster DNS and external DNS resolution
- **Gateway**: Default gateway reachability and latency
- **Connectivity**: External connectivity checks

### Kubernetes Components

- **kubelet**: Health endpoint, systemd status, PLEG performance
- **API Server**: Connectivity, latency, authentication
- **Container Runtime**: Docker/containerd/CRI-O health
- **Pod Capacity**: Available pod slots

### Custom Checks

- **Plugin Execution**: Run custom health check scripts
- **Log Patterns**: Match problematic log patterns

See [docs/monitors.md](docs/monitors.md) for detailed monitor documentation.

## Remediation

Node Doctor can automatically fix detected problems with multiple safety mechanisms:

### Safety Mechanisms

- **Cooldown Periods**: Prevent rapid re-remediation (default: 5 minutes)
- **Attempt Limiting**: Max attempts before giving up (default: 3)
- **Circuit Breaker**: Stop after repeated failures
- **Rate Limiting**: Max remediations per hour (default: 10)
- **Dry-Run Mode**: Test without making changes

### Remediation Strategies

- **systemd-restart**: Restart systemd services (kubelet, docker, containerd)
- **network**: Network remediation (flush DNS cache, restart interfaces)
- **disk**: Disk cleanup (clean logs, remove unused images)
- **runtime**: Container runtime remediation
- **custom-script**: Execute custom remediation scripts

See [docs/remediation.md](docs/remediation.md) for detailed remediation documentation.

## HTTP Endpoints

Node Doctor exposes several HTTP endpoints on port 8080 (configurable via hostPort):

- `GET /health` - Overall node health (200=healthy, 503=unhealthy)
- `GET /ready` - Node readiness status
- `GET /metrics` - Prometheus metrics
- `GET /status` - Detailed status of all monitors (JSON)
- `GET /remediation/history` - Remediation action history (JSON)
- `GET /conditions` - Current node conditions (JSON)

Example:

```bash
# From the node
curl http://localhost:8080/health

# From another pod (requires hostNetwork or NodePort service)
curl http://<node-ip>:8080/health
```

## Prometheus Metrics

Node Doctor exposes comprehensive Prometheus metrics on port 9100:

```
# Monitor execution
node_doctor_monitor_checks_total{monitor,result}
node_doctor_monitor_duration_seconds{monitor}

# Problems detected
node_doctor_problems_detected_total{problem,severity}

# Remediation
node_doctor_remediations_attempted_total{problem,remediator,result}
node_doctor_remediations_succeeded_total{problem,remediator}
node_doctor_circuit_breaker_state{problem}

# API operations
node_doctor_api_requests_total{operation,result}
```

## Examples

### Minimal Configuration

```yaml
apiVersion: node-doctor.io/v1alpha1
kind: NodeDoctorConfig
metadata:
  name: minimal

settings:
  nodeName: "${NODE_NAME}"

monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    interval: 30s

exporters:
  kubernetes:
    enabled: true
  http:
    enabled: true
    hostPort: 8080
```

### Enable Auto-Remediation

```yaml
monitors:
  - name: kubelet-health
    type: kubernetes-kubelet-check
    enabled: true
    interval: 30s
    remediation:
      enabled: true
      strategy: systemd-restart
      service: kubelet
      cooldown: 5m
      maxAttempts: 3

remediation:
  enabled: true
  maxRemediationsPerHour: 10
```

### Custom Plugin Check

```yaml
monitors:
  - name: custom-app-health
    type: custom-plugin-check
    enabled: true
    interval: 2m
    config:
      path: /opt/myapp/health_check.sh
      timeout: 30s
    remediation:
      enabled: true
      strategy: custom-script
      scriptPath: /opt/myapp/remediate.sh
```

## Kubernetes Integration

### Node Conditions

Node Doctor updates standard Kubernetes node conditions:

```bash
kubectl get nodes -o json | jq '.items[].status.conditions[] | select(.type | startswith("NodeDoctor"))'
```

Example conditions:
- `KubeletUnhealthy`
- `DiskPressure`
- `MemoryPressure`
- `NetworkUnreachable`

### Events

Node Doctor creates events for:
- Problem detection
- Remediation attempts
- Circuit breaker state changes

```bash
kubectl get events --field-selector involvedObject.kind=Node,involvedObject.name=<node-name>
```

### Annotations

Node Doctor updates node annotations:

```bash
kubectl get node <node-name> -o jsonpath='{.metadata.annotations}' | jq
```

Example annotations:
- `node-doctor.io/status`: Overall health status
- `node-doctor.io/version`: Node Doctor version
- `node-doctor.io/last-check`: Last check timestamp
- `node-doctor.io/last-remediation`: Last remediation timestamp

## Development

### Building from Source

```bash
# Clone repository
git clone https://github.com/supporttools/node-doctor.git
cd node-doctor

# Build binary
make build

# Build Docker image
make docker-build
```

### Running Locally

```bash
# Create local config
cp config/node-doctor.yaml /tmp/config.yaml

# Run with local config
./bin/node-doctor --config=/tmp/config.yaml --log-level=debug
```

### Running Tests

```bash
# Unit tests
make test

# Integration tests
make test-integration

# E2E tests (requires Kubernetes cluster)
make test-e2e

# All tests with coverage
make test-all
```

### Project Structure

```
node-doctor/
├── cmd/node-doctor/          # Main entry point
├── pkg/
│   ├── types/                # Core types
│   ├── detector/             # Problem detection
│   ├── monitors/             # Health monitors
│   ├── remediators/          # Remediation implementations
│   ├── exporters/            # Problem exporters
│   └── util/                 # Utilities
├── config/                   # Example configurations
├── deployment/               # Kubernetes manifests
├── docs/                     # Documentation
└── test/                     # Tests
```

## Documentation

- [Quick Start Guide](docs/quick-start.md) - Deploy, configure, and import dashboards
- [Architecture](docs/architecture.md) - System architecture and design
- [Monitors](docs/monitors.md) - Health check monitor implementations
- [Remediation](docs/remediation.md) - Auto-remediation system
- [Configuration](docs/configuration.md) - Configuration reference
- [Deployment](docs/deployment.md) - Deployment guide for production clusters
- [Release Process](docs/release-process.md) - Release management and versioning
- [Troubleshooting](docs/troubleshooting.md) - Common issues and debugging guide

## Comparison with Node Problem Detector

Node Doctor is based on Node Problem Detector but adds significant enhancements:

| Feature | Node Problem Detector | Node Doctor |
|---------|----------------------|-------------|
| Health Checks | ✅ | ✅ Enhanced |
| Node Conditions | ✅ | ✅ |
| Events | ✅ | ✅ |
| Prometheus Metrics | ✅ | ✅ Enhanced |
| Node Annotations | ❌ | ✅ |
| HTTP Health Endpoints | ⚠️ Basic | ✅ Comprehensive |
| Auto-Remediation | ⚠️ Limited | ✅ Full-featured |
| Safety Mechanisms | ⚠️ Basic | ✅ Multi-layer |
| Circuit Breaker | ❌ | ✅ |
| Rate Limiting | ❌ | ✅ |
| Dry-Run Mode | ❌ | ✅ |
| Remediation History | ❌ | ✅ |
| Network Health Checks | ❌ | ✅ |
| Disk Cleanup | ❌ | ✅ |

## Roadmap

### Phase 1: Core Framework ✅
- ✅ Architecture design
- ✅ Documentation
- ✅ Core types implementation
- ✅ Monitor interface
- ✅ Problem detector (orchestrator)

### Phase 2: Basic Monitors ✅
- ✅ System health monitors (CPU, memory, disk)
- ✅ Network health monitors (DNS, gateway, connectivity)
- ✅ Kubernetes component monitors (kubelet, API server, runtime)
- ✅ Custom plugin execution
- ✅ Log pattern matching

### Phase 3: Exporters ✅
- ✅ Kubernetes exporter (conditions, events, annotations)
- ✅ HTTP exporter (health endpoints)
- ✅ Prometheus exporter (metrics)

### Phase 4: Remediation ✅
- ✅ Remediation framework
- ✅ Safety mechanisms (circuit breaker, rate limiting, cooldowns)
- ✅ Remediator implementations (systemd, network, disk, runtime, custom)

### Phase 5: Testing & Polish (In Progress)
- ✅ Unit tests
- ✅ Integration tests
- ✅ E2E tests
- ⏳ Performance optimization
- ⏳ Production hardening

### Future Enhancements
- ⏳ Dynamic configuration reload
- Machine learning for anomaly detection
- Advanced remediation (node drain, cordon, reboot)
- Multi-cluster aggregation
- Web UI dashboard

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details on our code of conduct and the process for submitting pull requests.

## Security

See [SECURITY.md](SECURITY.md) for our security policy, vulnerability reporting process, and documentation of Node Doctor's security model including privilege requirements.

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.

## Support

- **Issues**: [GitHub Issues](https://github.com/supporttools/node-doctor/issues)
- **Discussions**: [GitHub Discussions](https://github.com/supporttools/node-doctor/discussions)
- **Documentation**: [docs/](docs/)

## Acknowledgments

Node Doctor is inspired by and based on the architecture of [Kubernetes Node Problem Detector](https://github.com/kubernetes/node-problem-detector). We're grateful to the NPD community for their excellent work.

## Related Projects

- [Node Problem Detector](https://github.com/kubernetes/node-problem-detector) - Kubernetes node problem detection
- [node-healthcheck-operator](https://github.com/medik8s/node-healthcheck-operator) - Node health checking operator
- [node-maintenance-operator](https://github.com/medik8s/node-maintenance-operator) - Node maintenance orchestration

---

Made with ❤️ by the SupportTools team
