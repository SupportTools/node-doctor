# Integration Tests

These tests wire real Node Doctor components together in one process. They need no cluster and no build tag, and CI runs them with `-race`. [docs/testing-guide.md](../../docs/testing-guide.md#integration-tests) explains how to run them and covers the two tests that need extra setup.

```bash
go test ./test/integration/... -race
go test ./test/integration/remediation -run TestCircuitBreakerIntegration -v
```

## Directories

| Path | What it tests |
|---|---|
| `workflows/` | Monitor to detector to exporter flow |
| `remediation/` | Remediation flow, cooldown, circuit breaker, rate limits, and the kubelet restart policy |
| `config/` | Config loading, validation, env var substitution, and hot reload |
| `concurrent/` | Many monitors, remediations and exporters running at once |
| `controller/` | Controller lease and correlation APIs over `httptest` |
| `kubernetes/` | Kubernetes exporter conditions and events, against a fake clientset |
| `chart_probes_test.go` | Renders the Helm chart and checks probes and the Service. Skips without `helm` |
| `ipv6_kind_test.go` | Dual-stack kind cluster. Needs `-tags=integration`, Docker and kind |
| `testdata/` | kind config for the dual-stack test |

## Adding a test

Put it in the directory for the area it covers. Use the helpers and fixtures in `test/helpers.go` and `test/fixtures.go`. Wait for asynchronous work with `test.Eventually`, not a fixed `time.Sleep`.
