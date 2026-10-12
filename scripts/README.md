# Scripts

| Script | What it does | How to run |
|---|---|---|
| `validate-pipeline-local.sh` | Runs the checks from `.github/workflows/ci.yml`: `gofmt -s`, `go vet`, golangci-lint, Helm template drift and lint, and unit plus integration tests. Tests are skipped off Linux because the agent only builds there. | `make validate-pipeline-local`, or `make validate-quick` to skip tests |
| `validate-go-version.sh` | Checks that every `go.mod` targets Go 1.25. | `make check-go-version` |
| `validate-repo-cleanliness.sh` | Flags files that don't belong in the repo root. | `./scripts/validate-repo-cleanliness.sh` |
| `test-monitors.sh` | Exercises every monitor on a live node, grouped by risk (`--safe`, `--caution`, `--destructive`). See [docs/testing.md](../docs/testing.md). | `sudo ./scripts/test-monitors.sh --safe` on a lab node |
| `test-vmxnet3-detection.sh` | Injects fake vmxnet3 kernel messages to check log-pattern detection. | `sudo ./scripts/test-vmxnet3-detection.sh --all` on a lab node |

`validate-pipeline-local.sh` never installs tools. If golangci-lint, helm or envsubst is missing, the check is skipped and reported. CI pins golangci-lint v1.64.8 because `.golangci.yml` is a v1 config.
