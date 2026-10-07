# Scripts

- `validate-pipeline-local.sh` - runs the CI checks locally (gofmt, go vet, golangci-lint, gosec, go test, helm lint, helm-verify-generated). `--quick` skips tests; `SKIP_LINT=1` allows a missing golangci-lint. Wired to `make validate-pipeline-local` and `make validate-quick`.
- `validate-go-version.sh` - checks that go.mod and the Dockerfiles use the required Go minor version. Wired to `make check-go-version`.
- `validate-repo-cleanliness.sh` - checks for stray Go files in the repo root, compiled binaries outside `bin/`, and editor temp files.
- `test-monitors.sh` - exercises the monitors on a real cluster node, grouped by risk level (`--safe`, `--caution`, `--destructive`). Run as root on a dedicated test node.
- `test-vmxnet3-detection.sh` - injects synthetic vmxnet3 kernel messages on a lab node to verify pattern detection. Run as root.
