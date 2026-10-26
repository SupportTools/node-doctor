# Node Doctor Developer Testing Guide

This guide covers running and writing the Go tests. To test monitors on a real node, see [testing.md](testing.md).

The agent only builds on Linux. On macOS, run the commands below in a Linux container, as shown in [CONTRIBUTING.md](../CONTRIBUTING.md#building-on-macos).

## Layout

```text
pkg/**/*_test.go        Unit tests, next to the code
cmd/**/*_test.go        Unit tests for the binaries
test/helpers.go         Assertions, Eventually, temp files, mock Kubernetes client, HTTP server and command executor
test/fixtures.go        Problem and Status builders and ready-made fixtures
test/example_test.go    Examples of the helpers and fixtures
test/integration/       Integration tests, see test/integration/README.md
test/e2e/               End-to-end tests on kind, see test/e2e/README.md
```

## Unit tests

```bash
make test
go test ./pkg/monitors/network/... -run TestGateway -count=1
```

`make test` runs `go test ./pkg/... ./cmd/... -short`. CI runs the same packages with `-race`.

## Integration tests

```bash
make test-integration
```

This runs `go test ./test/integration/...`. These tests need no build tag and no cluster. `chart_probes_test.go` skips when `helm` is not installed.

Two tests need extra setup and do not run in CI:

- `ipv6_kind_test.go` creates a dual-stack kind cluster. It needs the `integration` build tag, Docker and kind, and it skips with `-short`.

  ```bash
  go test -tags=integration ./test/integration/... -run IPv6Kind -v
  ```

- `TestDefaultPinger_Integration` sends real ICMP packets, so it runs as root. CI runs it in its own job.

  ```bash
  make test-net-icmp-integration
  ```

`make test-ci` runs the unit and integration tests the way CI does, without `-race`.

## End-to-end tests

The e2e tests need Docker, kind and kubectl. Each test deletes any existing `node-doctor-e2e` kind cluster, creates a new one, builds the image from `Dockerfile` as `node-doctor:e2e-test`, and loads it into the cluster.

```bash
make test-e2e
go test -tags=e2e ./test/e2e/scenarios/ -run TestE2E_KubeletFailure -v
```

`make test-e2e` runs `go test -tags=e2e ./test/e2e/... -timeout 10m`. Without the `e2e` tag the e2e packages compile to nothing.

The controller scenarios (`controller_coordination_test.go` and `multi_node_correlation_test.go`) also need the `e2e_controller` tag. They do not work yet: no `node-doctor-controller:e2e-test` image is built, and the setup code cannot create worker nodes.

These environment variables help with debugging:

| Variable | Effect |
|---|---|
| `E2E_KEEP_CLUSTER=1` | Keep the kind cluster after the test |
| `E2E_EXPORT_LOGS=1` | Export cluster logs to `e2e-logs/` in the test package directory on teardown |
| `E2E_DEBUG=1` | Print debug log lines |

## Coverage

CI has one coverage gate: unit-test coverage of `./pkg/...` and `./cmd/...` must be at least 70%. There are no per-package thresholds. Check it locally with:

```bash
make coverage-check
```

`make test-all` runs every test in the module and writes an HTML report to `coverage/coverage.html`.

## Local CI check

`scripts/validate-pipeline-local.sh` runs the checks from `.github/workflows/ci.yml` on your machine. It does not install tools. When a tool is missing it skips that check and says so.

```bash
make validate-pipeline-local   # all checks
make validate-quick            # skip the tests
```

It runs `gofmt -s`, `go vet` with `GOOS=linux`, golangci-lint, the Helm template and lint checks, and then the unit and integration tests with `-race`. It prefers the golangci-lint in `$(go env GOPATH)/bin`, because CI pins v1.64.8 and a v2 binary on `PATH` cannot read the v1 config. Off Linux it skips the tests. Pass `--fail-fast` to the script to stop at the first failure.

## Writing tests

Use table-driven tests with `t.Run`. Use `test.Eventually` instead of a fixed `time.Sleep` when you wait for asynchronous work. `test/example_test.go` shows the fixtures and mocks in use.
