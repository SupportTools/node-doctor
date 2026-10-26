# Contributing to Node Doctor

Open a pull request against `main`. CI must pass and a maintainer must approve it. This page covers setup, the checks CI runs, and the commit style.

## Prerequisites

- Go 1.25.3 or newer (the `go` line in `go.mod`). CI uses the Go toolchain baked into its self-hosted runners.
- Linux for building and testing the agent. See [Building on macOS](#building-on-macos).
- golangci-lint v1.64.8. The repo has a v1 config, so golangci-lint v2 does not work.
- Docker, kubectl, helm and git. Most make targets check for all of them first.
- kind, for the e2e tests only.

```bash
git clone git@github.com:SupportTools/node-doctor.git
cd node-doctor
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
make build
./bin/node-doctor --version
```

`make help` lists the other targets.

## Building on macOS

The agent only builds on Linux because `pkg/monitors/system` uses Linux syscall types, so `go build` and `go test` fail on macOS. Run the tests in a Linux container instead:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.26 \
  go test ./pkg/... ./cmd/... -race -short
```

## Before you push

Run the same checks CI runs:

```bash
make validate-pipeline-local
```

[docs/testing-guide.md](docs/testing-guide.md#local-ci-check) explains what it runs and what it skips.

CI runs these checks on every pull request:

- golangci-lint v1.64.8 with `.golangci.yml`. It also enforces `gofmt -s`, which catches more than plain `gofmt`. Run `make fmt` to fix formatting.
- Unit tests with `-race -short` and integration tests with `-race`.
- A coverage gate: unit-test coverage of `./pkg/...` and `./cmd/...` must be at least 70%. `make coverage-check` runs the same check.
- gosec, `helm lint`, and a check that `helm/node-doctor/Chart.yaml` and `values.yaml` match their `.template` files.
- A binary build and a Docker build that is not pushed.

Edit `values.yaml.template` and `Chart.yaml.template`, then run `make helm-generate`. The release rebuilds the chart from the templates, so edits made only to the generated files are lost.

## Tests

New code needs tests. [docs/testing-guide.md](docs/testing-guide.md) covers unit, integration and e2e tests. [docs/testing.md](docs/testing.md) covers testing monitors on a live node.

## Writing code

- Handle every error. Wrap it with `fmt.Errorf("...: %w", err)`.
- A new monitor registers itself in an `init()` function with `monitors.MustRegister`. `pkg/monitors/example/noop.go` is the smallest working example.
- A new remediator implements `types.Remediator` and embeds `remediators.BaseRemediator` for cooldown tracking. Register it in `pkg/remediators/builtin.go` and add its strategy name to `validRemediationStrategies` in `pkg/types/config.go`.
- Remediators run as root on production nodes. Validate every input before it reaches a command, and never bypass the cooldown, rate limit or circuit breaker in the remediator registry. [docs/remediation.md](docs/remediation.md) describes these controls.

## Commits and pull requests

Use [Conventional Commits](https://www.conventionalcommits.org/), with a scope when one fits. The PR title uses the same format.

```text
fix(exporter): never write kubelet-owned node conditions
feat(disk): write probe catches a root filesystem that stopped taking writes
ci: setup-go 1.25 -> 1.26
```

Common types are `feat`, `fix`, `docs`, `test`, `refactor`, `ci` and `chore`. Name branches the same way, for example `fix/dns-false-positive`.

In the PR description, say what changed, why, and how you tested it. Link the issue with `Fixes #123`.

## Reporting bugs and security issues

Open a GitHub issue for bugs. Include the Node Doctor version, the Kubernetes version, the node OS, the relevant pod logs, and your configuration with secrets removed.

Do not open public issues for security problems. Follow [SECURITY.md](SECURITY.md).
