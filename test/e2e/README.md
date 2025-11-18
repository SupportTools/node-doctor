# End-to-End Test Suite

This directory contains end-to-end (E2E) tests for Node Doctor that validate the complete workflow:
Monitor → Problem Detection → Export → Remediation

## Overview

The E2E tests deploy Node Doctor to a KIND (Kubernetes IN Docker) cluster, inject real problems,
and verify that the system correctly detects, reports, and remediates issues.

## Architecture

```
test/e2e/
├── README.md              # This file
├── cluster/               # KIND cluster management
│   ├── setup.go          # Cluster creation and configuration
│   ├── teardown.go       # Cleanup utilities
│   └── kind-config.yaml  # KIND cluster configuration
├── injectors/            # Problem injection utilities
│   ├── disk.go          # Disk pressure injection
│   ├── memory.go        # Memory pressure injection
│   ├── network.go       # Network failure injection
│   └── kubernetes.go    # Kubelet/K8s failures
├── observers/            # Status observation utilities
│   ├── events.go        # Kubernetes event monitoring
│   ├── conditions.go    # Node condition monitoring
│   ├── metrics.go       # Prometheus metrics validation
│   └── remediation.go   # Remediation action verification
├── scenarios/            # Test scenarios
│   ├── kubelet_failure_test.go
│   ├── disk_pressure_test.go
│   ├── network_failure_test.go
│   ├── circuit_breaker_test.go
│   ├── rate_limiting_test.go
│   ├── cooldown_test.go
│   ├── multi_problem_test.go
│   └── exporter_validation_test.go
└── utils/                # Shared utilities
    ├── daemonset.go      # DaemonSet deployment helpers
    ├── logging.go        # Test logging utilities
    └── wait.go           # Polling and retry logic

```

## Prerequisites

- Docker installed and running
- KIND CLI installed (`go install sigs.k8s.io/kind@latest`)
- kubectl installed
- Go 1.21+

## Running Tests

### Run all E2E tests:
```bash
go test -v ./test/e2e/scenarios/... -tags=e2e
```

### Run specific scenario:
```bash
go test -v ./test/e2e/scenarios/ -run TestE2E_KubeletFailure -tags=e2e
```

### Skip E2E tests (default):
```bash
go test ./...  # E2E tests are skipped without -tags=e2e
```

## Test Scenarios

### Priority 1 (Core Workflows)
- **Kubelet Failure**: Kubelet stops → Monitor detects → Event exported → Remediation attempted
- **Disk Pressure**: Disk fills → Monitor detects → Condition exported → Remediation cleans up
- **Network Failure**: Network blocked → Monitor detects → Alert exported

### Priority 2 (Safety Mechanisms)
- **Circuit Breaker**: Verify circuit breaker prevents cascading failures
- **Rate Limiting**: Verify rate limits prevent remediation storms
- **Cooldown**: Verify cooldown periods between remediation attempts

### Priority 3 (Integration)
- **Multi-Problem**: Multiple problems occurring simultaneously
- **Exporter Validation**: All 3 exporters (K8s Events, Prometheus, HTTP) working correctly

## Test Flow

Each E2E test follows this pattern:

1. **Setup**: Create KIND cluster, deploy Node Doctor DaemonSet
2. **Inject**: Introduce problem (disk fill, kubelet stop, etc.)
3. **Observe**: Monitor for detection (events, conditions, metrics)
4. **Verify**: Validate remediation actions and safety mechanisms
5. **Cleanup**: Remove problem, teardown cluster

## Debugging

### View cluster logs:
```bash
kind export logs --name node-doctor-e2e ./logs
```

### Access cluster:
```bash
kubectl --context kind-node-doctor-e2e get pods -A
```

### Keep cluster alive for debugging:
```bash
E2E_KEEP_CLUSTER=1 go test -v ./test/e2e/scenarios/... -tags=e2e
```

## CI/CD Integration

E2E tests run in CI only on:
- Pull requests to main branch
- Pre-release testing
- Nightly builds

CI uses `E2E_CI_MODE=1` environment variable to enable stricter timeouts and validation.
