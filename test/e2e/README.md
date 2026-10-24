# End-to-End Tests

These tests deploy Node Doctor to a kind cluster, cause a real problem on the node, and check what Node Doctor reports and does. They need Docker, kind, kubectl, and the `e2e` build tag. [docs/testing-guide.md](../../docs/testing-guide.md#end-to-end-tests) explains how to run them, the controller scenarios, and the debug variables.

```bash
make test-e2e
go test -tags=e2e ./test/e2e/scenarios/ -run TestE2E_DiskPressure -v
```

CI does not run these tests.

## Directories

| Path | Contents |
|---|---|
| `cluster/` | Creates and deletes the `node-doctor-e2e` kind cluster, builds `node-doctor:e2e-test` and loads it. `kind-config.yaml` defines the cluster |
| `scenarios/` | The tests |
| `observers/` | Watch node conditions, events, metrics and remediation results |
| `utils/` | kubectl helpers, DaemonSet deployment, wait and retry, logging |

## Scenarios

| File | Build tags | Run by `make test-e2e` |
|---|---|---|
| `kubelet_failure_test.go` | `e2e` | Yes |
| `disk_pressure_test.go` | `e2e` | Yes |
| `circuit_breaker_test.go` | `e2e` | Yes |
| `controller_coordination_test.go` | `e2e`, `e2e_controller` | No. Needs a controller image and multi-node setup that do not exist yet |
| `multi_node_correlation_test.go` | `e2e`, `e2e_controller` | No, for the same reason |

Each test creates its own cluster and deletes any existing `node-doctor-e2e` cluster first, so do not point it at a cluster you want to keep.
